// Package control is the container side of the control plane: it turns a tap into a request file and
// reads results, audit lines and auto-restore state back. It never constructs a command, and it can
// only ask for something config.yaml already declares (decisions #14-#19, #36, #62-#66).
package control

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// ErrUnavailable means this box has no control plane: the directories are not mounted, so there is
// nothing to ask and nothing to read. The gateway turns it into the unavailable code, because the
// browser renders sentences, not error strings.
var ErrUnavailable = errors.New("control: the control plane is not installed")

// ExecutorUnit is the systemd path unit that fires the executor. The host snapshot carries it because
// the installer adds it to observe.json (T31), so its state is how a silent executor is seen at all.
const ExecutorUnit = "meridian-control.path"

// Executor states as the page renders them.
const (
	ExecutorOK      = "ok"
	ExecutorMissing = "missing"
	ExecutorUnknown = "unknown"
)

const (
	// unclaimedAfter is when a queued request nothing has picked up means the executor is not
	// running. The path unit fires within a second of the file appearing.
	unclaimedAfter = 15 * time.Second
	// maxResults is how many results the view lists; the page has never needed more.
	maxResults = 20
	// maxResultsScanned is how many result files are decoded. Auto-restore entries are older than
	// the listed ones, and losing a countdown would take the cancel button with it.
	maxResultsScanned = 100
	// maxAuditBytes is the tail of audit.jsonl the status view reads. A year of actions fits.
	maxAuditBytes = 256 << 10

	// access(2) modes. syscall has named constants for neither on every platform.
	accessWrite = 0x2
	accessRead  = 0x4
)

// The reasons Available gives. The first is what /api/status.control shows on a Phase 1 box.
const (
	reasonNotInstalled = "control plane not installed"
	reasonNotWritable  = "the control plane is not writable from the container"
	reasonNotReadable  = "the control plane is not readable from the container"
)

// Plane is the container side of the protocol. Every field is a seam: the binary passes the live
// config and the real clock, and a test passes its own (STANDARDS section 1).
type Plane struct {
	// Config returns the configuration being served. A nil func or a nil config means the plane is
	// not installed. It is read per request, so a reload applies at once.
	Config func() *config.Config
	// Host reads the latest host snapshot; nil or absent leaves the executor's health unknown.
	Host func() (wire.HostSnapshot, bool)
	// Now is the clock. Nil means the wall clock.
	Now func() time.Time
	// Random supplies the id bits. Nil means crypto/rand.Reader.
	Random io.Reader
}

// Input is everything the browser may supply for one action. The id, the timestamp and every
// command are server or host side.
type Input struct {
	Action wire.Action
	Target string
	Ack    string
	Ref    string
}

// Available reports whether this box has a usable control plane, and why not when it does not. It is
// what /api/status.control is built from, so a Phase 1 deployment says so instead of offering
// buttons that cannot work.
func (plane Plane) Available() (available bool, reason string) {
	cfg := plane.config()
	if cfg == nil {
		return false, reasonNotInstalled
	}
	inbox := cfg.Control.InboxDir
	for _, dir := range []string{inbox, filepath.Join(inbox, "tmp"), filepath.Join(inbox, "queue"), cfg.Control.OutboxDir} {
		if !isDirectory(dir) {
			return false, reasonNotInstalled
		}
	}
	// A bind mount can be present and still read-only, which the container cannot fix.
	if err := syscall.Access(inbox, accessWrite); err != nil {
		return false, reasonNotWritable
	}
	if err := syscall.Access(cfg.Control.OutboxDir, accessRead); err != nil {
		return false, reasonNotReadable
	}
	return true, ""
}

// Enqueue checks one tap against config and the queue, then writes the request file atomically into
// inbox/queue. The executor checks everything again with the allowlist it trusts; this side's job is
// to refuse what the config already rules out, before the request reaches root.
func (plane Plane) Enqueue(ctx context.Context, in Input) (string, error) {
	cfg := plane.config()
	if cfg == nil {
		return "", ErrUnavailable
	}
	if available, _ := plane.Available(); !available {
		return "", ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	now := plane.now()
	id, err := wire.NewID(now, plane.randomness())
	if err != nil {
		return "", err
	}
	request := wire.Request{
		ID: id, Action: in.Action, Target: in.Target, Ack: in.Ack, Ref: in.Ref, RequestedAt: now,
	}
	if err := wire.ValidateRequest(request, now); err != nil {
		return "", err
	}

	dns, err := authorise(cfg, in)
	if err != nil {
		return "", err
	}

	queueDir := filepath.Join(cfg.Control.InboxDir, "queue")
	names, err := queuedNames(queueDir)
	if err != nil {
		return "", err
	}
	if len(names) >= cfg.Control.QueueDepth {
		return "", wire.Refuse(wire.CodeQueueFull)
	}
	for _, queued := range decodeQueued(queueDir, names) {
		if queued.Action == in.Action && queued.Target == in.Target {
			return "", wire.Refuse(wire.CodeCoalesced)
		}
	}

	if want := wire.RequiredAck(in.Action, dns); in.Ack != want {
		if in.Ack == "" {
			return "", wire.Refuse(wire.CodeMissingAck)
		}
		return "", wire.Refuse(wire.CodeBadAck)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("control: marshal the request: %w", err)
	}
	root, err := os.OpenRoot(cfg.Control.InboxDir)
	if err != nil {
		return "", fmt.Errorf("control: open the inbox: %w", err)
	}
	defer func() { _ = root.Close() }()
	if err := atomicfile.WriteRoot(root, "tmp", "queue", id+".json", payload, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// authorise applies config to a tap. It answers whether the target serves DNS, which is what makes a
// stop need the stronger acknowledgement. An action this table does not know cannot reach it:
// ValidateRequest has already refused it.
func authorise(cfg *config.Config, in Input) (bool, error) {
	if in.Action == wire.ActionReboot && in.Target == "host" {
		// The only target the page does not get from config.yaml, because it is the box itself.
		if !cfg.Control.Reboot {
			return false, wire.Refuse(wire.CodeActionNotAllowed)
		}
		return false, nil
	}
	for index := range cfg.Services {
		service := &cfg.Services[index]
		if service.Control == nil || service.Control.Target != in.Target {
			continue
		}
		// Cancelling an auto-restore is not one of a service's listed actions: it only undoes
		// something this box scheduled, and the executor still checks the ref.
		if in.Action == wire.ActionCancelAutorestore {
			return service.Control.DNS, nil
		}
		for _, allowed := range service.Control.Actions {
			if allowed == string(in.Action) {
				return service.Control.DNS, nil
			}
		}
		return false, wire.Refuse(wire.CodeActionNotAllowed)
	}
	return false, wire.Refuse(wire.CodeUnknownTarget)
}

func (plane Plane) config() *config.Config {
	if plane.Config == nil {
		return nil
	}
	return plane.Config()
}

// now is the clock, or the wall clock when none was injected.
func (plane Plane) now() time.Time {
	if plane.Now == nil {
		return time.Now()
	}
	return plane.Now()
}

func (plane Plane) randomness() io.Reader {
	if plane.Random == nil {
		return rand.Reader
	}
	return plane.Random
}

// isDirectory reports whether path exists and is a directory, without following anything surprising
// beyond what the operator mounted.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// queuedNames lists the request files in dir, oldest first. A file is a request only when its name is
// a canonical id and ".json"; anything else is left for the executor to refuse and delete.
func queuedNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("control: read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		stem, found := strings.CutSuffix(name, ".json")
		if !found {
			continue
		}
		if _, err := wire.ParseID(stem); err != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names) // UUIDv7 names sort by request time
	return names, nil
}

// decodeQueued reads the requests it can. A file that does not decode is the executor's to refuse; it
// must not stop the container from seeing the rest of the queue.
func decodeQueued(dir string, names []string) []wire.Request {
	requests := make([]wire.Request, 0, len(names))
	for _, name := range names {
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		request, err := wire.DecodeRequest(file)
		_ = file.Close()
		if err != nil {
			continue
		}
		requests = append(requests, request)
	}
	return requests
}

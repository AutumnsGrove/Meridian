// The stub executor: a stand-in for `meridian-host exec` in the end-to-end run.
//
// It lives beside the stub upstream for the same reason: the suite must exercise the whole round
// trip (POST, queue file, result, audit line, page) without root, without systemd and without
// touching the real box. It consumes the inbox exactly as the real executor does, writes the same
// wire shapes, and pretends that one target serves DNS so the auto-restore countdown has a subject.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// dnsTargets is the fixture's DNS hazard. It mirrors the `dns: true` in the portable config, which
// is what makes the page warn before arming the stop.
var dnsTargets = map[string]bool{"metrics": true}

// executorDrain is how often the stub looks for a new request, and executorWork is how long it
// pretends the action takes. Together they leave the page's "queued" state visible for at least one
// poll, which is the state the suite asserts on.
const (
	executorDrain = 50 * time.Millisecond
	executorWork  = 2500 * time.Millisecond
	// executorRebootHold is how long a reboot stays in progress, so the page can show its banner.
	executorRebootHold = 1500 * time.Millisecond
)

// autoRestoreWindow mirrors the real executor's ten minutes: long enough that the countdown is
// clearly running, short enough to read in a test.
const autoRestoreWindow = 10 * time.Minute

type executor struct {
	dir  string
	done chan struct{}
}

func newExecutor(controlDir string) *executor {
	return &executor{dir: controlDir, done: make(chan struct{})}
}

// start drains the queue in the background until ctx is cancelled. A request that arrives while an
// action is "running" waits, which is what one action at a time means.
func (exec *executor) start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(executorDrain):
			}
			exec.drain(ctx)
		}
	}()
}

// drain handles everything queued right now, oldest first.
func (exec *executor) drain(ctx context.Context) {
	queue := filepath.Join(exec.dir, "inbox", "queue")
	entries, err := os.ReadDir(queue)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if ctx.Err() != nil {
			return
		}
		exec.handle(ctx, queue, name)
	}
}

// handle reads one request file, writes the result the page will find, and removes the request.
func (exec *executor) handle(ctx context.Context, queue, name string) {
	path := filepath.Join(queue, name)
	data, err := os.ReadFile(path) //nolint:gosec // a fixture: the directory is this run's temp dir
	if err != nil {
		return
	}
	request, err := wire.DecodeRequest(bytes.NewReader(data))
	if err != nil || request.ID+".json" != name {
		// The real executor answers junk with a result and an audit line; the fixture has nothing
		// useful to say about it, so it just clears the queue.
		_ = os.Remove(path)
		return
	}

	// Hold a normal action for the same time the result will claim, so the page's in-flight state is
	// observable for a whole poll interval. Without the hold the result can land before the browser
	// has painted the busy label, which is a race in the fixture rather than in the page.
	if request.Action != wire.ActionReboot {
		select {
		case <-ctx.Done():
			return
		case <-time.After(executorWork):
		}
	}

	if request.Action == wire.ActionReboot {
		// A reboot is the one action whose result must exist before it happens: the container would be
		// gone. The page uses the provisional result to show its banner.
		exec.write(exec.provisional(request))
		select {
		case <-ctx.Done():
			return
		case <-time.After(executorRebootHold):
		}
	}

	result := exec.finish(request)
	exec.write(result)
	exec.audit(result)
	_ = os.Remove(path)
}

// finish is the result the action produces.
func (exec *executor) finish(request wire.Request) wire.Result {
	now := time.Now().UTC()
	result := wire.Result{
		ID: request.ID, Action: request.Action, Target: request.Target,
		OK:         true,
		StartedAt:  now.Add(-executorWork),
		FinishedAt: now,
		DurationMS: executorWork.Milliseconds(),
		Output:     fmt.Sprintf("stub executor: %s %s", request.Action, request.Target),
	}
	switch {
	case request.Action == wire.ActionCancelAutorestore:
		result.Ref = request.Ref
		result.Note = "auto-restore cancelled"
	case request.Action == wire.ActionStop && dnsTargets[request.Target]:
		restoreAt := now.Add(autoRestoreWindow)
		result.RestoreAt = &restoreAt
		result.Note = "auto-restore scheduled at " + restoreAt.Format(time.RFC3339)
	case request.Action == wire.ActionReboot:
		result.Note = "reboot scheduled"
	}
	return result
}

// provisional is the in-progress marker a reboot leaves behind.
func (exec *executor) provisional(request wire.Request) wire.Result {
	now := time.Now().UTC()
	return wire.Result{
		ID: request.ID, Action: request.Action, Target: request.Target,
		OK: true, Note: "in progress", StartedAt: now, InProgress: true,
	}
}

// write puts one result where the container reads it, the same way the real executor does.
func (exec *executor) write(result wire.Result) {
	data, err := json.Marshal(result)
	if err != nil {
		return
	}
	_ = atomicfile.Write(filepath.Join(exec.dir, "outbox", "results"), result.ID+".json", data, 0o644)
}

// audit appends the one line that makes the action a receipt.
func (exec *executor) audit(result wire.Result) {
	line := wire.AuditLine{
		At: result.FinishedAt, ID: result.ID, Action: result.Action, Target: result.Target,
		OK: result.OK, Code: result.Code, DurationMS: result.DurationMS, By: wire.AuditBy,
		Output: strings.TrimSpace(result.Note + " " + result.Output),
	}
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(exec.dir, "outbox", "audit.jsonl"), //nolint:gosec // a fixture path
		os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(append(data, '\n'))
}

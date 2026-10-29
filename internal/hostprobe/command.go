package hostprobe

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/hostfile"
)

// commandTimeout bounds one run of the timer's command. The timer fires every 30s, so a stuck
// systemctl or tailscale call must not still be running when the next one starts.
const commandTimeout = 20 * time.Second

// Options name the files the probe command reads and writes, and who must own the two it trusts.
type Options struct {
	TargetsPath string
	ObservePath string
	OutPath     string
	// Owner is the uid the unit lists must belong to: 0 when they are installed by the root-owned
	// installer, which is how /etc/meridian is written.
	Owner uint32
}

// Run collects one snapshot and writes it to opts.OutPath (mode 0644, atomically). A unit list that
// cannot be read or parsed costs its own units and a log line, never the snapshot: the container
// must still learn the uptime, the other units and the tailnet. The returned error is the write's
// alone, because a snapshot that could not be written is the only failure the timer can act on.
func Run(ctx context.Context, d Deps, opts Options, logf func(string, ...any)) error {
	observe, observeProblem := loadUnitFile(opts.ObservePath, maxObserveSize, opts.Owner)
	targets, targetsProblem := loadUnitFile(opts.TargetsPath, maxTargetsSize, opts.Owner)
	for _, problem := range []string{observeProblem, targetsProblem} {
		if problem != "" {
			logf("%s", problem)
		}
	}

	list := Units(observe, targets)
	for _, problem := range list.Problems {
		logf("%s", problem)
	}
	for _, name := range list.Dropped {
		logf("dropping %q: not a systemd unit name the host may ask systemctl about", name)
	}

	snapshot := Collect(ctx, d, list.Valid)
	document, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	dir, name := filepath.Split(opts.OutPath)
	if dir == "" {
		dir = "."
	}
	if err := atomicfile.Write(dir, name, append(document, '\n'), 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// loadUnitFile reads one of the host's unit lists. A file that is simply not there is normal — a
// launcher-only box has no targets.json, a control-plane box has no observe.json — so it is silent;
// anything else earns a line in the timer's log and contributes no units.
func loadUnitFile(path string, maxSize int64, owner uint32) (content []byte, problem string) {
	if path == "" {
		return nil, ""
	}
	content, err := hostfile.Read(path, hostfile.Trust{Owner: owner, MaxSize: maxSize})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ""
		}
		return nil, err.Error() // names the file, never its contents
	}
	return content, ""
}

// Main runs `meridian-host probe`: parse the flags, collect once, write the snapshot once, and return
// the exit code. It exits non-zero only when the snapshot could not be written.
func Main(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "where to write the snapshot (required)")
	targets := flags.String("targets", "/etc/meridian/targets.json", "the allowlist to take systemd units from")
	observe := flags.String("observe", "/etc/meridian/observe.json", "the observation-only unit list")
	flags.Usage = func() {
		fmt.Fprint(stderr, "usage: meridian-host probe --out PATH [--targets PATH] [--observe PATH]\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "meridian-host probe: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}
	if *out == "" {
		fmt.Fprintln(stderr, "meridian-host probe: --out PATH is required")
		flags.Usage()
		return 2
	}

	logf := func(format string, values ...any) {
		fmt.Fprintf(stderr, "meridian-host probe: "+format+"\n", values...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	err := Run(ctx, RealDeps(), Options{
		TargetsPath: *targets,
		ObservePath: *observe,
		OutPath:     *out,
		Owner:       0,
	}, logf)
	if err != nil {
		fmt.Fprintf(stderr, "meridian-host probe: %v\n", err)
		return 1
	}
	return 0
}

package hostexec

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
)

// Main runs `meridian-host exec`: one pass over the queue, a result and an audit line for every
// request, then exit. It is the exact command the sudoers rule names, so it takes no positional
// arguments; the flags exist for the installer and the tests (decision #66).
func Main(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	host := flags.String("host", DefaultHostFile, "the host configuration (root-owned)")
	targets := flags.String("targets", DefaultTargetsFile, "the allowlist (root-owned)")
	lock := flags.String("lock", DefaultLockFile, "the lock that makes one run at a time")
	flags.Usage = func() {
		fmt.Fprint(stderr, "usage: meridian-host exec [--host PATH] [--targets PATH] [--lock PATH]\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "meridian-host exec: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}

	// Everything goes to stderr, which is the journal: the executor never writes to stdout, so a
	// future caller cannot mistake a log line for data.
	logger := log.New(stderr, "meridian-host exec: ", 0)
	executor := &Executor{
		HostFile:    *host,
		TargetsFile: *targets,
		LockFile:    *lock,
		Owner:       0,
		Build:       Build,
		Run:         Runner(logger.Printf),
		Log:         logger,
	}
	if err := executor.Execute(context.Background()); err != nil {
		// A run that cannot trust its own configuration exits non-zero with nothing executed, so the
		// unit fails visibly instead of pretending the queue is empty.
		logger.Print(err)
		return 1
	}
	return 0
}

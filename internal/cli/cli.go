// Package cli implements the meridian command line.
package cli

import (
	"fmt"
	"io"
)

// Set at link time with -ldflags "-X github.com/autumnsgrove/meridian/internal/cli.version=...".
// An unstamped build is "dev" in every field.
var (
	version = "dev"
	commit  = "dev"
	date    = "dev"
)

const usage = `usage: meridian <command>

commands:
  version   print the build version
  help      print this help
`

// Version reports the identity of this build: name, version, commit and build date.
func Version() string {
	return fmt.Sprintf("meridian %s (commit %s, built %s)", version, commit, date)
}

// Main runs the meridian command line and returns the process exit code. It writes to the writers
// it is given so the whole CLI can be tested without a real process.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, Version())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "meridian: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

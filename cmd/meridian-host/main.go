// Command meridian-host is the small stdlib-only binary that runs on the host, outside the container
// and outside the repo. It never holds the dashboard's config and has no shell: every command it runs
// is a fixed argv whose variable parts came from a root-owned file.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/hostprobe"
)

const usage = `usage: meridian-host <command> [flags]

commands:
  probe   write the host snapshot the dashboard reads
  exec    run one pass over the control queue, as root, through sudo
  update        pin, pull and recreate the dashboard from the digest it resolved
  render-units  fill the deploy templates with the installer's paths

Run "meridian-host <command> --help" for that command's flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "probe":
		return hostprobe.Main(args[1:], stderr)
	case "exec":
		return hostexec.Main(args[1:], stderr)
	case "update":
		return hostexec.UpdateMain(args[1:], stderr)
	case "render-units":
		return hostexec.RenderUnitsMain(args[1:], stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "meridian-host: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

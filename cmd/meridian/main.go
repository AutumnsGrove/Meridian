// Command meridian is the dashboard container: one page of services, one API.
package main

import (
	"os"

	"github.com/autumnsgrove/meridian/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}

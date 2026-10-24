// Command meridian-host is the small stdlib-only binary that runs on the host with root.
// It is not the container binary and never imports it.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "meridian-host: not implemented yet")
	os.Exit(2)
}

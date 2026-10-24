// Fixture for the no shell rule: a command is routed through a shell.
package bad

import "os/exec"

func run() error {
	return exec.Command("sh", "-c", "true").Run()
}

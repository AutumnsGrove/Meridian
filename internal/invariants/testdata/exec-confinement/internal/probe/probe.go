// Fixture for the os/exec confinement rule: the container side runs a subprocess.
package probe

import "os/exec"

func run() error {
	return exec.Command("true").Run()
}

// Fixture for the exit-in-cmd rule: a library package exits the process.
package probe

import "os"

func fail() {
	os.Exit(1)
}

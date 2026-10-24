// Fixture for the atomic-write rule: a handoff file is written in place.
package control

import "os"

func save(path string) error {
	return os.WriteFile(path, []byte("{}"), 0o600)
}

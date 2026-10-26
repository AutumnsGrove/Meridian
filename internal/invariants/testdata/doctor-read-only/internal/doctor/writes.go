package doctor

import "os"

// Save writes a report to disk. Doctor must never do this: the rule under test says so.
func Save(path string, report []byte) error {
	return os.WriteFile(path, report, 0o600)
}

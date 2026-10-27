// Fixture for the no-shutdown rule: a verb that is unrecoverable without physical access.
package hostexec

var commands = map[string][]string{
	"stop": {"systemctl", "poweroff"},
}

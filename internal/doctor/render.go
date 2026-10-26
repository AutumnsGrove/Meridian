package doctor

import (
	"fmt"
	"strings"
)

// Render turns a report into the text a person reads. It is pure and stable: same report, same text,
// no colour, no timestamps, so a golden file can pin it.
func Render(report Report) string {
	var out strings.Builder
	out.WriteString("meridian doctor\n")

	if len(report.Config) > 0 {
		out.WriteString("\nconfig\n")
		writeChecks(&out, report.Config)
	}

	for _, service := range report.Services {
		out.WriteString("\n" + service.Name + "\n")
		writeChecks(&out, service.Checks)
	}

	out.WriteString("\n" + summary(report) + "\n")
	return out.String()
}

// writeChecks prints one line per check: the level, what was checked, what was found, and the code a
// bug report can quote. A hint hangs under the detail so the columns stay readable.
func writeChecks(out *strings.Builder, checks []Check) {
	for _, check := range checks {
		fmt.Fprintf(out, "  %-4s %-11s %s [%s]\n", check.Level, check.Name, check.Detail, check.Code)
		if check.Hint != "" && check.Level != OK {
			fmt.Fprintf(out, "  %-4s %-11s hint: %s\n", "", "", check.Hint)
		}
	}
}

// summary is the last line: the count that decides the exit code, said in a sentence.
func summary(report Report) string {
	fails, warns := 0, 0
	for _, check := range report.Config {
		count(&fails, &warns, check)
	}
	for _, service := range report.Services {
		for _, check := range service.Checks {
			count(&fails, &warns, check)
		}
	}

	switch {
	case fails > 0:
		return fmt.Sprintf("%d failing %s. Fix %s, then run meridian doctor again.",
			fails, plural(fails, "check", "checks"), plural(fails, "it", "them"))
	case warns > 0:
		return fmt.Sprintf("No failures. %d %s.", warns, plural(warns, "warning", "warnings"))
	}
	return "Everything checks out."
}

func count(fails, warns *int, check Check) {
	switch check.Level { //nolint:exhaustive // OK is the only level that counts for nothing
	case Fail:
		*fails++
	case Warn:
		*warns++
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

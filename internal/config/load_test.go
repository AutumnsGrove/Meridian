package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
)

// minimal is a complete config that passes validation in this task. Rows in the broken-config table
// mutate one piece of it.
const appService = `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}}`

const minimal = `listen: 127.0.0.1:8891
sections:
  - {id: everyday, title: Everyday}
services:
` + appService + "\n"

func substitute(t *testing.T, document, old, replacement string) string {
	t.Helper()
	if !strings.Contains(document, old) {
		t.Fatalf("test setup: %q does not contain %q", document, old)
	}
	return strings.Replace(document, old, replacement, 1)
}

func parse(t *testing.T, document string) (*config.Config, []config.Warning) {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(document))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return parsed, warnings
}

func parseProblems(t *testing.T, document string) config.Problems {
	t.Helper()
	_, _, err := config.Parse([]byte(document))
	if err == nil {
		t.Fatal("Parse returned no error, want problems")
	}
	var problems config.Problems
	if !errors.As(err, &problems) {
		t.Fatalf("Parse error = %v, want Problems", err)
	}
	return problems
}

func TestParse_AppliesDefaults(t *testing.T) {
	parsed, warnings := parse(t, minimal)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	if got, want := time.Duration(parsed.Poll.Interval), 15*time.Second; got != want {
		t.Errorf("poll.interval = %v, want %v", got, want)
	}
	if got, want := time.Duration(parsed.Poll.IdleInterval), 2*time.Minute; got != want {
		t.Errorf("poll.idle_interval = %v, want %v", got, want)
	}
	if got, want := time.Duration(parsed.Poll.IdleAfter), 10*time.Minute; got != want {
		t.Errorf("poll.idle_after = %v, want %v", got, want)
	}
	if got, want := time.Duration(parsed.Poll.Timeout), 4*time.Second; got != want {
		t.Errorf("poll.timeout = %v, want %v", got, want)
	}
	if got, want := time.Duration(parsed.Poll.RefreshMin), 5*time.Second; got != want {
		t.Errorf("poll.refresh_min = %v, want %v", got, want)
	}
	if parsed.Control.QueueDepth != 8 {
		t.Errorf("control.queue_depth = %d, want 8", parsed.Control.QueueDepth)
	}
	if got, want := time.Duration(parsed.Control.ActionTimeout), 90*time.Second; got != want {
		t.Errorf("control.action_timeout = %v, want %v", got, want)
	}
	if parsed.Control.AuditLines != 50 {
		t.Errorf("control.audit_lines = %d, want 50", parsed.Control.AuditLines)
	}
	if got, want := time.Duration(parsed.Control.AuditRetention), 365*24*time.Hour; got != want {
		t.Errorf("control.audit_retention = %v, want %v", got, want)
	}
	if parsed.Control.InboxDir != "/control/inbox" || parsed.Control.OutboxDir != "/control/outbox" {
		t.Errorf("control dirs = %q, %q, want the container defaults", parsed.Control.InboxDir, parsed.Control.OutboxDir)
	}
	if parsed.HostStatus != nil {
		t.Errorf("host_status = %+v, want nil when the block is absent", parsed.HostStatus)
	}
}

const appServiceWithUpdate = `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, actions: [restart, update]}}`

func TestParse_DefaultsTheUpdateSignalDirectory(t *testing.T) {
	document := substitute(t, minimal, appService, appServiceWithUpdate)
	document = substitute(t, document, "listen: 127.0.0.1:8891",
		"listen: 127.0.0.1:8891\nupdate: {image: ghcr.io/owner/name, publish_url: \"https://api.example.org/runs\", target: app}")
	parsed, _ := parse(t, document)
	if parsed.Update == nil {
		t.Fatal("update = nil, want the block")
	}
	if got, want := parsed.Update.SignalDir, "/update-signal"; got != want {
		t.Errorf("update.signal_dir = %q, want %q", got, want)
	}
	if got, want := parsed.Update.Image, "ghcr.io/owner/name"; got != want {
		t.Errorf("update.image = %q, want %q", got, want)
	}
	if got, want := parsed.Update.PublishURL, "https://api.example.org/runs"; got != want {
		t.Errorf("update.publish_url = %q, want %q", got, want)
	}
}

func TestParse_KeepsTheUpdateSignalDirectoryWhenWritten(t *testing.T) {
	document := substitute(t, minimal, appService, appServiceWithUpdate)
	document = substitute(t, document, "listen: 127.0.0.1:8891",
		"listen: 127.0.0.1:8891\nupdate: {image: ghcr.io/owner/name, publish_url: \"https://api.example.org/runs\", signal_dir: /var/run/update, target: app}")
	parsed, _ := parse(t, document)
	if got, want := parsed.Update.SignalDir, "/var/run/update"; got != want {
		t.Errorf("update.signal_dir = %q, want %q", got, want)
	}
}

func TestParse_LeavesUpdateNilWhenTheBlockIsAbsent(t *testing.T) {
	parsed, _ := parse(t, minimal)
	if parsed.Update != nil {
		t.Errorf("update = %+v, want nil", parsed.Update)
	}
}

func TestParse_KeepsExplicitValuesOverDefaults(t *testing.T) {
	document := substitute(t, minimal, "listen: 127.0.0.1:8891",
		"listen: 127.0.0.1:8891\npoll: {interval: 30s, timeout: 2s}")
	parsed, _ := parse(t, document)
	if got, want := time.Duration(parsed.Poll.Interval), 30*time.Second; got != want {
		t.Errorf("poll.interval = %v, want %v", got, want)
	}
	if got, want := time.Duration(parsed.Poll.Timeout), 2*time.Second; got != want {
		t.Errorf("poll.timeout = %v, want %v", got, want)
	}
}

func TestParse_DefaultsHostStatusMaxAgeWhenTheBlockIsPresent(t *testing.T) {
	document := substitute(t, minimal, "listen: 127.0.0.1:8891",
		"listen: 127.0.0.1:8891\nhost_status: {file: /status/host.json}")
	parsed, _ := parse(t, document)
	if parsed.HostStatus == nil {
		t.Fatal("host_status = nil, want the block")
	}
	if got, want := time.Duration(parsed.HostStatus.MaxAge), 90*time.Second; got != want {
		t.Errorf("host_status.max_age = %v, want %v", got, want)
	}
}

func TestParse_ReportsAllProblemsAtOnce(t *testing.T) {
	document := substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: nowhere")
	document = substitute(t, document, "icon: sparkles", `icon: ""`)
	document = substitute(t, document, "name: App", `name: ""`)

	problems := parseProblems(t, document)
	if len(problems) != 3 {
		t.Fatalf("problems = %v, want three", problems)
	}
	seen := map[config.Code]bool{}
	for _, problem := range problems {
		seen[problem.Code] = true
	}
	for _, code := range []config.Code{config.CodeListen, config.CodeIconInvalid, config.CodeServiceNameMissing} {
		if !seen[code] {
			t.Errorf("problems = %v, want one with code %s", problems, code)
		}
	}
}

func TestParse_RejectsWhatTheSchemaDoesNotHave(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "unknown top-level key",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nnope: 1"),
		},
		{
			name:     "unknown probe key",
			document: substitute(t, minimal, "{kind: self}", `{kind: self, ur: "http://localhost/"}`),
		},
		{
			name:     "unknown service key",
			document: substitute(t, minimal, "icon: sparkles", "icon: sparkles, icons: mail"),
		},
		{
			name:     "unknown icon key",
			document: substitute(t, minimal, "icon: sparkles", "icon: {image: icons/x.png, size: 24}"),
		},
		{
			name:     "a second document",
			document: minimal + "---\nlisten: 127.0.0.1:9999\n",
		},
		{
			name:     "a duration that is not one",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\npoll: {interval: soon}"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			problems := parseProblems(t, test.document)
			if len(problems) != 1 || problems[0].Code != config.CodeDecode {
				t.Fatalf("problems = %v, want one decode problem", problems)
			}
		})
	}
}

func TestParse_EmptyDocumentIsAllMissingPieces(t *testing.T) {
	problems := parseProblems(t, "")
	if len(problems) < 2 {
		t.Fatalf("problems = %v, want at least sections_missing and services_missing", problems)
	}
}

func TestLoad_ReadsAFileAndNamesItInErrors(t *testing.T) {
	dir := t.TempDir()
	validPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(validPath, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Load(validPath); err != nil {
		t.Fatalf("Load(valid): %v", err)
	}

	brokenPath := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(brokenPath, []byte("listen: nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := config.Load(brokenPath)
	if err == nil {
		t.Fatal("Load(broken) = nil, want an error")
	}
	if !strings.Contains(err.Error(), brokenPath) {
		t.Errorf("error = %q, want it to name %s", err, brokenPath)
	}
}

func TestLoad_MissingFileIsNotAValidationProblem(t *testing.T) {
	_, _, err := config.Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Fatal("Load(missing) = nil, want an error")
	}
	var problems config.Problems
	if errors.As(err, &problems) {
		t.Errorf("error = %v, want a plain error rather than Problems", err)
	}
}

func TestLoad_RefusesAFileOverTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.yaml")
	content := minimal + "# " + strings.Repeat("x", config.MaxConfigSize) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Load(path); err == nil {
		t.Fatal("Load(oversized) = nil, want an error")
	}
}

func TestProblems_ErrorIsReadable(t *testing.T) {
	problems := config.Problems{
		{Path: "listen", Code: config.CodeListen, Message: "listen must be host:port"},
		{Path: "services[0].name", Code: config.CodeServiceNameMissing, Message: "name is required"},
	}
	message := problems.Error()
	for _, want := range []string{"2 problems", "listen: listen must be host:port", "services[0].name: name is required"} {
		if !strings.Contains(message, want) {
			t.Errorf("Problems.Error() = %q, want it to contain %q", message, want)
		}
	}
}

// FuzzParse holds the loader contract for arbitrary bytes: never panic, never return a config with
// an error, never return neither, and never return a config whose own validation fails again.
func FuzzParse(f *testing.F) {
	f.Add([]byte(minimal))
	f.Add([]byte(appService))
	f.Add([]byte(""))
	f.Add([]byte("listen: ["))
	f.Add([]byte("listen: 127.0.0.1:8891\nservices:\n  - {id: x, name: X, section: y, icon: z, probes: {health: {kind: self}}}"))
	f.Add([]byte(strings.Repeat("- ", 1000)))
	f.Add([]byte("---\n---\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, _, err := config.Parse(data)
		if err != nil {
			if parsed != nil {
				t.Fatal("Parse returned a config together with an error")
			}
			return
		}
		if parsed == nil {
			t.Fatal("Parse returned no config and no error")
		}
		problems, _ := config.Validate(parsed)
		if len(problems) != 0 {
			t.Fatalf("validating a loaded config found problems: %v", problems)
		}
	})
}

// The shipped configs are the product for anyone else, so they are held to the same validation as a
// user's file. The template is a complete config; the worked example is the homelab's own.
func TestLoad_TheShippedExamplesValidate(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "config.yaml.example"),
		filepath.Join("..", "..", "examples", "homelab.yaml"),
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, _, err := config.Load(path); err != nil {
				t.Fatalf("Load(%s): %v", path, err)
			}
		})
	}
}

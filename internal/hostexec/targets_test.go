package hostexec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

const deployUser = "deploy"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hostexec", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func TestLoadTargets_AcceptsTheGolden(t *testing.T) {
	targets, err := hostexec.LoadTargets(readFixture(t, "targets.golden.json"), deployUser)
	if err != nil {
		t.Fatalf("LoadTargets: %v", err)
	}

	app, ok := targets["app"]
	if !ok {
		t.Fatalf("targets = %v, want an app", targets)
	}
	if app.Kind != "systemd" || app.Unit != "app.service" {
		t.Errorf("app = %+v, want a systemd unit app.service", app)
	}
	if len(app.Actions) != 4 || app.Actions[0] != "start" {
		t.Errorf("app.Actions = %v, want start, stop, restart, update", app.Actions)
	}
	if app.Update == nil || app.Update.Kind != "command" || app.Update.RunAs != deployUser {
		t.Errorf("app.Update = %+v, want a command run as the deploy user", app.Update)
	}

	stack := targets["stack"]
	if stack.Kind != "compose" || stack.Dir != "/srv/stack" || stack.Service != "web" {
		t.Errorf("stack = %+v, want the compose project /srv/stack service web", stack)
	}
	if stack.UpdateTimeoutS != 420 {
		t.Errorf("stack.UpdateTimeoutS = %d, want 420", stack.UpdateTimeoutS)
	}

	if resolver := targets["resolver"]; !resolver.DNS {
		t.Errorf("resolver.DNS = false, want true")
	}
	if worker := targets["worker"]; worker.Container != "worker" {
		t.Errorf("worker = %+v, want the container worker", worker)
	}
	if host := targets["host"]; host.Kind != "host" || len(host.Actions) != 1 || host.Actions[0] != "reboot" {
		t.Errorf("host = %+v, want reboot only", host)
	}
}

func TestLoadTargets_RefusesWhatTheExecutorCannotSafelyRun(t *testing.T) {
	// One whole document per row: every row must be refused. A single impossible target refuses the
	// entire allowlist, because running half of one is worse than running none.
	const valid = `"kind":"systemd","unit":"app.service","actions":["start","restart"]`
	wrap := func(id, body string) string {
		return `{"targets":{"` + id + `":{` + body + `}}}`
	}

	tests := []struct {
		name     string
		document string
	}{
		{"not JSON", wrap("app", `"kind":`)},
		{"not an object", `{"targets":[]}`},
		{"unknown field", wrap("app", valid+`,"surprise":true`)},
		{"unknown kind", wrap("app", `"kind":"daemon","unit":"app.service","actions":["start"]`)},
		{"id is not a slug", wrap("App", valid)},
		{"id is a path", wrap("../app", valid)},
		{"no actions", wrap("app", `"kind":"systemd","unit":"app.service","actions":[]`)},
		{"unknown action", wrap("app", `"kind":"systemd","unit":"app.service","actions":["destroy"]`)},
		{"cancel-autorestore in actions", wrap("app", `"kind":"systemd","unit":"app.service","actions":["cancel-autorestore"]`)},
		{"dns without a stop to guard", wrap("app", `"kind":"systemd","unit":"app.service","actions":["start","restart"],"dns":true`)},
		{"reboot on a non-host target", wrap("app", `"kind":"systemd","unit":"app.service","actions":["reboot"]`)},
		{"duplicate action", wrap("app", `"kind":"systemd","unit":"app.service","actions":["restart","restart"]`)},
		{"systemd without a unit", wrap("app", `"kind":"systemd","actions":["start"]`)},
		{"unit is not a unit name", wrap("app", `"kind":"systemd","unit":"--all","actions":["start"]`)},
		{"unit with a path", wrap("app", `"kind":"systemd","unit":"../../bin/sh","actions":["start"]`)},
		{"compose without a dir", wrap("app", `"kind":"compose","service":"web","actions":["start"]`)},
		{"compose with a relative dir", wrap("app", `"kind":"compose","dir":"srv/stack","service":"web","actions":["start"]`)},
		{"compose with an unclean dir", wrap("app", `"kind":"compose","dir":"/srv/../etc","service":"web","actions":["start"]`)},
		{"compose without a service", wrap("app", `"kind":"compose","dir":"/srv/stack","actions":["start"]`)},
		{"compose with a hostile service", wrap("app", `"kind":"compose","dir":"/srv/stack","service":"-v","actions":["start"]`)},
		{"container without a name", wrap("app", `"kind":"container","actions":["start"]`)},
		{"container with a flag name", wrap("app", `"kind":"container","container":"--all","actions":["start"]`)},
		{"container with a path", wrap("app", `"kind":"container","container":"../../x","actions":["start"]`)},
		{"host with a unit", wrap("host", `"kind":"host","unit":"app.service","actions":["reboot"]`)},
		{"update without the update action", wrap("app", valid+`,"update":{"kind":"unit","unit":"app-update.service"}`)},
		{"unknown update kind", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"script","cmd":["/opt/app","update"]}`)},
		{"command update without a cmd", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"command"}`)},
		{"command update with an empty cmd", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"command","cmd":[]}`)},
		{"command update with a relative program", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"command","cmd":["app","update"]}`)},
		{"command update with a NUL", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"command","cmd":["/opt/app\u0000","update"]}`)},
		{"command update as another user", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"command","cmd":["/opt/app","update"],"run_as":"root"}`)},
		{"unit update without a unit", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"update":{"kind":"unit"}`)},
		{"no_update_reason with an update", wrap("app", `"kind":"systemd","unit":"app.service","actions":["update"],"no_update_reason":"none","update":{"kind":"unit","unit":"app-update.service"}`)},
		{"unknown criticality", wrap("app", valid+`,"criticality":"apocalyptic"`)},
		{"timeout too large", wrap("app", valid+`,"update_timeout_s":1801`)},
		{"timeout negative", wrap("app", valid+`,"update_timeout_s":-1`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targets, err := hostexec.LoadTargets([]byte(test.document), deployUser)
			if err == nil {
				t.Fatalf("LoadTargets(%s) = %v, want an error", test.document, targets)
			}
		})
	}
}

func TestLoadTargets_RefusesAnEmptyAllowlist(t *testing.T) {
	for _, document := range []string{`{}`, `{"targets":{}}`, `{"targets":null}`, ``} {
		if _, err := hostexec.LoadTargets([]byte(document), deployUser); err == nil {
			t.Errorf("LoadTargets(%q) = nil, want an error", document)
		}
	}
}

func TestLoadHost_AcceptsTheGolden(t *testing.T) {
	host, err := hostexec.LoadHost(readFixture(t, "host.golden.json"))
	if err != nil {
		t.Fatalf("LoadHost: %v", err)
	}
	if host.ControlDir != "/srv/meridian/control" {
		t.Errorf("ControlDir = %q, want /srv/meridian/control", host.ControlDir)
	}
	if host.DeployUser != deployUser {
		t.Errorf("DeployUser = %q, want %q", host.DeployUser, deployUser)
	}
}

func TestLoadHost_RefusesWhatTheExecutorCannotUse(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{"empty", ``},
		{"not an object", `[]`},
		{"unknown field", `{"control_dir":"/srv/c","deploy_user":"deploy","extra":1}`},
		{"no control dir", `{"deploy_user":"deploy"}`},
		{"relative control dir", `{"control_dir":"control","deploy_user":"deploy"}`},
		{"unclean control dir", `{"control_dir":"/srv/../etc","deploy_user":"deploy"}`},
		{"no deploy user", `{"control_dir":"/srv/c"}`},
		{"deploy user with a slash", `{"control_dir":"/srv/c","deploy_user":"../root"}`},
		{"deploy user that looks like a flag", `{"control_dir":"/srv/c","deploy_user":"-root"}`},
		{"deploy user with a control character", `{"control_dir":"/srv/c","deploy_user":"de\u0000ploy"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if host, err := hostexec.LoadHost([]byte(test.document)); err == nil {
				t.Fatalf("LoadHost(%s) = %+v, want an error", test.document, host)
			}
		})
	}
}

// FuzzLoadTargets feeds arbitrary bytes at the allowlist parser: it must never panic, and anything it
// accepts must pass the same validation again.
func FuzzLoadTargets(f *testing.F) {
	f.Add([]byte(`{"targets":{}}`))
	f.Add([]byte(`{"targets":{"app":{"kind":"systemd","unit":"app.service","actions":["restart"]}}}`))
	f.Add([]byte(`{"targets":{"a":{"kind":"compose","dir":"/srv/a","service":"s","actions":["start"]}}}`))
	f.Add([]byte(`{"targets":{"host":{"kind":"host","actions":["reboot"]}}}`))
	f.Add([]byte(`{"targets":{"x":{"kind":"systemd","unit":"--all","actions":["start"]}}}`))
	f.Add([]byte(`{"targets":{"x":{"kind":"systemd\u0000","unit":"a.service","actions":["start"]}}}`))
	f.Add([]byte(`{"targets":{"x":null}}`))
	f.Add([]byte(`{"targets":{"` + strings.Repeat("a", 200) + `":{"kind":"host","actions":["reboot"]}}}`))
	f.Add([]byte(`{"targets":[]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		targets, err := hostexec.LoadTargets(data, deployUser)
		if err != nil {
			return
		}
		// What loaded once loads again identically: the validator is deterministic and the values
		// it accepted are re-checkable without the bytes.
		if verifyErr := hostexec.ValidateTargets(targets, deployUser); verifyErr != nil {
			t.Fatalf("accepted targets do not validate: %v", verifyErr)
		}
	})
}

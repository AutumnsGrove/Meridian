package hostexec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

// renderValues is one installer's values. The paths deliberately look like the box's layout.
func renderValues() hostexec.UnitValues {
	return hostexec.UnitValues{
		ControlDir: "/srv/meridian/control",
		DeployUser: "deploy",
		StatusDir:  "/srv/meridian/status",
	}
}

func rendered(t *testing.T, output, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestRenderUnits_RendersEveryTemplateWithoutAPlaceholder(t *testing.T) {
	templates := filepath.Join("..", "..", "deploy")
	output := t.TempDir()
	if err := hostexec.RenderUnits(templates, output, renderValues()); err != nil {
		t.Fatalf("RenderUnits: %v", err)
	}

	for _, name := range []string{
		"systemd/meridian-control.path",
		"systemd/meridian-control.service",
		"systemd/meridian-probe.service",
		"systemd/meridian-probe.timer",
		"sudoers/meridian",
	} {
		body := rendered(t, output, name)
		if strings.Contains(body, "@") {
			t.Errorf("%s still has an @ placeholder:\n%s", name, body)
		}
	}

	path := rendered(t, output, "systemd/meridian-control.path")
	for _, want := range []string{
		"DirectoryNotEmpty=/srv/meridian/control/inbox/queue",
		"Unit=meridian-control.service",
	} {
		if !strings.Contains(path, want) {
			t.Errorf("meridian-control.path does not contain %q:\n%s", want, path)
		}
	}

	control := rendered(t, output, "systemd/meridian-control.service")
	for _, want := range []string{
		"User=deploy",
		"ExecStart=/usr/bin/sudo -n /etc/meridian/meridian-host exec",
		"TimeoutStartSec=660",
		"StartLimitIntervalSec=0",
	} {
		if !strings.Contains(control, want) {
			t.Errorf("meridian-control.service does not contain %q:\n%s", want, control)
		}
	}
	if strings.Contains(control, "NoNewPrivileges=") {
		t.Error("meridian-control.service sets NoNewPrivileges, which breaks its sudo")
	}

	probe := rendered(t, output, "systemd/meridian-probe.service")
	for _, want := range []string{
		"User=deploy",
		"ExecStart=/etc/meridian/meridian-host probe --out /srv/meridian/status/host.json",
		"ProtectHome=read-only",
		"ReadWritePaths=/srv/meridian/status",
	} {
		if !strings.Contains(probe, want) {
			t.Errorf("meridian-probe.service does not contain %q:\n%s", want, probe)
		}
	}

	timer := rendered(t, output, "systemd/meridian-probe.timer")
	if !strings.Contains(timer, "Unit=meridian-probe.service") {
		t.Errorf("meridian-probe.timer does not name its service:\n%s", timer)
	}

	sudoers := rendered(t, output, "sudoers/meridian")
	if !strings.Contains(sudoers, "deploy ALL=(root) NOPASSWD: /etc/meridian/meridian-host exec") {
		t.Errorf("sudoers does not carry the one exact rule:\n%s", sudoers)
	}
}

func TestRenderUnits_RefusesAnUnknownPlaceholder(t *testing.T) {
	templates := t.TempDir()
	writeTemplate(t, templates, "systemd/bad.service", "[Service]\nExecStart=@NOPE@\n")
	output := t.TempDir()

	err := hostexec.RenderUnits(templates, output, renderValues())
	if err == nil {
		t.Fatal("RenderUnits accepted an unknown placeholder")
	}
	if !strings.Contains(err.Error(), "@NOPE@") {
		t.Errorf("error = %v, want it to name the placeholder", err)
	}
}

func TestRenderUnits_RefusesUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		values hostexec.UnitValues
	}{
		{"relative control dir", hostexec.UnitValues{ControlDir: "srv/control", DeployUser: "deploy", StatusDir: "/srv/status"}},
		{"unclean status dir", hostexec.UnitValues{ControlDir: "/srv/control", DeployUser: "deploy", StatusDir: "/srv/../status"}},
		{"control dir with a newline", hostexec.UnitValues{ControlDir: "/srv/control\nExecStart=/bin/sh", DeployUser: "deploy", StatusDir: "/srv/status"}},
		{"empty deploy user", hostexec.UnitValues{ControlDir: "/srv/control", DeployUser: "", StatusDir: "/srv/status"}},
		{"deploy user with a slash", hostexec.UnitValues{ControlDir: "/srv/control", DeployUser: "deploy/user", StatusDir: "/srv/status"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			templates := t.TempDir()
			writeTemplate(t, templates, "systemd/ok.service", "[Service]\nUser=@DEPLOY_USER@\n")
			if err := hostexec.RenderUnits(templates, t.TempDir(), test.values); err == nil {
				t.Fatal("RenderUnits accepted unsafe values")
			}
		})
	}
}

func TestRenderUnits_RefusesAMissingTemplateDirectory(t *testing.T) {
	if err := hostexec.RenderUnits(filepath.Join(t.TempDir(), "missing"), t.TempDir(), renderValues()); err == nil {
		t.Fatal("RenderUnits accepted a missing template directory")
	}
}

func writeTemplate(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // a temporary template, not a secret
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRenderUnitsMain_HelpAndMissingFlags(t *testing.T) {
	var stderr strings.Builder
	if code := hostexec.RenderUnitsMain([]string{"--help"}, &stderr); code != 0 {
		t.Errorf("--help exit = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "usage: meridian-host render-units") {
		t.Errorf("--help wrote %q", stderr.String())
	}
	stderr.Reset()
	if code := hostexec.RenderUnitsMain([]string{"--templates", t.TempDir()}, &stderr); code != 2 {
		t.Errorf("missing --output exit = %d, want 2", code)
	}
}

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_DispatchesProbeAndItsUsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Without --out the probe refuses before it runs anything, which is the one path through this
	// dispatch that needs no real systemd.
	if code := run([]string{"probe"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(probe) = %d, want 2\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--out") {
		t.Errorf("stderr = %q, want the probe's usage", stderr.String())
	}
}

func TestRun_RefusesAnUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"nonsense"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("run(nonsense) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `"nonsense"`) || !strings.Contains(stderr.String(), "probe") {
		t.Errorf("stderr = %q, want the unknown command and the usage", stderr.String())
	}
}

func TestRun_DispatchesExecAndItsFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// A missing host.json is enough: the executor refuses to run without trusted configuration, which
	// is the one path through this dispatch that needs no root and touches no real install.
	code := run([]string{"exec", "--host", filepath.Join(t.TempDir(), "host.json")}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(exec) = %d, want 1\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "meridian-host exec") {
		t.Errorf("stderr = %q, want the executor's refusal", stderr.String())
	}
}

func TestRun_PrintsTheUsageForHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(--help) = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "usage: meridian-host") {
		t.Errorf("stdout = %q, want the usage", stdout.String())
	}

	stdout.Reset()
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Errorf("run() = %d, want 2", code)
	}
}

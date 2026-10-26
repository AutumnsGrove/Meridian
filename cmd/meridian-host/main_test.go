package main

import (
	"bytes"
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

	code := run([]string{"exec"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("run(exec) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `"exec"`) || !strings.Contains(stderr.String(), "probe") {
		t.Errorf("stderr = %q, want the unknown command and the usage", stderr.String())
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

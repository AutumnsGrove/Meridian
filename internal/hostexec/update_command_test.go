package hostexec_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

func TestUpdateMain_HelpAndUnexpectedArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := hostexec.UpdateMain([]string{"--help"}, &stderr); code != 0 {
		t.Errorf("--help exit = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "usage: meridian-host update") {
		t.Errorf("--help wrote %q", stderr.String())
	}

	stderr.Reset()
	if code := hostexec.UpdateMain([]string{"extra"}, &stderr); code != 2 {
		t.Errorf("an extra argument exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Errorf("an extra argument wrote %q", stderr.String())
	}
}

func TestUpdateMain_RefusesWhenTheAllowlistCannotBeTrusted(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	var stderr bytes.Buffer
	code := hostexec.UpdateMain([]string{"--host", missing, "--targets", missing}, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "meridian-host update:") {
		t.Errorf("the refusal was not logged: %q", stderr.String())
	}
}

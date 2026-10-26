package doctor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/doctor"
)

func TestParseAllowlist_ReadsNamesAndActionsAndIgnoresTheRest(t *testing.T) {
	// The shape is the executor's; doctor only needs names and actions, and a newer executor may add
	// fields doctor has never heard of.
	body := `{
	  "targets": {
	    "alpha": {
	      "kind": "systemd",
	      "unit": "alpha.service",
	      "actions": ["start", "stop", "restart"],
	      "update": {"kind": "command", "cmd": ["/usr/bin/true"], "run_as": "autumn"},
	      "criticality": "low",
	      "something_new": true
	    }
	  }
	}`

	allow, err := doctor.ParseAllowlist([]byte(body))

	if err != nil {
		t.Fatalf("ParseAllowlist() = %v, want the allowlist", err)
	}
	target, found := allow.Targets["alpha"]
	if !found {
		t.Fatalf("targets = %+v, want alpha", allow.Targets)
	}
	if len(target.Actions) != 3 || target.Actions[2] != "restart" {
		t.Errorf("actions = %v, want the three in the file", target.Actions)
	}
}

func TestParseAllowlist_RefusesWhatItCannotUse(t *testing.T) {
	tests := []struct{ name, body string }{
		{name: "not JSON", body: "targets: ["},
		{name: "no targets key", body: `{"other": {}}`},
		{name: "null targets", body: `{"targets": null}`},
		{name: "a target with no actions", body: `{"targets": {"alpha": {"kind": "systemd"}}}`},
		{name: "an empty target name", body: `{"targets": {"": {"actions": ["restart"]}}}`},
		{name: "not an object at all", body: `[]`},
		{name: "empty", body: ``},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allow, err := doctor.ParseAllowlist([]byte(test.body))

			if err == nil {
				t.Fatalf("ParseAllowlist() = %+v, want an error", allow)
			}
			if !errors.Is(err, doctor.ErrInvalidAllowlist) {
				t.Errorf("error = %v, want ErrInvalidAllowlist", err)
			}
		})
	}
}

func TestParseAllowlist_RefusesAnOversizeFile(t *testing.T) {
	body := `{"targets": {"alpha": {"actions": ["restart"]}}}` + strings.Repeat(" ", doctor.MaxAllowlistSize)

	if _, err := doctor.ParseAllowlist([]byte(body)); !errors.Is(err, doctor.ErrInvalidAllowlist) {
		t.Fatalf("error = %v, want ErrInvalidAllowlist", err)
	}
}

func TestReadAllowlist_ReadsAStream(t *testing.T) {
	allow, err := doctor.ReadAllowlist(strings.NewReader(`{"targets": {"alpha": {"actions": ["restart"]}}}`))

	if err != nil {
		t.Fatalf("ReadAllowlist() = %v, want the allowlist", err)
	}
	if _, found := allow.Targets["alpha"]; !found {
		t.Fatalf("targets = %+v, want alpha", allow.Targets)
	}
}

// FuzzParseAllowlist holds the property the command line depends on: arbitrary bytes produce either
// an allowlist or the typed error, never a panic and never a half-parsed value.
func FuzzParseAllowlist(f *testing.F) {
	for _, seed := range []string{
		`{"targets": {}}`,
		`{"targets": {"alpha": {"actions": ["restart"]}}}`,
		`{"targets": {"alpha": {"actions": []}}}`,
		`{"targets": null}`,
		`{`,
		``,
		"\x00\xff",
		strings.Repeat("[", 1000),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, body string) {
		allow, err := doctor.ParseAllowlist([]byte(body))
		if err != nil {
			if !errors.Is(err, doctor.ErrInvalidAllowlist) {
				t.Fatalf("error = %v, want ErrInvalidAllowlist", err)
			}
			return
		}
		if allow == nil {
			t.Fatal("ParseAllowlist() returned no allowlist and no error")
		}
		for name, target := range allow.Targets {
			if name == "" || len(target.Actions) == 0 {
				t.Fatalf("accepted an unusable target: %q %+v", name, target)
			}
		}
	})
}

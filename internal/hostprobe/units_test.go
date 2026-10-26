package hostprobe_test

import (
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostprobe"
)

const targetsFile = `{
  "targets": {
    "alpha": {
      "kind": "systemd",
      "unit": "alpha.service",
      "actions": ["start", "stop", "restart"],
      "criticality": "low"
    },
    "beta": {
      "kind": "compose",
      "dir": "/home/someone/beta",
      "service": "beta",
      "actions": ["start", "stop", "restart", "update"],
      "update": {"kind": "unit", "unit": "beta-update.service"},
      "criticality": "medium"
    },
    "broken": {
      "kind": "systemd",
      "unit": "--help",
      "actions": ["start"],
      "criticality": "high"
    }
  }
}`

func TestUnits_WatchesTheUnionOfBothFiles(t *testing.T) {
	list := hostprobe.Units([]byte(`{"units": ["meridian-control.path", "gamma.timer"]}`), []byte(targetsFile))

	want := []string{"alpha.service", "gamma.timer", "meridian-control.path"}
	if strings.Join(list.Valid, ",") != strings.Join(want, ",") {
		t.Errorf("valid = %v, want %v (deduplicated and sorted)", list.Valid, want)
	}
	if wantDropped := []string{"--help"}; strings.Join(list.Dropped, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("dropped = %v, want %v", list.Dropped, wantDropped)
	}
	if len(list.Problems) != 0 {
		t.Errorf("problems = %v, want none", list.Problems)
	}
}

func TestUnits_DeduplicatesThreadsAcrossBothFiles(t *testing.T) {
	list := hostprobe.Units(
		[]byte(`{"units": ["alpha.service", "alpha.service"]}`),
		[]byte(`{"targets": {"a": {"kind": "systemd", "unit": "alpha.service"}}}`),
	)

	if want := []string{"alpha.service"}; strings.Join(list.Valid, ",") != strings.Join(want, ",") {
		t.Errorf("valid = %v, want %v", list.Valid, want)
	}
}

func TestUnits_ReportsFilesItCannotUseAndKeepsTheRest(t *testing.T) {
	list := hostprobe.Units(
		[]byte(`{"units": ["alpha.service"]}`),
		[]byte(`{"targets": {`),
	)

	if want := []string{"alpha.service"}; strings.Join(list.Valid, ",") != strings.Join(want, ",") {
		t.Errorf("valid = %v, want %v", list.Valid, want)
	}
	if len(list.Problems) != 1 {
		t.Fatalf("problems = %v, want one for the file that did not parse", list.Problems)
	}
	if !strings.Contains(list.Problems[0], "targets.json") {
		t.Errorf("problem %q does not name the file it came from", list.Problems[0])
	}
}

func TestUnits_RefusesTheWholeOfAFileItCannotReadStrictly(t *testing.T) {
	// observe.json is our own shape, so a misspelled key is an error rather than a probe that
	// silently watches nothing; its units are then not used at all.
	list := hostprobe.Units([]byte(`{"units": ["alpha.service"], "unexpected": 1}`), nil)

	if len(list.Valid) != 0 {
		t.Errorf("valid = %v, want nothing from a file that did not decode strictly", list.Valid)
	}
	if len(list.Problems) != 1 {
		t.Errorf("problems = %v, want one complaint", list.Problems)
	}
}

func TestUnits_HandlesEitherFileBeingAbsent(t *testing.T) {
	list := hostprobe.Units(nil, nil)

	if len(list.Valid) != 0 || len(list.Dropped) != 0 || len(list.Problems) != 0 {
		t.Errorf("Units(nil, nil) = %+v, want an empty list", list)
	}
}

func TestUnits_ReadsEveryUnitAComposeOrHostTargetNames(t *testing.T) {
	list := hostprobe.Units(nil, []byte(`{"targets": {
	  "unit": {"kind": "systemd", "unit": "one.service"},
	  "compose": {"kind": "compose", "dir": "/srv/two", "service": "two"},
	  "container": {"kind": "container", "service": "three"},
	  "host": {"kind": "host"}
	}}`))

	if want := []string{"one.service"}; strings.Join(list.Valid, ",") != strings.Join(want, ",") {
		t.Errorf("valid = %v, want only the target that names a unit", list.Valid)
	}
}

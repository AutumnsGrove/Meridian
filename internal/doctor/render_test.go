package doctor_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/autumnsgrove/meridian/internal/doctor"
)

// updateGoldens rewrites the pinned report. It is never set by scripts/check.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files in testdata/doctor")

// sampleReport is one of every kind of line: an ok, a warning and a failure, with and without a hint.
// It is built by hand so the golden does not depend on a live service.
func sampleReport() doctor.Report {
	return doctor.Report{
		Config: []doctor.Check{
			{Name: "allowlist", Code: doctor.CodeAllowlistOK, Detail: "2 targets", Level: doctor.OK},
			{
				Name:   "allowlist",
				Code:   doctor.CodeTargetUnused,
				Detail: `target "unused" in the allowlist is not in the config`,
				Level:  doctor.Warn,
				Hint:   "remove it from targets.json, or add the service that would use it",
			},
		},
		Services: []doctor.ServiceReport{
			{
				ID:   "alpha",
				Name: "Alpha",
				Checks: []doctor.Check{
					{Name: "environment", Code: doctor.CodeNoCredentials, Detail: "no credentials named", Level: doctor.OK},
					{Name: "url", Code: doctor.CodeURL, Detail: "https://alpha.example.org", Level: doctor.OK},
					{Name: "health", Code: doctor.CodeProbeOK, Detail: "http_status http://127.0.0.1:8096/health", Level: doctor.OK},
					{
						Name:   "version",
						Code:   "missing_field",
						Detail: "the field you asked for is missing",
						Level:  doctor.Fail,
						Hint:   "check the path against what the service actually sends",
					},
				},
			},
			{
				ID:   "beta",
				Name: "Beta",
				Checks: []doctor.Check{
					{Name: "service", Code: doctor.CodeDisabled, Detail: "disabled in the config, so nothing was probed", Level: doctor.Warn, Hint: "remove disabled: true when the service should be polled again"},
				},
			},
		},
	}
}

func TestRender_IsPinnedByAGoldenFile(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "doctor", "report.golden.txt")
	got := []byte(doctor.Render(sampleReport()))

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/doctor -update to create it)", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s does not match the report\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestRender_SummarisesTheCountsThatDecideTheExitCode(t *testing.T) {
	clean := doctor.Report{
		Config:   []doctor.Check{{Name: "allowlist", Code: doctor.CodeAllowlistOK, Detail: "1 target", Level: doctor.OK}},
		Services: []doctor.ServiceReport{{ID: "a", Name: "A", Checks: []doctor.Check{{Name: "health", Code: doctor.CodeProbeOK, Level: doctor.OK, Detail: "ok"}}}},
	}
	if got := doctor.Render(clean); !bytes.Contains([]byte(got), []byte("Everything checks out.")) {
		t.Errorf("a clean report says %q, want the all-clear", got)
	}

	warned := doctor.Report{Config: []doctor.Check{
		{Name: "allowlist", Code: doctor.CodeAllowlistNotChecked, Level: doctor.Warn, Detail: "not checked", Hint: "pass --allowlist"},
	}}
	if got := doctor.Render(warned); !bytes.Contains([]byte(got), []byte("No failures. 1 warning.")) {
		t.Errorf("one warning says %q", got)
	}

	failed := doctor.Report{Config: []doctor.Check{
		{Name: "config", Code: doctor.CodeInvalidConfig, Level: doctor.Fail, Detail: "broken", Hint: "fix it"},
	}}
	if got := doctor.Render(failed); !bytes.Contains([]byte(got), []byte("1 failing check.")) {
		t.Errorf("one failure says %q", got)
	}
}

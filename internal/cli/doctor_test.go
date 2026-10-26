package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/cli"
)

// doctorConfig writes a config file for the doctor tests and returns its path.
func doctorConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// doctorRun runs the command line with the given stdin and returns its exit code and output.
func doctorRun(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Main(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

const doctorService = `
listen: 127.0.0.1:8891
sections:
  - {id: test, title: Test}
poll: {interval: 10s, idle_interval: 1m, idle_after: 5m, timeout: 2s, refresh_min: 1s}
services:
  - id: alpha
    name: Alpha
    section: test
    icon: activity
    url: https://alpha.example.org
    probes:
      health: {kind: http_status, url: "%s/health", expect: [200]}
`

func healthyUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDoctor_ExplainsAnUnknownProbeKind(t *testing.T) {
	upstream := healthyUpstream(t)
	body := strings.Replace(doctorService, "%s", upstream.URL, 1)
	body = strings.Replace(body, "kind: http_status", "kind: nonsense", 1)
	path := doctorConfig(t, body)

	code, stdout, stderr := doctorRun(t, "", "doctor", "--config", path)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty: the report is the whole answer", stderr)
	}
	for _, want := range []string{"invalid_config", "nonsense", "hint:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report = %q, want it to contain %q", stdout, want)
		}
	}
}

func TestDoctor_ExplainsAControlTargetTheAllowlistDoesNotHave(t *testing.T) {
	upstream := healthyUpstream(t)
	body := strings.Replace(doctorService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart]}\n"
	path := doctorConfig(t, body)
	allowlist := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(allowlist, []byte(`{"targets": {"other": {"actions": ["restart"]}}}`), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	code, stdout, stderr := doctorRun(t, "", "doctor", "--config", path, "--allowlist", allowlist)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr %q, stdout %q)", code, stderr, stdout)
	}
	for _, want := range []string{"target_missing", "target \"other\" in the allowlist is not in the config"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report = %q, want it to contain %q", stdout, want)
		}
	}
}

func TestDoctor_ExplainsAURLThatDoesNotAnswer(t *testing.T) {
	upstream := healthyUpstream(t)
	address := upstream.URL
	upstream.Close()
	path := doctorConfig(t, strings.Replace(doctorService, "%s", address, 1))

	code, stdout, _ := doctorRun(t, "", "doctor", "--config", path, "--allowlist", "-")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout %q)", code, stdout)
	}
	if !strings.Contains(stdout, "fail") || !strings.Contains(stdout, "hint:") {
		t.Errorf("report = %q, want a failing check with a hint", stdout)
	}
}

func TestDoctor_PassesAHealthyConfigAndSaysSo(t *testing.T) {
	upstream := healthyUpstream(t)
	body := strings.Replace(doctorService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart]}\n"
	path := doctorConfig(t, body)

	code, stdout, stderr := doctorRun(t, `{"targets": {"alpha": {"actions": ["restart"]}}}`, "doctor", "--config", path, "--allowlist", "-")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	for _, want := range []string{"Everything checks out.", "probe_ok", "control_ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report = %q, want it to contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "fail") {
		t.Errorf("report = %q, want no failures", stdout)
	}
}

func TestDoctor_WithoutAnAllowlistWarnsInsteadOfFailing(t *testing.T) {
	upstream := healthyUpstream(t)
	body := strings.Replace(doctorService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart]}\n"
	path := doctorConfig(t, body)

	code, stdout, stderr := doctorRun(t, "", "doctor", "--config", path)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0: an unchecked allowlist is a warning (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "allowlist_not_checked") {
		t.Errorf("report = %q, want the allowlist warning", stdout)
	}
}

func TestDoctor_NeverPrintsACredentialValue(t *testing.T) {
	upstream := healthyUpstream(t)
	const secret = "distinctive-doctor-secret-value"
	body := strings.Replace(doctorService, "%s", upstream.URL, 1)
	body = strings.Replace(body, "expect: [200]}", "expect: [200], auth: {bearer_env: MERIDIAN_DOCTOR_SECRET}}", 1)
	path := doctorConfig(t, body)
	t.Setenv("MERIDIAN_DOCTOR_SECRET", secret)

	code, stdout, _ := doctorRun(t, `{"targets": {"alpha": {"actions": ["restart"]}}}`, "doctor", "--config", path, "--allowlist", "-")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout %q)", code, stdout)
	}
	if strings.Contains(stdout, secret) {
		t.Fatalf("the report printed the credential value: %q", stdout)
	}
	if !strings.Contains(stdout, "MERIDIAN_DOCTOR_SECRET") {
		t.Errorf("report = %q, want it to name the variable", stdout)
	}
}

func TestDoctor_RefusesAnAllowlistItCannotRead(t *testing.T) {
	upstream := healthyUpstream(t)
	path := doctorConfig(t, strings.Replace(doctorService, "%s", upstream.URL, 1))

	code, stdout, _ := doctorRun(t, "not json", "doctor", "--config", path, "--allowlist", "-")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout %q)", code, stdout)
	}
	if !strings.Contains(stdout, "invalid_allowlist") {
		t.Errorf("report = %q, want invalid_allowlist", stdout)
	}
}

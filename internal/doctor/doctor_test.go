package doctor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/doctor"
	"github.com/autumnsgrove/meridian/internal/probe"
)

// parseConfig is the test's only way in: doctor takes a config that already validated, exactly as the
// command line does after loading one.
func parseConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, _, err := config.Parse([]byte(body))
	if err != nil {
		t.Fatalf("the test config does not validate: %v", err)
	}
	return cfg
}

func liveService(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func testEnv() probe.Env {
	return probe.Env{
		Fetch:   probe.NewHTTPFetcher(func(string) string { return "" }, "doctor", 1<<20),
		Resolve: probe.NewDNSResolver(),
		Now:     time.Now,
	}
}

func reportFor(t *testing.T, body string, allow *doctor.Allowlist) doctor.Report {
	t.Helper()
	return doctor.Run(context.Background(), parseConfig(t, body), testEnv(), allow)
}

func serviceChecks(report doctor.Report) []doctor.Check {
	if len(report.Services) == 0 {
		return nil
	}
	return report.Services[0].Checks
}

func findCheck(checks []doctor.Check, name string) (doctor.Check, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return doctor.Check{}, false
}

const oneService = `
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

func configWith(upstreamURL string) string {
	return strings.Replace(oneService, "%s", upstreamURL, 1)
}

func TestRun_AHealthyServicePassesEveryCheck(t *testing.T) {
	upstream := liveService(t)
	report := reportFor(t, configWith(upstream.URL), nil)

	checks := serviceChecks(report)
	for _, name := range []string{"environment", "url", "health"} {
		check, found := findCheck(checks, name)
		if !found {
			t.Fatalf("no %s check in %+v", name, checks)
		}
		if check.Level != doctor.OK {
			t.Errorf("%s = %s (%s), want ok", name, check.Level, check.Detail)
		}
	}
	if report.Failed() {
		t.Fatalf("a healthy service produced a failure: %+v", report)
	}
}

func TestRun_AURLThatDoesNotAnswerIsAFailureWithAHint(t *testing.T) {
	upstream := liveService(t)
	address := upstream.URL
	upstream.Close() // the portrait of "the service is down"

	report := reportFor(t, configWith(address), nil)

	health, found := findCheck(serviceChecks(report), "health")
	if !found {
		t.Fatal("no health check at all")
	}
	if health.Level != doctor.Fail {
		t.Fatalf("health = %s, want fail", health.Level)
	}
	if health.Code == "" || health.Code == doctor.CodeProbeOK {
		t.Errorf("health code = %q, want the probe's reason code", health.Code)
	}
	if health.Hint == "" {
		t.Error("a failure without a hint leaves the reader stuck")
	}
	if !report.Failed() {
		t.Error("Report.Failed() = false, want true")
	}
}

func TestRun_AControlTargetMissingFromTheAllowlistFails(t *testing.T) {
	upstream := liveService(t)
	body := strings.Replace(oneService, "%s", upstream.URL, 1) +
		"    control: {target: missing-target, actions: [restart]}\n"
	allow := &doctor.Allowlist{Targets: map[string]doctor.Target{
		"other": {Actions: []string{"restart"}},
	}}

	report := reportFor(t, body, allow)

	control, found := findCheck(serviceChecks(report), "control")
	if !found {
		t.Fatal("no control check")
	}
	if control.Code != doctor.CodeTargetMissing || control.Level != doctor.Fail {
		t.Fatalf("control = %+v, want target_missing and Fail", control)
	}
	// The unused target is a warning, not a failure: the executor may have more targets than the
	// config uses, and that is not an error.
	var unused bool
	for _, check := range report.Config {
		if check.Code == doctor.CodeTargetUnused {
			unused = check.Level == doctor.Warn
		}
	}
	if !unused {
		t.Errorf("the allowlist's unused target was not reported as a warning: %+v", report.Config)
	}
}

func TestRun_AnActionTheAllowlistRefusesFails(t *testing.T) {
	upstream := liveService(t)
	body := strings.Replace(oneService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart, update]}\n"
	allow := &doctor.Allowlist{Targets: map[string]doctor.Target{
		"alpha": {Actions: []string{"restart"}},
	}}

	report := reportFor(t, body, allow)

	var found bool
	for _, check := range serviceChecks(report) {
		if check.Code == doctor.CodeActionNotAllowed {
			found = true
			if check.Level != doctor.Fail || !strings.Contains(check.Detail, "update") {
				t.Errorf("action check = %+v, want a Fail naming update", check)
			}
		}
	}
	if !found {
		t.Fatalf("the refused action was not reported: %+v", serviceChecks(report))
	}
}

func TestRun_ADisabledServiceIsAWarningAndNothingIsProbed(t *testing.T) {
	upstream := liveService(t)
	body := strings.Replace(oneService, "%s", upstream.URL, 1) + "    disabled: true\n"

	report := reportFor(t, body, nil)

	checks := serviceChecks(report)
	if len(checks) != 1 || checks[0].Code != doctor.CodeDisabled || checks[0].Level != doctor.Warn {
		t.Fatalf("checks = %+v, want one disabled warning and no probes", checks)
	}
	if report.Failed() {
		t.Error("a deliberately disabled service failed the report")
	}
}

func TestRun_AMissingCredentialVariableIsAFailureThatNamesItAndNeverItsValue(t *testing.T) {
	upstream := liveService(t)
	secretName := "MERIDIAN_DOCTOR_TEST_SECRET"
	body := strings.Replace(oneService, "%s", upstream.URL, 1)
	body = strings.Replace(body, "expect: [200]}", "expect: [200], auth: {bearer_env: "+secretName+"}}", 1)
	t.Setenv(secretName, "")

	report := reportFor(t, body, nil)

	check, found := findCheck(serviceChecks(report), "environment")
	if !found || check.Code != probe.CodeEnvMissing || check.Level != doctor.Fail {
		t.Fatalf("environment check = %+v, want a Fail with %q", check, probe.CodeEnvMissing)
	}
	if !strings.Contains(check.Detail, secretName) {
		t.Errorf("detail = %q, want it to name the variable", check.Detail)
	}

	rendered := doctor.Render(report)
	if !strings.Contains(rendered, secretName) {
		t.Errorf("the report does not name the variable: %q", rendered)
	}

	// And when it is set, the value never appears either.
	t.Setenv(secretName, "distinctive-doctor-value")
	report = reportFor(t, body, nil)
	if rendered := doctor.Render(report); strings.Contains(rendered, "distinctive-doctor-value") {
		t.Errorf("the report printed a secret: %q", rendered)
	}
}

func TestRun_ABadClickThroughURLIsAFailure(t *testing.T) {
	upstream := liveService(t)
	// Validation refuses a URL without a scheme, so this one is built by hand: the check exists for
	// a config that loaded under a looser build, and it must not depend on validation to be tested.
	cfg := &config.Config{
		Poll: config.Poll{Timeout: config.Duration(2 * time.Second)},
		Services: []config.Service{{
			ID: "alpha", Name: "Alpha", URL: "not-a-url",
			Probes: config.Probes{Health: config.Probe{
				Kind: "http_status", URL: upstream.URL + "/health", Expect: []int{200},
			}},
		}},
	}

	report := doctor.Run(context.Background(), cfg, testEnv(), nil)

	check, found := findCheck(serviceChecks(report), "url")
	if !found || check.Code != doctor.CodeBadURL || check.Level != doctor.Fail {
		t.Fatalf("url check = %+v, want bad_url and Fail", check)
	}
}

func TestInvalidConfig_ExplainsEveryProblem(t *testing.T) {
	_, _, err := config.Parse([]byte(`
listen: 127.0.0.1:8891
sections:
  - {id: test, title: Test}
services:
  - id: alpha
    name: Alpha
    section: test
    icon: activity
    probes:
      health: {kind: nonsense, url: "http://127.0.0.1:1/health"}
`))
	if err == nil {
		t.Fatal("the test config validated, so there is nothing to explain")
	}

	report := doctor.InvalidConfig(err)

	if len(report.Config) == 0 {
		t.Fatal("InvalidConfig() produced no checks")
	}
	for _, check := range report.Config {
		if check.Code != doctor.CodeInvalidConfig || check.Level != doctor.Fail || check.Hint == "" {
			t.Errorf("check = %+v, want an invalid_config Fail with a hint", check)
		}
		if !strings.Contains(check.Detail, "nonsense") {
			t.Errorf("detail = %q, want it to name the unknown kind", check.Detail)
		}
	}
	if !report.Failed() {
		t.Error("Report.Failed() = false, want true")
	}
}

func TestInvalidConfig_AnUnreadableFileIsOneCheck(t *testing.T) {
	report := doctor.InvalidConfig(assertiveError("config: open /nope: no such file"))

	if len(report.Config) != 1 {
		t.Fatalf("checks = %+v, want one", report.Config)
	}
	if !strings.Contains(report.Config[0].Detail, "/nope") {
		t.Errorf("detail = %q, want the path", report.Config[0].Detail)
	}
}

func TestInvalidAllowlist_IsAFailureWithAnInstruction(t *testing.T) {
	report := doctor.InvalidAllowlist(doctor.ErrInvalidAllowlist)

	if len(report.Config) != 1 || report.Config[0].Level != doctor.Fail || report.Config[0].Hint == "" {
		t.Fatalf("report = %+v, want one Fail with a hint", report.Config)
	}
}

func TestRun_WithoutAnAllowlistTheControlSectionSaysItWasNotChecked(t *testing.T) {
	upstream := liveService(t)
	body := strings.Replace(oneService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart]}\n"

	report := reportFor(t, body, nil)

	warning, found := findCheck(report.Config, "allowlist")
	if !found || warning.Code != doctor.CodeAllowlistNotChecked || warning.Level != doctor.Warn {
		t.Fatalf("config checks = %+v, want an allowlist_not_checked warning", report.Config)
	}
	if report.Failed() {
		t.Error("an unchecked allowlist failed the report; it is a warning, not a failure")
	}
}

func TestRun_AnAllowlistThatMatchesPasses(t *testing.T) {
	upstream := liveService(t)
	body := strings.Replace(oneService, "%s", upstream.URL, 1) +
		"    control: {target: alpha, actions: [restart]}\n"
	allow := &doctor.Allowlist{Targets: map[string]doctor.Target{
		"alpha": {Actions: []string{"restart", "update"}},
	}}

	report := reportFor(t, body, allow)

	if check, found := findCheck(report.Config, "allowlist"); !found || check.Code != doctor.CodeAllowlistOK {
		t.Fatalf("config checks = %+v, want allowlist_ok", report.Config)
	}
	if check, found := findCheck(serviceChecks(report), "control"); !found || check.Code != doctor.CodeControlOK {
		t.Fatalf("service checks = %+v, want control_ok", serviceChecks(report))
	}
}

// assertiveError is a plain error for the cases that only need the message.
type assertiveError string

func (err assertiveError) Error() string { return string(err) }

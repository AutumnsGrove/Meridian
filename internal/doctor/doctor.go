// Package doctor explains a config against the world, read only: it runs each probe once, compares
// the control blocks with the host's allowlist, and never writes a file or asks the control plane for
// anything (docs/agent/05-cli-and-deploy.md, T21; decision #15 keeps /etc/meridian out of the image).
package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
)

// Level says what a check means for the exit code: only Fail makes doctor exit non-zero.
type Level string

const (
	OK   Level = "ok"
	Warn Level = "warn"
	Fail Level = "fail"
)

// The codes doctor adds to the probe's own reason codes. A probe failure keeps the probe's code, so
// the log, the page and the doctor line all name the same cause.
const (
	// CodeProbeOK is a health probe that answered as the config expected.
	CodeProbeOK = "probe_ok"
	// CodeVitalOK is a vital that produced a value.
	CodeVitalOK = "vital_ok"
	// CodeURL is a click-through URL that parses as http or https.
	CodeURL = "url_ok"
	// CodeBadURL is a URL a browser could not open.
	CodeBadURL = "bad_url"
	// CodeInvalidConfig is a config that did not load or validate.
	CodeInvalidConfig = "invalid_config"
	// CodeInvalidAllowlist is an allowlist file that could not be read or parsed.
	CodeInvalidAllowlist = "invalid_allowlist"
	// CodeAllowlistNotChecked is the warning that stands in for the check when nobody passed one.
	CodeAllowlistNotChecked = "allowlist_not_checked"
	// CodeAllowlistOK is an allowlist that parsed.
	CodeAllowlistOK = "allowlist_ok"
	// CodeTargetMissing is a control block whose target the allowlist does not have.
	CodeTargetMissing = "target_missing"
	// CodeActionNotAllowed is an action the allowlist refuses on that target.
	CodeActionNotAllowed = "action_not_allowed"
	// CodeControlOK is a control block the allowlist fully permits.
	CodeControlOK = "control_ok"
	// CodeTargetUnused is an allowlist target no config mentions.
	CodeTargetUnused = "target_unused"
	// CodeEnvPresent and CodeNoCredentials describe credential variables.
	CodeEnvPresent    = "env_present"
	CodeNoCredentials = "no_credentials"
	// CodeDisabled is a service the config turns off on purpose.
	CodeDisabled = "disabled"
)

// Check is one line of the report: what was looked at, what was found, and what to do about it.
type Check struct {
	// Name is the thing checked: "health", a vital's label, "environment", "control", "allowlist".
	Name string
	// Code is the closed-vocabulary reason, safe to grep for and to feed to a bug report.
	Code string
	// Detail is one line: what doctor saw, never a secret and never a raw stack trace.
	Detail string
	// Level is OK, Warn or Fail.
	Level Level
	// Hint is one sentence of what to do next; every Warn and Fail has one.
	Hint string
}

// ServiceReport is one service's checks, in the order the config lists them.
type ServiceReport struct {
	ID     string
	Name   string
	Checks []Check
}

// Report is everything doctor found: the config-level checks, then one block per service.
type Report struct {
	Config   []Check
	Services []ServiceReport
}

// Failed reports whether anything in the report is a Fail, which is what decides the exit code.
func (report Report) Failed() bool {
	for _, check := range report.Config {
		if check.Level == Fail {
			return true
		}
	}
	for _, service := range report.Services {
		for _, check := range service.Checks {
			if check.Level == Fail {
				return true
			}
		}
	}
	return false
}

// defaultProbeTimeout bounds a probe when the config's poll timeout is missing, which validation
// normally prevents; doctor must not hang on a config it is explaining.
const defaultProbeTimeout = 5 * time.Second

// Run builds the report. Every probe runs once, here, with the same runners the page uses, so doctor
// and the page cannot disagree about whether a service answers (docs/agent/05, T21). It is read only:
// it writes no file and asks the control plane for nothing.
func Run(ctx context.Context, cfg *config.Config, env probe.Env, allow *Allowlist) Report {
	report := Report{Config: configChecks(cfg, allow)}
	for index := range cfg.Services {
		service := &cfg.Services[index]
		report.Services = append(report.Services, ServiceReport{
			ID:     service.ID,
			Name:   service.Name,
			Checks: serviceChecks(ctx, cfg, *service, env, allow),
		})
	}
	return report
}

// InvalidConfig turns a config that did not load into a report, so the same renderer explains a
// broken file instead of doctor refusing to say anything. Errors that are not parsed problems (an
// unreadable file, say) become one check.
func InvalidConfig(err error) Report {
	var problems config.Problems
	if !errors.As(err, &problems) {
		return Report{Config: []Check{{
			Name:   "config",
			Code:   CodeInvalidConfig,
			Detail: err.Error(),
			Level:  Fail,
			Hint:   hints[CodeInvalidConfig],
		}}}
	}
	checks := make([]Check, len(problems))
	for index, problem := range problems {
		checks[index] = Check{
			Name:   "config",
			Code:   CodeInvalidConfig,
			Detail: problem.String(),
			Level:  Fail,
			Hint:   hints[string(problem.Code)],
		}
		if checks[index].Hint == "" {
			checks[index].Hint = hints[CodeInvalidConfig]
		}
	}
	return Report{Config: checks}
}

// InvalidAllowlist turns an unreadable allowlist into a report. Doctor keeps going without it: the
// control checks then say they were not checked, which is a warning, not a failure.
func InvalidAllowlist(err error) Report {
	return Report{Config: []Check{{
		Name:   "allowlist",
		Code:   CodeInvalidAllowlist,
		Detail: err.Error(),
		Level:  Fail,
		Hint:   "pass --allowlist with the output of `meridian-host targets`, or a file with that shape",
	}}}
}

func configChecks(cfg *config.Config, allow *Allowlist) []Check {
	if allow == nil {
		return []Check{{
			Name:   "allowlist",
			Code:   CodeAllowlistNotChecked,
			Detail: "the control allowlist was not checked",
			Level:  Warn,
			Hint:   "pass --allowlist with the output of `meridian-host targets`",
		}}
	}

	checks := []Check{{
		Name:   "allowlist",
		Code:   CodeAllowlistOK,
		Detail: fmt.Sprintf("%d targets", len(allow.Targets)),
		Level:  OK,
	}}

	used := make(map[string]bool, len(cfg.Services))
	for index := range cfg.Services {
		service := &cfg.Services[index]
		if service.Control != nil {
			used[service.Control.Target] = true
		}
	}
	names := make([]string, 0, len(allow.Targets))
	for name := range allow.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if used[name] {
			continue
		}
		checks = append(checks, Check{
			Name:   "allowlist",
			Code:   CodeTargetUnused,
			Detail: fmt.Sprintf("target %q in the allowlist is not in the config", name),
			Level:  Warn,
			Hint:   hints[CodeTargetUnused],
		})
	}
	return checks
}

func serviceChecks(ctx context.Context, cfg *config.Config, service config.Service, env probe.Env, allow *Allowlist) []Check {
	if service.Disabled {
		return []Check{{
			Name:   "service",
			Code:   CodeDisabled,
			Detail: "disabled in the config, so nothing was probed",
			Level:  Warn,
			Hint:   "remove disabled: true when the service should be polled again",
		}}
	}

	timeout := time.Duration(cfg.Poll.Timeout)
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	checks := envChecks(service)
	if service.URL != "" {
		checks = append(checks, urlCheck(service.URL))
	}
	checks = append(checks, healthCheck(ctx, timeout, env, service.Probes.Health))
	for index := range service.Probes.Vitals {
		vital := &service.Probes.Vitals[index]
		checks = append(checks, vitalCheck(ctx, timeout, env, *vital))
	}
	if service.Control != nil {
		checks = append(checks, controlChecks(service, allow)...)
	}
	return checks
}

// credentialNames lists the environment variables every probe of a service reads, in config order,
// without duplicates. Only names ever leave this function.
func credentialNames(service config.Service) []string {
	specs := make([]config.Probe, 0, 1+len(service.Probes.Vitals))
	specs = append(specs, service.Probes.Health)
	specs = append(specs, service.Probes.Vitals...)

	seen := make(map[string]bool, len(specs))
	names := make([]string, 0, len(specs))
	for index := range specs {
		spec := &specs[index]
		if spec.Auth == nil {
			continue
		}
		referenced := append([]string{}, spec.Auth.BasicEnv...)
		if spec.Auth.BearerEnv != "" {
			referenced = append(referenced, spec.Auth.BearerEnv)
		}
		for _, name := range referenced {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func envChecks(service config.Service) []Check {
	names := credentialNames(service)
	if len(names) == 0 {
		return []Check{{Name: "environment", Code: CodeNoCredentials, Level: OK, Detail: "no credentials named"}}
	}

	checks := make([]Check, 0, len(names))
	for _, name := range names {
		if os.Getenv(name) != "" {
			checks = append(checks, Check{Name: "environment", Code: CodeEnvPresent, Level: OK, Detail: name + " is set"})
			continue
		}
		checks = append(checks, Check{
			Name:   "environment",
			Code:   probe.CodeEnvMissing,
			Detail: name + " is not set",
			Level:  Fail,
			Hint:   hints[probe.CodeEnvMissing],
		})
	}
	return checks
}

func urlCheck(raw string) Check {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Check{
			Name:   "url",
			Code:   CodeBadURL,
			Detail: "the click-through URL is not an http or https address",
			Level:  Fail,
			Hint:   hints[CodeBadURL],
		}
	}
	// Redacted keeps a password out of the report even if the config carries one in the URL.
	return Check{Name: "url", Code: CodeURL, Level: OK, Detail: parsed.Redacted()}
}

func healthCheck(ctx context.Context, timeout time.Duration, env probe.Env, spec config.Probe) Check {
	runner, found := probe.HealthRunners[spec.Kind]
	if !found {
		return Check{
			Name:   "health",
			Code:   probe.CodeBadValue,
			Detail: "no runner for kind " + spec.Kind,
			Level:  Fail,
			Hint:   hints[probe.CodeBadValue],
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	health := runner(probeCtx, env, spec)
	if health.Status != probe.StatusOK {
		return failure("health", health.Code)
	}
	return Check{Name: "health", Code: CodeProbeOK, Level: OK, Detail: describe(spec)}
}

func vitalCheck(ctx context.Context, timeout time.Duration, env probe.Env, spec config.Probe) Check {
	name := spec.Label
	if name == "" {
		name = spec.Kind
	}

	runner, found := probe.VitalRunners[spec.Kind]
	if !found {
		return Check{
			Name:   name,
			Code:   probe.CodeBadValue,
			Detail: "no runner for kind " + spec.Kind,
			Level:  Fail,
			Hint:   hints[probe.CodeBadValue],
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	value, err := runner(probeCtx, env, spec)
	if err != nil {
		return failure(name, probe.CodeOf(err))
	}
	return Check{Name: name, Code: CodeVitalOK, Level: OK, Detail: name + ": " + value}
}

func controlChecks(service config.Service, allow *Allowlist) []Check {
	if allow == nil {
		return nil // the config-level warning already says the allowlist was not checked
	}

	target, found := allow.Targets[service.Control.Target]
	if !found {
		return []Check{{
			Name:   "control",
			Code:   CodeTargetMissing,
			Detail: "target " + service.Control.Target + " is not in the allowlist",
			Level:  Fail,
			Hint:   hints[CodeTargetMissing],
		}}
	}

	permitted := make(map[string]bool, len(target.Actions))
	for _, action := range target.Actions {
		permitted[action] = true
	}

	checks := make([]Check, 0, len(service.Control.Actions))
	for _, action := range service.Control.Actions {
		if permitted[action] {
			continue
		}
		checks = append(checks, Check{
			Name:   "control",
			Code:   CodeActionNotAllowed,
			Detail: "the allowlist does not permit " + action + " on " + service.Control.Target,
			Level:  Fail,
			Hint:   hints[CodeActionNotAllowed],
		})
	}
	if len(checks) > 0 {
		return checks
	}
	return []Check{{
		Name:   "control",
		Code:   CodeControlOK,
		Level:  OK,
		Detail: service.Control.Target + " allows " + strings.Join(service.Control.Actions, ", "),
	}}
}

// failure builds a Fail from a probe's reason code, so the sentence and the hint come from the same
// closed vocabulary the page uses.
func failure(name, code string) Check {
	detail := probe.Reason(code)
	if detail == "" {
		detail = "the probe did not give a reason this build knows (" + code + ")"
	}
	return Check{Name: name, Code: code, Detail: detail, Level: Fail, Hint: hintFor(code)}
}

// describe names what was probed, so two rows of the same service are tellable apart.
func describe(spec config.Probe) string {
	target := spec.Kind
	switch {
	case spec.URL != "":
		target = spec.URL
	case spec.Qname != "":
		target = spec.Qname
	case spec.Unit != "":
		target = spec.Unit
	case spec.Addr != "":
		target = spec.Addr
	case spec.Path != "":
		target = spec.Path
	}
	return spec.Kind + " " + target
}

// hintFor is the one sentence a failure carries. A code without its own sentence still gets one: a
// Fail without a hint is a dead end for whoever is reading it.
func hintFor(code string) string {
	if hint := hints[code]; hint != "" {
		return hint
	}
	return "the reason code above is the same one the page and the log carry; fix that and run doctor again"
}

// hints is the table of next steps, keyed by the code that produced them (docs/agent/05, T21). It is
// deliberately generic in one direction: it may name a decision, never a service.
var hints = map[string]string{
	probe.CodeBadStatus:       "check the expected statuses; a service behind a proxy may answer 403 unless it gets its own Host header, and probe URLs normally use localhost",
	probe.CodeRefused:         "is anything listening on that port from inside the container (is network_mode: host set)?",
	probe.CodeTimeout:         "the service may still be starting, or poll.timeout is too short",
	probe.CodeUnreachable:     "check the address, and that the container can route to it",
	probe.CodeBadJSON:         "check the path: the answer has to be JSON this build can read",
	probe.CodeMissingField:    "check the path against what the service actually sends",
	probe.CodeUnexpectedValue: "compare the value the config expects with the one the service reports",
	probe.CodeUnexpectedType:  "the value is there but not a shape this build formats",
	probe.CodeBodyTooLarge:    "point the probe at a smaller endpoint, the answer is over the body cap",
	probe.CodeEnvMissing:      "set the variable in the container's environment",
	probe.CodeHostUnavailable: "check that host_status.file exists and is readable from the container",
	probe.CodeHostStale:       "check that meridian-probe.timer is running on the host",
	probe.CodeUnitInactive:    "the unit is installed but not running; start it, or fix its own failure",
	probe.CodeUnitNotReported: "add the unit to observe.json on the host, or add the target to targets.json",
	probe.CodeDNSRcode:        "check the name and the expected response code",
	probe.CodeOff:             "the service reports this as off; if that is wrong, look at the service",
	probe.CodeBadValue:        "a value the service reported is not usable; check the config that reads it",
	CodeBadURL:                "a click-through URL has to be absolute and http or https",
	CodeInvalidConfig:         "fix the problem named at the start of the line, then run doctor again",
	CodeTargetMissing:         "add the target to /etc/meridian/targets.json on the host",
	CodeActionNotAllowed:      "add the action to that target in /etc/meridian/targets.json, or drop it from the config",
	CodeTargetUnused:          "remove it from targets.json, or add the service that would use it",
}

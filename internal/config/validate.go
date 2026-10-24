package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	lucidePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	imagePattern  = regexp.MustCompile(`^icons/[A-Za-z0-9._-]+\.(png|webp|jpg|jpeg|gif)$`)
	envPattern    = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	headerPattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

// controlActions is the closed set a service may offer (documents/CONTROL.md).
var controlActions = map[string]bool{"restart": true, "start": true, "stop": true, "update": true}

// Validate checks a parsed config and returns everything wrong with it at once, plus the things
// that load but will not do what their author meant.
func Validate(parsed *Config) (Problems, []Warning) {
	validator := &validator{}
	validator.validateListen(parsed)
	validator.validateServer(parsed)
	validator.validateSections(parsed)
	validator.validatePoll(parsed)
	validator.validateHostStatus(parsed)
	validator.validateSearch(parsed)
	validator.validateControlSettings(parsed)
	validator.validateServices(parsed)
	return validator.problems, validator.warnings
}

type validator struct {
	problems Problems
	warnings []Warning
}

func (validator *validator) fail(path string, code Code, format string, args ...any) {
	validator.problems = append(validator.problems, Problem{
		Path:    path,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
}

func (validator *validator) warn(path string, code Code, format string, args ...any) {
	validator.warnings = append(validator.warnings, Warning{
		Path:    path,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
}

func (validator *validator) validateListen(parsed *Config) {
	host, port, err := net.SplitHostPort(parsed.Listen)
	if err != nil || host == "" {
		validator.fail("listen", CodeListen, "listen must be host:port, for example 127.0.0.1:8891")
		return
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		validator.fail("listen", CodeListen, "port must be a number between 1 and 65535")
	}
}

func (validator *validator) validateServer(parsed *Config) {
	for i, host := range parsed.Server.AllowedHosts {
		if !validHostEntry(host) {
			validator.fail(fmt.Sprintf("server.allowed_hosts[%d]", i), CodeAllowedHost,
				"%q is not a host or host:port", host)
		}
	}
	if parsed.Server.PublicURL != "" && !validHTTPURL(parsed.Server.PublicURL) {
		validator.fail("server.public_url", CodePublicURL, "public_url must be an http or https URL")
	}
}

func (validator *validator) validateSections(parsed *Config) {
	if len(parsed.Sections) == 0 {
		validator.fail("sections", CodeSectionsMissing, "at least one section is required")
		return
	}
	seen := make(map[string]bool, len(parsed.Sections))
	for i, section := range parsed.Sections {
		path := fmt.Sprintf("sections[%d]", i)
		if !slugPattern.MatchString(section.ID) {
			validator.fail(path+".id", CodeSectionIDInvalid, "%q is not a slug like my-app", section.ID)
		}
		if seen[section.ID] {
			validator.fail(path+".id", CodeSectionIDDuplicate, "section %q is declared twice", section.ID)
		}
		seen[section.ID] = true
		if strings.TrimSpace(section.Title) == "" {
			validator.fail(path+".title", CodeSectionTitleMissing, "title is required")
		}
	}
}

func (validator *validator) validatePoll(parsed *Config) {
	interval := time.Duration(parsed.Poll.Interval)
	timeout := time.Duration(parsed.Poll.Timeout)
	idle := time.Duration(parsed.Poll.IdleInterval)
	if interval < 5*time.Second {
		validator.fail("poll.interval", CodePollIntervalRange, "interval must be at least 5s")
	}
	if timeout < 500*time.Millisecond || timeout > 30*time.Second {
		validator.fail("poll.timeout", CodePollTimeoutRange, "timeout must be between 500ms and 30s")
	}
	if timeout >= interval {
		validator.fail("poll.timeout", CodePollTimeoutOrder, "timeout must be shorter than interval")
	}
	if idle < interval {
		validator.fail("poll.idle_interval", CodePollIdleIntervalRange, "idle_interval must be at least interval")
	}
}

func (validator *validator) validateHostStatus(parsed *Config) {
	if parsed.HostStatus == nil {
		return
	}
	if strings.TrimSpace(parsed.HostStatus.File) == "" {
		validator.fail("host_status.file", CodeHostStatusFile, "host_status needs the snapshot file path")
	}
	if time.Duration(parsed.HostStatus.MaxAge) <= 0 {
		validator.fail("host_status.max_age", CodeHostStatusAge, "max_age must be a positive duration")
	}
}

func (validator *validator) validateSearch(parsed *Config) {
	if len(parsed.Search.Buttons) > 4 {
		validator.fail("search.buttons", CodeSearchTooMany, "at most four search buttons are supported")
	}
	for i, button := range parsed.Search.Buttons {
		path := fmt.Sprintf("search.buttons[%d]", i)
		if strings.TrimSpace(button.Label) == "" {
			validator.fail(path+".label", CodeSearchLabelMissing, "label is required")
		}
		if strings.Count(button.URLTemplate, "{q}") != 1 {
			validator.fail(path+".url_template", CodeSearchPlaceholder, "url_template must contain {q} exactly once")
			continue
		}
		if !validHTTPURL(strings.Replace(button.URLTemplate, "{q}", "query", 1)) {
			validator.fail(path+".url_template", CodeSearchURLInvalid, "url_template must be an http or https URL")
		}
	}
}

func (validator *validator) validateControlSettings(parsed *Config) {
	control := parsed.Control
	if control.QueueDepth < 1 {
		validator.fail("control.queue_depth", CodeControlQueueDepthRange, "queue_depth must be at least 1")
	}
	if time.Duration(control.ActionTimeout) <= 0 {
		validator.fail("control.action_timeout", CodeControlActionTimeoutRange, "action_timeout must be positive")
	}
	if control.AuditLines < 1 {
		validator.fail("control.audit_lines", CodeControlAuditLinesRange, "audit_lines must be at least 1")
	}
	if time.Duration(control.AuditRetention) <= 0 {
		validator.fail("control.audit_retention", CodeControlAuditRetentionRange, "audit_retention must be positive")
	}
	if strings.TrimSpace(control.InboxDir) == "" {
		validator.fail("control.inbox_dir", CodeControlTargetInvalid, "inbox_dir must not be empty")
	}
	if strings.TrimSpace(control.OutboxDir) == "" {
		validator.fail("control.outbox_dir", CodeControlTargetInvalid, "outbox_dir must not be empty")
	}
}

func (validator *validator) validateServices(parsed *Config) {
	if len(parsed.Services) == 0 {
		validator.fail("services", CodeServicesMissing, "at least one service is required")
		return
	}

	declared := make(map[string]bool, len(parsed.Sections))
	for _, section := range parsed.Sections {
		declared[section.ID] = true
	}
	seen := make(map[string]bool, len(parsed.Services))

	for i := range parsed.Services {
		service := &parsed.Services[i]
		path := fmt.Sprintf("services[%d]", i)

		if !slugPattern.MatchString(service.ID) {
			validator.fail(path+".id", CodeServiceIDInvalid, "%q is not a slug like my-app", service.ID)
		}
		if seen[service.ID] {
			validator.fail(path+".id", CodeServiceIDDuplicate, "service %q is declared twice", service.ID)
		}
		seen[service.ID] = true

		if strings.TrimSpace(service.Name) == "" {
			validator.fail(path+".name", CodeServiceNameMissing, "name is required")
		}
		if !declared[service.Section] {
			validator.fail(path+".section", CodeServiceSectionUndeclared,
				"section %q is not declared in sections", service.Section)
		}
		if !validIcon(service.Icon) {
			validator.fail(path+".icon", CodeIconInvalid,
				"icon must be a kebab-case lucide name or {image: icons/<file>.png}")
		}
		if service.URL != "" && !validHTTPURL(service.URL) {
			validator.fail(path+".url", CodeServiceURLInvalid, "url must be an http or https URL")
		}

		validator.validateProbe(path+".probes.health", &service.Probes.Health, false)
		for j := range service.Probes.Vitals {
			vitalPath := fmt.Sprintf("%s.probes.vitals[%d]", path, j)
			validator.validateProbe(vitalPath, &service.Probes.Vitals[j], true)
		}
		if service.Control != nil {
			validator.validateServiceControl(path+".control", service.Control)
		}
	}
}

// validateProbe checks the parts common to every kind. The kinds table (T07) adds the per-kind
// required and unexpected keys.
func (validator *validator) validateProbe(path string, probe *Probe, isVital bool) {
	if strings.TrimSpace(probe.Kind) == "" {
		validator.fail(path+".kind", CodeProbeKindMissing, "kind is required")
	}
	if probe.URL != "" && !validHTTPURL(probe.URL) {
		validator.fail(path+".url", CodeProbeURLInvalid, "url must be an http or https URL without credentials")
	}
	if probe.Has("decimals") && (probe.Decimals < 0 || probe.Decimals > 6) {
		validator.fail(path+".decimals", CodeProbeDecimalsRange, "decimals must be between 0 and 6")
	}
	if isVital && strings.TrimSpace(probe.Label) == "" {
		validator.fail(path+".label", CodeVitalLabelMissing, "every vital needs a short label")
	}
	if probe.Auth != nil {
		validator.validateAuth(path+".auth", probe.Auth)
	}
}

func (validator *validator) validateAuth(path string, auth *Auth) {
	configured := 0
	if len(auth.BasicEnv) > 0 {
		configured++
	}
	if auth.BearerEnv != "" {
		configured++
	}
	if len(auth.Header) > 0 {
		configured++
	}
	if configured != 1 {
		validator.fail(path, CodeAuthInvalid, "auth needs exactly one of basic_env, bearer_env or header")
		return
	}

	switch {
	case len(auth.BasicEnv) > 0:
		if len(auth.BasicEnv) != 2 {
			validator.fail(path+".basic_env", CodeAuthInvalid,
				"basic_env needs the user and the password variable name")
		}
		for i, name := range auth.BasicEnv {
			if !envPattern.MatchString(name) {
				validator.fail(fmt.Sprintf("%s.basic_env[%d]", path, i), CodeAuthInvalid,
					"%q is not an environment variable name", name)
			}
		}
	case auth.BearerEnv != "":
		if !envPattern.MatchString(auth.BearerEnv) {
			validator.fail(path+".bearer_env", CodeAuthInvalid,
				"%q is not an environment variable name", auth.BearerEnv)
		}
	default:
		names := make([]string, 0, len(auth.Header))
		for name := range auth.Header {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !headerPattern.MatchString(name) {
				validator.fail(path+".header", CodeAuthInvalid, "%q is not a header name", name)
			}
			if hasControlBytes(auth.Header[name]) {
				validator.fail(path+".header."+name, CodeAuthInvalid, "header value contains control characters")
			}
		}
	}
}

func (validator *validator) validateServiceControl(path string, control *ServiceControl) {
	if !slugPattern.MatchString(control.Target) {
		validator.fail(path+".target", CodeControlTargetInvalid, "%q is not a slug", control.Target)
	}
	if len(control.Actions) == 0 {
		validator.fail(path+".actions", CodeControlActionsInvalid, "at least one action is required")
	}

	seen := make(map[string]bool, len(control.Actions))
	hasStop := false
	for i, action := range control.Actions {
		if !controlActions[action] {
			validator.fail(fmt.Sprintf("%s.actions[%d]", path, i), CodeControlActionsInvalid,
				"%q is not one of start, stop, restart, update", action)
		}
		if seen[action] {
			validator.fail(fmt.Sprintf("%s.actions[%d]", path, i), CodeControlActionsInvalid,
				"action %q is listed twice", action)
		}
		seen[action] = true
		if action == "stop" {
			hasStop = true
		}
	}

	if control.DNS && !hasStop {
		validator.warn(path+".actions", CodeControlDNSWithoutStop,
			"dns is true but stop is not offered, so the auto-restore never applies")
	}
	if control.NoUpdateReason != "" && seen["update"] {
		validator.fail(path+".no_update_reason", CodeControlUpdateConflict,
			"no_update_reason is set but update is offered")
	}
}

func validIcon(icon Icon) bool {
	switch {
	case icon.Lucide != "" && icon.Image != "":
		return false
	case icon.Lucide != "":
		return lucidePattern.MatchString(icon.Lucide)
	case icon.Image != "":
		return imagePattern.MatchString(icon.Image)
	default:
		return false
	}
}

func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != "" && parsed.User == nil
}

// validHostEntry accepts a hostname or host:port with no scheme, path or credentials.
func validHostEntry(entry string) bool {
	if entry == "" {
		return false
	}
	parsed, err := url.Parse("http://" + entry)
	if err != nil {
		return false
	}
	if parsed.Host != entry || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Hostname() == "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	return true
}

func hasControlBytes(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return true
		}
	}
	return false
}

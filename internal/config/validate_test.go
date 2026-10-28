package config_test

import (
	"testing"

	"github.com/autumnsgrove/meridian/internal/config"
)

// brokenConfigs is one row per validation rule: the smallest change to minimal that breaks it, the
// path a user needs to look at, and the stable code a caller can branch on.
type brokenConfig struct {
	name     string
	document string
	path     string
	code     config.Code
}

func brokenConfigs(t *testing.T) []brokenConfig {
	return []brokenConfig{
		{
			name:     "listen without a port",
			document: substitute(t, minimal, "127.0.0.1:8891", "localhost"),
			path:     "listen",
			code:     config.CodeListen,
		},
		{
			name:     "listen port out of range",
			document: substitute(t, minimal, "127.0.0.1:8891", "127.0.0.1:0"),
			path:     "listen",
			code:     config.CodeListen,
		},
		{
			name:     "allowed host with a scheme",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nserver: {allowed_hosts: [\"https://hub.example.com\"]}"),
			path:     "server.allowed_hosts[0]",
			code:     config.CodeAllowedHost,
		},
		{
			name:     "allowed host with a path",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nserver: {allowed_hosts: [\"hub.example.com/page\"]}"),
			path:     "server.allowed_hosts[0]",
			code:     config.CodeAllowedHost,
		},
		{
			name:     "public url with the wrong scheme",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nserver: {public_url: \"ftp://hub.example.com\"}"),
			path:     "server.public_url",
			code:     config.CodePublicURL,
		},
		{
			name:     "no sections",
			document: substitute(t, minimal, "sections:\n  - {id: everyday, title: Everyday}\n", ""),
			path:     "sections",
			code:     config.CodeSectionsMissing,
		},
		{
			name:     "section id is not a slug",
			document: substitute(t, minimal, "{id: everyday, title: Everyday}", "{id: Everyday, title: Everyday}"),
			path:     "sections[0].id",
			code:     config.CodeSectionIDInvalid,
		},
		{
			name:     "section declared twice",
			document: substitute(t, minimal, "  - {id: everyday, title: Everyday}\n", "  - {id: everyday, title: Everyday}\n  - {id: everyday, title: Again}\n"),
			path:     "sections[1].id",
			code:     config.CodeSectionIDDuplicate,
		},
		{
			name:     "section without a title",
			document: substitute(t, minimal, "title: Everyday", `title: ""`),
			path:     "sections[0].title",
			code:     config.CodeSectionTitleMissing,
		},
		{
			name:     "no services",
			document: substitute(t, minimal, "services:\n"+appService+"\n", ""),
			path:     "services",
			code:     config.CodeServicesMissing,
		},
		{
			name:     "service id is not a slug",
			document: substitute(t, minimal, "id: app", "id: App"),
			path:     "services[0].id",
			code:     config.CodeServiceIDInvalid,
		},
		{
			name:     "service declared twice",
			document: substitute(t, minimal, appService, appService+"\n"+appService),
			path:     "services[1].id",
			code:     config.CodeServiceIDDuplicate,
		},
		{
			name:     "service without a name",
			document: substitute(t, minimal, "name: App", `name: ""`),
			path:     "services[0].name",
			code:     config.CodeServiceNameMissing,
		},
		{
			name:     "service in an undeclared section",
			document: substitute(t, minimal, "section: everyday", "section: media"),
			path:     "services[0].section",
			code:     config.CodeServiceSectionUndeclared,
		},
		{
			name:     "service url with the wrong scheme",
			document: substitute(t, minimal, "icon: sparkles", "icon: sparkles, url: \"ftp://app.example.com\""),
			path:     "services[0].url",
			code:     config.CodeServiceURLInvalid,
		},
		{
			name:     "lucide icon is not kebab-case",
			document: substitute(t, minimal, "icon: sparkles", "icon: Sparkles"),
			path:     "services[0].icon",
			code:     config.CodeIconInvalid,
		},
		{
			name:     "image icon climbs out of the directory",
			document: substitute(t, minimal, "icon: sparkles", "icon: {image: icons/../secret.png}"),
			path:     "services[0].icon",
			code:     config.CodeIconInvalid,
		},
		{
			name:     "icon missing",
			document: substitute(t, minimal, "icon: sparkles", "icon: \"\""),
			path:     "services[0].icon",
			code:     config.CodeIconInvalid,
		},
		{
			name:     "poll interval below the floor",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\npoll: {interval: 1s}"),
			path:     "poll.interval",
			code:     config.CodePollIntervalRange,
		},
		{
			name:     "poll timeout below the floor",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\npoll: {timeout: 100ms}"),
			path:     "poll.timeout",
			code:     config.CodePollTimeoutRange,
		},
		{
			name:     "timeout is not shorter than interval",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\npoll: {interval: 5s, timeout: 5s}"),
			path:     "poll.timeout",
			code:     config.CodePollTimeoutOrder,
		},
		{
			name:     "idle interval below interval",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\npoll: {interval: 15s, idle_interval: 10s}"),
			path:     "poll.idle_interval",
			code:     config.CodePollIdleIntervalRange,
		},
		{
			name:     "probe without a kind",
			document: substitute(t, minimal, "{kind: self}", `{url: "http://localhost/"}`),
			path:     "services[0].probes.health.kind",
			code:     config.CodeProbeKindMissing,
		},
		{
			name:     "probe url with the wrong scheme",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "ftp://localhost/"}`),
			path:     "services[0].probes.health.url",
			code:     config.CodeProbeURLInvalid,
		},
		{
			name:     "probe url with credentials in it",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://user:pass@localhost/"}`),
			path:     "services[0].probes.health.url",
			code:     config.CodeProbeURLInvalid,
		},
		{
			name: "decimals out of range",
			document: substitute(t, minimal, "{kind: self}",
				`{kind: self}, vitals: [{kind: json_field, label: build, url: "http://localhost/", path: version, decimals: 9}]`),
			path: "services[0].probes.vitals[0].decimals",
			code: config.CodeProbeDecimalsRange,
		},
		{
			name: "vital without a label",
			document: substitute(t, minimal, "{kind: self}",
				`{kind: self}, vitals: [{kind: json_field, url: "http://localhost/", path: version}]`),
			path: "services[0].probes.vitals[0].label",
			code: config.CodeVitalLabelMissing,
		},
		{
			name:     "too many search buttons",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nsearch:\n  buttons: [{label: a, url_template: \"https://x/?q={q}\"}, {label: b, url_template: \"https://x/?q={q}\"}, {label: c, url_template: \"https://x/?q={q}\"}, {label: d, url_template: \"https://x/?q={q}\"}, {label: e, url_template: \"https://x/?q={q}\"}]"),
			path:     "search.buttons",
			code:     config.CodeSearchTooMany,
		},
		{
			name:     "search button without a label",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nsearch: {buttons: [{label: \"\", url_template: \"https://x/?q={q}\"}]}"),
			path:     "search.buttons[0].label",
			code:     config.CodeSearchLabelMissing,
		},
		{
			name:     "search template without the placeholder",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nsearch: {buttons: [{label: Search, url_template: \"https://x/search\"}]}"),
			path:     "search.buttons[0].url_template",
			code:     config.CodeSearchPlaceholder,
		},
		{
			name:     "search template is not a URL",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nsearch: {buttons: [{label: Search, url_template: \"ftp://x/?q={q}\"}]}"),
			path:     "search.buttons[0].url_template",
			code:     config.CodeSearchURLInvalid,
		},
		{
			name:     "auth with nothing configured",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://localhost/", auth: {}}`),
			path:     "services[0].probes.health.auth",
			code:     config.CodeAuthInvalid,
		},
		{
			name:     "auth with two mechanisms",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://localhost/", auth: {basic_env: [USER, PASS], bearer_env: TOKEN}}`),
			path:     "services[0].probes.health.auth",
			code:     config.CodeAuthInvalid,
		},
		{
			name:     "basic auth with one variable",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://localhost/", auth: {basic_env: [USER]}}`),
			path:     "services[0].probes.health.auth.basic_env",
			code:     config.CodeAuthInvalid,
		},
		{
			name:     "environment variable named in lowercase",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://localhost/", auth: {bearer_env: token}}`),
			path:     "services[0].probes.health.auth.bearer_env",
			code:     config.CodeAuthInvalid,
		},
		{
			name:     "header value with a newline",
			document: substitute(t, minimal, "{kind: self}", `{kind: http_status, url: "http://localhost/", auth: {header: {X-Test: "bad\nvalue"}}}`),
			path:     "services[0].probes.health.auth.header.X-Test",
			code:     config.CodeAuthInvalid,
		},
		{
			name:     "control target is not a slug",
			document: substitute(t, minimal, appService, `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: Bad, actions: [restart]}}`),
			path:     "services[0].control.target",
			code:     config.CodeControlTargetInvalid,
		},
		{
			name:     "control action outside the vocabulary",
			document: substitute(t, minimal, appService, `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, actions: [restart, explode]}}`),
			path:     "services[0].control.actions[1]",
			code:     config.CodeControlActionsInvalid,
		},
		{
			name:     "control action listed twice",
			document: substitute(t, minimal, appService, `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, actions: [restart, restart]}}`),
			path:     "services[0].control.actions[1]",
			code:     config.CodeControlActionsInvalid,
		},
		{
			name:     "control with no actions",
			document: substitute(t, minimal, appService, `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, actions: []}}`),
			path:     "services[0].control.actions",
			code:     config.CodeControlActionsInvalid,
		},
		{
			name:     "update offered with a no-update reason",
			document: substitute(t, minimal, appService, `  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, actions: [update], no_update_reason: "not yet"}}`),
			path:     "services[0].control.no_update_reason",
			code:     config.CodeControlUpdateConflict,
		},
		{
			name:     "queue depth below one",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\ncontrol: {queue_depth: -1}"),
			path:     "control.queue_depth",
			code:     config.CodeControlQueueDepthRange,
		},
		{
			name:     "action timeout not positive",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\ncontrol: {action_timeout: -1s}"),
			path:     "control.action_timeout",
			code:     config.CodeControlActionTimeoutRange,
		},
		{
			name:     "audit lines below one",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\ncontrol: {audit_lines: -1}"),
			path:     "control.audit_lines",
			code:     config.CodeControlAuditLinesRange,
		},
		{
			name:     "audit retention not positive",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\ncontrol: {audit_retention: -1h}"),
			path:     "control.audit_retention",
			code:     config.CodeControlAuditRetentionRange,
		},
		{
			name:     "update image with a tag",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nupdate: {image: \"ghcr.io/owner/name:latest\", publish_url: \"https://api.example.org/runs\"}"),
			path:     "update.image",
			code:     config.CodeUpdateImage,
		},
		{
			name:     "update image at another registry",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nupdate: {image: docker.io/owner/name, publish_url: \"https://api.example.org/runs\"}"),
			path:     "update.image",
			code:     config.CodeUpdateImage,
		},
		{
			name:     "update publish url with the wrong scheme",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nupdate: {image: ghcr.io/owner/name, publish_url: \"ftp://api.example.org/runs\"}"),
			path:     "update.publish_url",
			code:     config.CodeUpdatePublishURL,
		},
		{
			name:     "update signal directory is relative",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nupdate: {image: ghcr.io/owner/name, publish_url: \"https://api.example.org/runs\", signal_dir: update}"),
			path:     "update.signal_dir",
			code:     config.CodeUpdateSignalDir,
		},
		{
			name:     "host status without a file",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nhost_status: {max_age: 1m}"),
			path:     "host_status.file",
			code:     config.CodeHostStatusFile,
		},
		{
			name:     "host status with a negative max age",
			document: substitute(t, minimal, "listen: 127.0.0.1:8891", "listen: 127.0.0.1:8891\nhost_status: {file: /status/host.json, max_age: -1m}"),
			path:     "host_status.max_age",
			code:     config.CodeHostStatusAge,
		},
	}
}

func TestValidate_OneRowPerRule(t *testing.T) {
	for _, test := range brokenConfigs(t) {
		t.Run(test.name, func(t *testing.T) {
			problems := parseProblems(t, test.document)
			for _, problem := range problems {
				if problem.Path == test.path && problem.Code == test.code {
					return
				}
			}
			t.Fatalf("problems = %v, want one at %s with code %s", problems, test.path, test.code)
		})
	}
}

func TestValidate_DNSWithoutStopOnlyWarns(t *testing.T) {
	document := substitute(t, minimal, appService,
		`  - {id: app, name: App, section: everyday, icon: sparkles, probes: {health: {kind: self}}, control: {target: app, dns: true, actions: [restart]}}`)
	parsed, warnings := parse(t, document)
	if parsed == nil {
		t.Fatal("Parse returned no config")
	}
	if len(warnings) != 1 || warnings[0].Code != config.CodeControlDNSWithoutStop {
		t.Fatalf("warnings = %v, want one dns_without_stop", warnings)
	}
}

func TestValidate_IsIdempotent(t *testing.T) {
	parsed, _ := parse(t, minimal)
	problems, _ := config.Validate(parsed)
	if len(problems) != 0 {
		t.Fatalf("validating a loaded config found problems: %v", problems)
	}
}

func TestValidate_ImageIcons(t *testing.T) {
	document := substitute(t, minimal, "icon: sparkles", "icon: {image: icons/app-icon.png}")
	if _, _, err := config.Parse([]byte(document)); err != nil {
		t.Fatalf("Parse with an image icon: %v", err)
	}

	bad := substitute(t, minimal, "icon: sparkles", "icon: {image: icons/app.svg}")
	if _, _, err := config.Parse([]byte(bad)); err == nil {
		t.Fatal("Parse accepted an SVG icon, which can carry script")
	}
}

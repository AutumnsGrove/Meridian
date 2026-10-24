package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/config"
)

// TestKindTables_MatchTheVocabularyComment keeps the tables and the documented vocabulary at the
// bottom of config.yaml.example from drifting: the comment is the contract users read.
func TestKindTables_MatchTheVocabularyComment(t *testing.T) {
	health, vital := parseVocabulary(t, filepath.Join("..", "..", "config.yaml.example"))
	assertSameKinds(t, "health", health, config.HealthKinds)
	assertSameKinds(t, "vital", vital, config.VitalKinds)
}

func TestKindTables_AreSelfConsistent(t *testing.T) {
	fields := probeFieldNames(t)
	tables := map[string]map[string]config.KindRules{"health": config.HealthKinds, "vital": config.VitalKinds}

	for family, table := range tables {
		for kind, rules := range table {
			t.Run(family+"/"+kind, func(t *testing.T) {
				if rules.Doc == "" {
					t.Errorf("%s has no doc line", kind)
				}
				taken := map[string]bool{}
				for _, key := range rules.Required {
					if !fields[key] {
						t.Errorf("%s requires %q, which is not a probe field", kind, key)
					}
					if taken[key] {
						t.Errorf("%s lists %q twice", kind, key)
					}
					taken[key] = true
				}
				for _, key := range rules.Optional {
					if !fields[key] {
						t.Errorf("%s allows %q, which is not a probe field", kind, key)
					}
					if taken[key] {
						t.Errorf("%s lists %q twice", kind, key)
					}
					taken[key] = true
				}
				for _, need := range rules.Needs {
					for _, key := range need.Keys {
						if !taken[key] {
							t.Errorf("%s has a %s rule naming %q, which it does not take", kind, need.Kind, key)
						}
					}
				}
			})
		}
	}
}

func TestKindTables_RejectFieldsTheirKindDoesNotTake(t *testing.T) {
	tests := []struct {
		name  string
		probe string
		path  string
		code  config.Code
	}{
		{
			name:  "unknown kind",
			probe: `{kind: http_ping}`,
			path:  "services[0].probes.health.kind",
			code:  config.CodeProbeKindUnknown,
		},
		{
			name:  "missing required key",
			probe: `{kind: http_status}`,
			path:  "services[0].probes.health.url",
			code:  config.CodeProbeKeyMissing,
		},
		{
			name:  "dns does not take a url",
			probe: `{kind: dns, addr: "127.0.0.1:5335", qname: "example.com.", url: "http://localhost/"}`,
			path:  "services[0].probes.health.url",
			code:  config.CodeProbeKeyUnexpected,
		},
		{
			name:  "host_unit does not take a url",
			probe: `{kind: host_unit, unit: daemon.service, url: "http://localhost/"}`,
			path:  "services[0].probes.health.url",
			code:  config.CodeProbeKeyUnexpected,
		},
		{
			name:  "self takes nothing",
			probe: `{kind: self, url: "http://localhost/"}`,
			path:  "services[0].probes.health.url",
			code:  config.CodeProbeKeyUnexpected,
		},
		{
			name:  "a vital kind is not a health kind",
			probe: `{kind: json_field}`,
			path:  "services[0].probes.health.kind",
			code:  config.CodeProbeKindUnknown,
		},
		{
			name:  "http_json needs one match rule",
			probe: `{kind: http_json, url: "http://localhost/", path: status}`,
			path:  "services[0].probes.health",
			code:  config.CodeProbeNeedExactlyOne,
		},
		{
			name:  "http_json takes only one match rule",
			probe: `{kind: http_json, url: "http://localhost/", path: status, equals: ok, one_of: [ok]}`,
			path:  "services[0].probes.health",
			code:  config.CodeProbeNeedExactlyOne,
		},
		{
			name:  "http_status takes expect or expect_class",
			probe: `{kind: http_status, url: "http://localhost/", expect: [200], expect_class: "2xx"}`,
			path:  "services[0].probes.health",
			code:  config.CodeProbeNeedAtMostOne,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := substitute(t, minimal, "{kind: self}", test.probe)
			problems := parseProblems(t, document)
			for _, problem := range problems {
				if problem.Path == test.path && problem.Code == test.code {
					return
				}
			}
			t.Fatalf("problems = %v, want one at %s with code %s", problems, test.path, test.code)
		})
	}
}

func TestKindTables_VitalRules(t *testing.T) {
	tests := []struct {
		name  string
		vital string
		path  string
		code  config.Code
	}{
		{
			name:  "a health kind is not a vital kind",
			vital: `{kind: http_status, label: code, url: "http://localhost/"}`,
			path:  "services[0].probes.vitals[0].kind",
			code:  config.CodeProbeKindUnknown,
		},
		{
			name:  "json_sum scale is a closed value",
			vital: `{kind: json_sum, label: total, url: "http://localhost/", where: devices, fields: [bytes], scale: binary}`,
			path:  "services[0].probes.vitals[0].scale",
			code:  config.CodeProbeNeedValue,
		},
		{
			name:  "json_count field and equals go together",
			vital: `{kind: json_count, label: needs, url: "http://localhost/", where: items, field: needs}`,
			path:  "services[0].probes.vitals[0]",
			code:  config.CodeProbeNeedBothOrNeither,
		},
		{
			name:  "json_sum needs its scale",
			vital: `{kind: json_sum, label: total, url: "http://localhost/", where: devices, fields: [bytes]}`,
			path:  "services[0].probes.vitals[0].scale",
			code:  config.CodeProbeKeyMissing,
		},
		{
			name:  "json_count does not take path",
			vital: `{kind: json_count, label: needs, url: "http://localhost/", where: items, path: status}`,
			path:  "services[0].probes.vitals[0].path",
			code:  config.CodeProbeKeyUnexpected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := substitute(t, minimal, "{kind: self}", "{kind: self}, vitals: ["+test.vital+"]")
			problems := parseProblems(t, document)
			for _, problem := range problems {
				if problem.Path == test.path && problem.Code == test.code {
					return
				}
			}
			t.Fatalf("problems = %v, want one at %s with code %s", problems, test.path, test.code)
		})
	}
}

func TestKindTables_ValidProbesLoad(t *testing.T) {
	tests := []struct {
		name  string
		probe string
	}{
		{"http_status with expect", `{kind: http_status, url: "http://localhost/", expect: [200, 302]}`},
		{"http_status with expect_class", `{kind: http_status, url: "http://localhost/", expect_class: "2xx"}`},
		{"http_status with no expectation", `{kind: http_status, url: "http://localhost/"}`},
		{"http_json with one_of", `{kind: http_json, url: "http://localhost/", path: status, one_of: [ok, ready]}`},
		{"http_json with exists", `{kind: http_json, url: "http://localhost/", path: build, exists: true}`},
		{"dns", `{kind: dns, addr: "127.0.0.1:5335", qname: "example.com.", expect_rcode: NOERROR}`},
		{"host_unit", `{kind: host_unit, unit: daemon.service}`},
		{"host_field", `{kind: host_field, path: tailscale.online}`},
		{"http_status with auth", `{kind: http_status, url: "http://localhost/", auth: {bearer_env: TOKEN}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := substitute(t, minimal, "{kind: self}", test.probe)
			if _, _, err := config.Parse([]byte(document)); err != nil {
				t.Fatalf("Parse: %v", err)
			}
		})
	}
}

// TestRepositoryConfigs_Load is the contract that every config in the repo is valid config, checked
// here so a schema change that breaks the template or the worked example is caught immediately.
func TestRepositoryConfigs_Load(t *testing.T) {
	paths := map[string]string{
		"template":       filepath.Join("..", "..", "config.yaml.example"),
		"worked example": filepath.Join("..", "..", "examples", "homelab.yaml"),
		"portable":       filepath.Join("..", "..", "testdata", "portable", "config.yaml"),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			parsed, warnings, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load(%s): %v", path, err)
			}
			if len(warnings) != 0 {
				t.Errorf("Load(%s) warnings = %v, want none", path, warnings)
			}
			if len(parsed.Services) == 0 {
				t.Errorf("Load(%s) found no services", path)
			}
		})
	}
}

var kindName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// parseVocabulary reads the kind names out of the comment block at the bottom of the example config.
func parseVocabulary(t *testing.T, path string) (health, vital []string) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a repo path named by the test
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "#   ") {
			switch {
			case strings.Contains(line, "Probe kinds"):
				section = "health"
			case strings.Contains(line, "Vital kinds"):
				section = "vital"
			default:
				section = ""
			}
			continue
		}
		if section == "" || !strings.HasPrefix(line, "#   ") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#")))
		if len(fields) < 2 || !kindName.MatchString(fields[0]) || !strings.HasPrefix(fields[1], "{") {
			continue
		}
		switch section {
		case "health":
			health = append(health, fields[0])
		case "vital":
			vital = append(vital, fields[0])
		}
	}
	return health, vital
}

func assertSameKinds(t *testing.T, family string, documented []string, table map[string]config.KindRules) {
	t.Helper()
	fromTable := make([]string, 0, len(table))
	for kind := range table {
		fromTable = append(fromTable, kind)
	}
	sort.Strings(documented)
	sort.Strings(fromTable)
	if strings.Join(documented, ",") != strings.Join(fromTable, ",") {
		t.Errorf("%s kinds in the example config = %v, in the table = %v", family, documented, fromTable)
	}
}

func probeFieldNames(t *testing.T) map[string]bool {
	t.Helper()
	typ := reflect.TypeOf(config.Probe{})
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		fields[strings.Split(tag, ",")[0]] = true
	}
	return fields
}

func TestKindTables_ValidVitalsLoad(t *testing.T) {
	tests := []struct {
		name  string
		vital string
	}{
		{"json_count of everything", `{kind: json_count, label: n, url: "http://localhost/", where: items}`},
		{"json_count matching a field", `{kind: json_count, label: n, url: "http://localhost/", where: items, field: needs, equals: true, suffix: waiting}`},
		{"json_sum in bytes", `{kind: json_sum, label: total, url: "http://localhost/", where: devices, fields: [up_bytes, down_bytes], scale: bytes, unit: GB, decimals: 1, suffix: "GB today"}`},
		{"json_sum without a scale", `{kind: json_sum, label: total, url: "http://localhost/", where: devices, fields: [count], scale: none, prefix: "~"}`},
		{"json_ratio_pct", `{kind: json_ratio_pct, label: blocked, url: "http://localhost/", numerator: a, denominator: b, decimals: 0, suffix: "% blocked"}`},
		{"host_field", `{kind: host_field, label: peers, path: tailscale.peers_online, suffix: online}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := substitute(t, minimal, "{kind: self}", "{kind: self}, vitals: ["+test.vital+"]")
			if _, _, err := config.Parse([]byte(document)); err != nil {
				t.Fatalf("Parse: %v", err)
			}
		})
	}
}

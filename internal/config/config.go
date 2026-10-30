// Package config loads and validates config.yaml: the whole product for anyone but us, since no
// service name, host or quirk lives in code (decisions #38-#42).
package config

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is one parsed config.yaml.
type Config struct {
	Listen     string          `yaml:"listen"`
	Server     Server          `yaml:"server"`
	Sections   []Section       `yaml:"sections"`
	Poll       Poll            `yaml:"poll"`
	HostStatus *HostStatus     `yaml:"host_status"`
	Search     Search          `yaml:"search"`
	Control    ControlSettings `yaml:"control"`
	Update     *Update         `yaml:"update"`
	Services   []Service       `yaml:"services"`
}

// Server holds the browser-facing guards: the names this page can be reached by (decision #54).
type Server struct {
	AllowedHosts []string `yaml:"allowed_hosts"`
	PublicURL    string   `yaml:"public_url"`
}

// Section is one declared group of tiles, in display order.
type Section struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
}

// MaxPollTimeout is the longest poll.timeout that means anything: the scheduler gives one service's
// whole cycle this long, so a larger per-probe timeout would validate and then be silently cut short
// (docs/ARCHITECTURE.md "Polling model").
const MaxPollTimeout = 6 * time.Second

// Poll holds the probe cadence and ceilings.
type Poll struct {
	Interval     Duration `yaml:"interval"`
	IdleInterval Duration `yaml:"idle_interval"`
	IdleAfter    Duration `yaml:"idle_after"`
	Timeout      Duration `yaml:"timeout"`
	RefreshMin   Duration `yaml:"refresh_min"`
}

// HostStatus points at the snapshot the host probe writes. Nil means host probes report unknown.
type HostStatus struct {
	File   string   `yaml:"file"`
	MaxAge Duration `yaml:"max_age"`
}

// Search is the buttons above the tiles; the first is what Enter submits.
type Search struct {
	Buttons []SearchButton `yaml:"buttons"`
}

// SearchButton is one search destination; {q} is replaced with the URL-encoded query.
type SearchButton struct {
	Label       string `yaml:"label"`
	URLTemplate string `yaml:"url_template"`
}

// ControlSettings are the container-side control defaults.
type ControlSettings struct {
	Reboot         bool     `yaml:"reboot"`
	QueueDepth     int      `yaml:"queue_depth"`
	ActionTimeout  Duration `yaml:"action_timeout"`
	AuditLines     int      `yaml:"audit_lines"`
	AuditRetention Duration `yaml:"audit_retention"`
	InboxDir       string   `yaml:"inbox_dir"`
	OutboxDir      string   `yaml:"outbox_dir"`
}

// Update configures self-update (decision #20). The image is a repository, never a tag: the digest is
// resolved from the registry and pinned by the host. The publish URL is the repository's own status
// endpoint, so no GitHub hostname is baked into the code (decision #38 applies to infrastructure too).
type Update struct {
	// Image is the GHCR repository this build publishes to, "ghcr.io/<owner>/<name>".
	Image string `yaml:"image"`
	// PublishURL is the status endpoint that names the newest commit and whether its build is
	// running. It is read to wait out an in-flight CI build before resolving a digest.
	PublishURL string `yaml:"publish_url"`
	// SignalDir is the container path of the writable self-update handoff directory.
	SignalDir string `yaml:"signal_dir"`
	// Target is the control target this image belongs to. An update request for it resolves the
	// digest and writes the handoff before the request reaches the host.
	Target string `yaml:"target"`
}

// Service is one tile and everything behind it.
type Service struct {
	ID          string          `yaml:"id"`
	Name        string          `yaml:"name"`
	Section     string          `yaml:"section"`
	Icon        Icon            `yaml:"icon"`
	Description string          `yaml:"description"`
	URL         string          `yaml:"url"`
	Disabled    bool            `yaml:"disabled"`
	Probes      Probes          `yaml:"probes"`
	Control     *ServiceControl `yaml:"control"`
}

// Probes is one required health probe and zero or more vitals.
type Probes struct {
	Health Probe   `yaml:"health"`
	Vitals []Probe `yaml:"vitals"`
}

// Probe is the flat union of every field any probe or vital kind uses. Set records the keys that
// were actually present, so validation can say "kind dns does not take url" instead of ignoring it.
type Probe struct {
	Kind        string              `yaml:"kind"`
	URL         string              `yaml:"url"`
	Path        string              `yaml:"path"`
	Equals      any                 `yaml:"equals"`
	OneOf       []any               `yaml:"one_of"`
	Exists      *bool               `yaml:"exists"`
	Expect      []int               `yaml:"expect"`
	ExpectClass string              `yaml:"expect_class"`
	Addr        string              `yaml:"addr"`
	Qname       string              `yaml:"qname"`
	ExpectRcode string              `yaml:"expect_rcode"`
	Unit        string              `yaml:"unit"`
	Label       string              `yaml:"label"`
	Where       string              `yaml:"where"`
	Field       string              `yaml:"field"`
	Fields      []string            `yaml:"fields"`
	Scale       string              `yaml:"scale"`
	Numerator   string              `yaml:"numerator"`
	Denominator string              `yaml:"denominator"`
	Prefix      string              `yaml:"prefix"`
	Suffix      string              `yaml:"suffix"`
	Decimals    int                 `yaml:"decimals"`
	Auth        *Auth               `yaml:"auth"`
	Set         map[string]struct{} `yaml:"-"`
}

// Auth is a credential reference, never a value: the environment variable is read at request time.
type Auth struct {
	BasicEnv  []string          `yaml:"basic_env"`
	BearerEnv string            `yaml:"bearer_env"`
	Header    map[string]string `yaml:"header"`
}

// ServiceControl names the host allowlist target and what the page may ask of it.
type ServiceControl struct {
	Target         string   `yaml:"target"`
	Actions        []string `yaml:"actions"`
	DNS            bool     `yaml:"dns"`
	NoUpdateReason string   `yaml:"no_update_reason"`
}

// Icon is a lucide name or a local image path, never both (decision #43).
type Icon struct {
	Lucide string
	Image  string
}

// Duration is a Go duration written as a YAML string ("15s", "2m").
type Duration time.Duration

// probeKeys is every key a probe may carry across all kinds. A key outside this set is a typo; the
// kinds table decides which of these a particular kind may use.
var probeKeys = map[string]struct{}{
	"addr": {}, "auth": {}, "decimals": {}, "denominator": {}, "equals": {}, "exists": {},
	"expect": {}, "expect_class": {}, "expect_rcode": {}, "field": {}, "fields": {}, "kind": {},
	"label": {}, "numerator": {}, "one_of": {}, "path": {}, "prefix": {}, "qname": {},
	"scale": {}, "suffix": {}, "unit": {}, "url": {}, "where": {},
}

// UnmarshalYAML decodes a probe and records which keys were present. Unknown keys are refused here
// because a custom unmarshaler is outside the strict decoder's reach.
func (probe *Probe) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("probe must be a mapping")
	}
	type plain Probe
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	present := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, known := probeKeys[key]; !known {
			return fmt.Errorf("unknown probe key %q", key)
		}
		present[key] = struct{}{}
	}
	*probe = Probe(decoded)
	probe.Set = present
	return nil
}

// Has reports whether the probe carried the given key.
func (probe *Probe) Has(key string) bool {
	_, ok := probe.Set[key]
	return ok
}

// UnmarshalYAML decodes either a lucide name or {image: path}.
func (icon *Icon) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var name string
		if err := node.Decode(&name); err != nil {
			return err
		}
		*icon = Icon{Lucide: name}
		return nil
	case yaml.MappingNode:
		var path string
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if key != "image" {
				return fmt.Errorf("unknown icon key %q", key)
			}
			if err := node.Content[i+1].Decode(&path); err != nil {
				return err
			}
		}
		if path == "" {
			return fmt.Errorf("icon image path is empty")
		}
		*icon = Icon{Image: path}
		return nil
	default:
		return fmt.Errorf("icon must be a lucide name or {image: path}")
	}
}

// UnmarshalYAML parses a duration string such as "15s" or "2m".
func (duration *Duration) UnmarshalYAML(node *yaml.Node) error {
	var text string
	if err := node.Decode(&text); err != nil {
		return fmt.Errorf("duration must be a string like 15s: %w", err)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return err
	}
	*duration = Duration(parsed)
	return nil
}

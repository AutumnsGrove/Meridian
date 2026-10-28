package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// MaxConfigSize caps the config file. A config is a few hundred lines, not a data store.
const MaxConfigSize = 256 << 10

// Code is a stable identifier for a kind of problem, so callers and tests never match on English.
type Code string

const (
	CodeDecode                     Code = "decode"
	CodeTooLarge                   Code = "too_large"
	CodeListen                     Code = "listen_invalid"
	CodeAllowedHost                Code = "allowed_host_invalid"
	CodePublicURL                  Code = "public_url_invalid"
	CodeHostStatusFile             Code = "host_status_file_missing"
	CodeHostStatusAge              Code = "host_status_max_age_invalid"
	CodeSectionsMissing            Code = "sections_missing"
	CodeSectionIDInvalid           Code = "section_id_invalid"
	CodeSectionIDDuplicate         Code = "section_id_duplicate"
	CodeSectionTitleMissing        Code = "section_title_missing"
	CodeServicesMissing            Code = "services_missing"
	CodeServiceIDInvalid           Code = "service_id_invalid"
	CodeServiceIDDuplicate         Code = "service_id_duplicate"
	CodeServiceNameMissing         Code = "service_name_missing"
	CodeServiceSectionUndeclared   Code = "service_section_undeclared"
	CodeServiceURLInvalid          Code = "service_url_invalid"
	CodeIconInvalid                Code = "icon_invalid"
	CodePollIntervalRange          Code = "poll_interval_range"
	CodePollTimeoutRange           Code = "poll_timeout_range"
	CodePollTimeoutOrder           Code = "poll_timeout_order"
	CodePollIdleIntervalRange      Code = "poll_idle_interval_range"
	CodeSearchTooMany              Code = "search_too_many"
	CodeSearchLabelMissing         Code = "search_label_missing"
	CodeSearchURLInvalid           Code = "search_url_invalid"
	CodeSearchPlaceholder          Code = "search_placeholder"
	CodeAuthInvalid                Code = "auth_invalid"
	CodeControlQueueDepthRange     Code = "control_queue_depth_range"
	CodeControlActionTimeoutRange  Code = "control_action_timeout_range"
	CodeControlAuditLinesRange     Code = "control_audit_lines_range"
	CodeControlAuditRetentionRange Code = "control_audit_retention_range"
	CodeControlTargetInvalid       Code = "control_target_invalid"
	CodeControlActionsInvalid      Code = "control_actions_invalid"
	CodeControlDNSWithoutStop      Code = "control_dns_without_stop"
	CodeControlUpdateConflict      Code = "control_update_conflict"
	CodeUpdateImage                Code = "update_image_invalid"
	CodeUpdatePublishURL           Code = "update_publish_url_invalid"
	CodeUpdateSignalDir            Code = "update_signal_dir_invalid"
	CodeProbeKindMissing           Code = "probe_kind_missing"
	CodeProbeKindUnknown           Code = "probe_kind_unknown"
	CodeProbeKeyMissing            Code = "probe_key_missing"
	CodeProbeKeyUnexpected         Code = "probe_key_unexpected"
	CodeProbeNeedExactlyOne        Code = "probe_need_exactly_one"
	CodeProbeNeedAtMostOne         Code = "probe_need_at_most_one"
	CodeProbeNeedBothOrNeither     Code = "probe_need_both_or_neither"
	CodeProbeNeedValue             Code = "probe_need_value"
	CodeProbeURLInvalid            Code = "probe_url_invalid"
	CodeProbeDecimalsRange         Code = "probe_decimals_range"
	CodeVitalLabelMissing          Code = "vital_label_missing"
)

// Problem is one thing wrong with a config file, at a dotted path such as services[2].url.
type Problem struct {
	Path    string
	Code    Code
	Message string
}

// String renders the problem for a person: the path, then what is wrong.
func (problem Problem) String() string {
	if problem.Path == "" {
		return problem.Message
	}
	return problem.Path + ": " + problem.Message
}

// Problems is every problem found in one pass, so a user fixing a config does not need five round
// trips. It implements error so Load can return all of them at once.
type Problems []Problem

// Error lists every problem with its path.
func (problems Problems) Error() string {
	switch len(problems) {
	case 0:
		return "config: no problems"
	case 1:
		return "config: " + problems[0].String()
	}
	listed := make([]string, len(problems))
	for i, problem := range problems {
		listed[i] = problem.String()
	}
	return fmt.Sprintf("config: %d problems: %s", len(problems), strings.Join(listed, "; "))
}

// Warning is a config that loads but probably does not do what its author meant.
type Warning struct {
	Path    string
	Code    Code
	Message string
}

// Load reads path, decodes it strictly, applies defaults and validates it.
func Load(path string) (*Config, []Warning, error) {
	file, err := os.Open(path) //nolint:gosec // the operator names their own config file
	if err != nil {
		return nil, nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxConfigSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(data) > MaxConfigSize {
		return nil, nil, fmt.Errorf("config: %s is larger than %d bytes", path, MaxConfigSize)
	}

	parsed, warnings, err := Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return parsed, warnings, nil
}

// Parse decodes, defaults and validates one config document. It returns all problems at once, and
// never returns a config together with an error.
func Parse(data []byte) (*Config, []Warning, error) {
	if len(data) > MaxConfigSize {
		return nil, nil, fmt.Errorf("config: %d bytes is larger than %d", len(data), MaxConfigSize)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var parsed Config
	if err := decoder.Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, Problems{{Code: CodeDecode, Message: err.Error()}}
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, Problems{{Code: CodeDecode, Message: "more than one YAML document"}}
	}

	applyDefaults(&parsed)
	problems, warnings := Validate(&parsed)
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return &parsed, warnings, nil
}

// applyDefaults fills the template's defaults. These fields all have positive lower bounds, so a
// zero value can only mean "not written".
func applyDefaults(parsed *Config) {
	if parsed.Poll.Interval == 0 {
		parsed.Poll.Interval = Duration(15 * time.Second)
	}
	if parsed.Poll.IdleInterval == 0 {
		parsed.Poll.IdleInterval = Duration(2 * time.Minute)
	}
	if parsed.Poll.IdleAfter == 0 {
		parsed.Poll.IdleAfter = Duration(10 * time.Minute)
	}
	if parsed.Poll.Timeout == 0 {
		parsed.Poll.Timeout = Duration(4 * time.Second)
	}
	if parsed.Poll.RefreshMin == 0 {
		parsed.Poll.RefreshMin = Duration(5 * time.Second)
	}
	if parsed.Control.QueueDepth == 0 {
		parsed.Control.QueueDepth = 8
	}
	if parsed.Control.ActionTimeout == 0 {
		parsed.Control.ActionTimeout = Duration(90 * time.Second)
	}
	if parsed.Control.AuditLines == 0 {
		parsed.Control.AuditLines = 50
	}
	if parsed.Control.AuditRetention == 0 {
		parsed.Control.AuditRetention = Duration(365 * 24 * time.Hour)
	}
	if parsed.Control.InboxDir == "" {
		parsed.Control.InboxDir = "/control/inbox"
	}
	if parsed.Control.OutboxDir == "" {
		parsed.Control.OutboxDir = "/control/outbox"
	}
	if parsed.HostStatus != nil && parsed.HostStatus.MaxAge == 0 {
		parsed.HostStatus.MaxAge = Duration(90 * time.Second)
	}
	if parsed.Update != nil && parsed.Update.SignalDir == "" {
		parsed.Update.SignalDir = "/update-signal"
	}
}

package probe

import "errors"

// This file is the closed table of reason codes (decisions #40 and #46). A probe returns a code, the
// logs carry the code, and the page shows the sentence: no caller invents either. Errors never carry
// response bodies, URLs with secrets or stack traces (STANDARDS section 4).

// The reason codes a probe can report. Render one through Reason, never directly.
const (
	// CodeTimeout and CodeRefused describe a call that did not come back.
	CodeTimeout     = "timeout"
	CodeRefused     = "refused"
	CodeUnreachable = "unreachable"
	// CodeBadStatus is an HTTP answer whose status the config did not expect.
	CodeBadStatus = "bad_status"
	// CodeBadJSON and CodeMissingField describe an unreadable or unhelpful JSON answer.
	CodeBadJSON      = "bad_json"
	CodeMissingField = "missing_field"
	// CodeUnexpectedValue and CodeUnexpectedType are answers of the wrong value or shape.
	CodeUnexpectedValue = "unexpected_value"
	CodeUnexpectedType  = "unexpected_type"
	// CodeBodyTooLarge is a response over the probe body cap.
	CodeBodyTooLarge = "body_too_large"
	// CodeEnvMissing is a credential variable the config named that is unset: we could not ask.
	CodeEnvMissing = "env_missing"
	// CodeHostUnavailable and CodeHostStale describe the host snapshot, which must never read green.
	CodeHostUnavailable = "host_unavailable"
	CodeHostStale       = "host_stale"
	// CodeUnitInactive and CodeUnitNotReported describe a systemd unit.
	CodeUnitInactive    = "unit_inactive"
	CodeUnitNotReported = "unit_not_reported"
	// CodeDNSRcode is a DNS answer whose response code is not the expected one.
	CodeDNSRcode = "dns_rcode"
	// CodeOff is a reported boolean that says the thing is off, such as an exit node.
	CodeOff = "off"
	// CodeBadValue is a value the probe could not use, including an unbuildable request.
	CodeBadValue = "bad_value"
)

// reasonText is the sentence shown for each code, written for the tile: lower case, present tense,
// no trailing punctuation, and never a value the service reported.
var reasonText = map[string]string{
	CodeTimeout:         "it did not answer in time",
	CodeRefused:         "it refused the connection",
	CodeUnreachable:     "it cannot be reached",
	CodeBadStatus:       "it answered with an unexpected status",
	CodeBadJSON:         "its answer is not JSON this page can read",
	CodeMissingField:    "the field you asked for is missing",
	CodeUnexpectedValue: "its value is not the one you expected",
	CodeUnexpectedType:  "its value is not the type you expected",
	CodeBodyTooLarge:    "its answer is too large to read",
	CodeEnvMissing:      "the credential variable is not set",
	CodeHostUnavailable: "the host status file is not readable",
	CodeHostStale:       "the host status file is out of date",
	CodeUnitInactive:    "the unit is not active",
	CodeUnitNotReported: "the host status file does not report this unit",
	CodeDNSRcode:        "the DNS answer is not the one you expected",
	CodeOff:             "it is reported as off",
	CodeBadValue:        "a value it reported is not usable",
}

// Errors a vital returns besides the fetch and host ones, so the merge step names the reason from the
// error instead of matching on message text.
var (
	// ErrBadStatus means a JSON probe got an answer that was not a success, so its body says nothing
	// about the service's own state.
	ErrBadStatus = errors.New("probe: the answer is not a success status")
	// ErrMissingField means the path or field the config names is not in the answer.
	ErrMissingField = errors.New("probe: field is missing")
	// ErrUnexpectedType means the value is there but of a shape a vital does not format.
	ErrUnexpectedType = errors.New("probe: value is not the expected type")
	// ErrUnexpectedValue means the value is there and well typed, but not the expected one.
	ErrUnexpectedValue = errors.New("probe: value is not the expected one")
	// ErrBadValue means a value could not be used, such as bytes that are negative.
	ErrBadValue = errors.New("probe: value is not usable")
)

// Reason returns the sentence for code, or "" for a code this build does not know, so an old page
// can never print a raw identifier at the user.
func Reason(code string) string {
	return reasonText[code]
}

package probe_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/autumnsgrove/meridian/internal/probe"
)

// TestReason_EveryCodeHasASentence keeps the reason table and the codes together: a code a runner
// can return must have something to show, and the sentence must follow the copy rules.
func TestReason_EveryCodeHasASentence(t *testing.T) {
	codes := []string{
		probe.CodeTimeout,
		probe.CodeRefused,
		probe.CodeUnreachable,
		probe.CodeBadStatus,
		probe.CodeBadJSON,
		probe.CodeMissingField,
		probe.CodeUnexpectedValue,
		probe.CodeUnexpectedType,
		probe.CodeBodyTooLarge,
		probe.CodeEnvMissing,
		probe.CodeHostUnavailable,
		probe.CodeHostStale,
		probe.CodeUnitInactive,
		probe.CodeUnitNotReported,
		probe.CodeDNSRcode,
		probe.CodeOff,
		probe.CodeBadValue,
	}
	for _, code := range codes {
		reason := probe.Reason(code)
		if reason == "" {
			t.Errorf("code %q has no sentence", code)
			continue
		}
		first, _ := utf8.DecodeRuneInString(reason)
		if unicode.IsUpper(first) {
			t.Errorf("reason for %q starts with a capital: %q", code, reason)
		}
		for _, forbidden := range []string{"!", "—", ";", "."} {
			if strings.Contains(reason, forbidden) {
				t.Errorf("reason for %q contains %q: %q", code, forbidden, reason)
			}
		}
	}
}

func TestReason_UnknownCodeShowsNothing(t *testing.T) {
	if reason := probe.Reason("not_a_code"); reason != "" {
		t.Errorf("Reason(unknown) = %q, want an empty string", reason)
	}
}

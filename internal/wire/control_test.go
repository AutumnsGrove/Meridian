package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// updateGoldens rewrites the pinned wire contracts. It is never set by scripts/check.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files in testdata/wire")

const (
	restartID = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44"
	stopID    = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e45"
	requestAt = "2026-10-23T18:04:11Z"
)

func requestTime(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, requestAt)
	if err != nil {
		t.Fatalf("parse %s: %v", requestAt, err)
	}
	return at
}

// checkGolden pins encoded against the golden file, or rewrites it under -update. Byte equality
// (not semantic equality) is the point: field order and omitempty are part of the contract.
func checkGolden(t *testing.T, path string, encoded []byte) {
	t.Helper()
	encoded = append(encoded, '\n')
	if *updateGoldens {
		if err := os.WriteFile(path, encoded, 0o600); err != nil { //nolint:gosec // a fixed path under testdata, never input
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/wire -update to create it)", path, err)
	}
	if !bytes.Equal(want, encoded) {
		t.Fatalf("%s does not match the contract\n got:\n%s\nwant:\n%s", path, encoded, want)
	}
}

func validRequest(t *testing.T) wire.Request {
	t.Helper()
	return wire.Request{
		ID:          restartID,
		Action:      wire.ActionRestart,
		Target:      "bwmon",
		RequestedAt: requestTime(t),
	}
}

func TestValidateRequest_AcceptsWellFormedRequests(t *testing.T) {
	request := validRequest(t)
	stop := request
	stop.Action = wire.ActionStop
	stop.Ack = "stop-confirmed"
	dnsStop := stop
	dnsStop.Ack = "dns-stop-confirmed"
	cancel := request
	cancel.Action = wire.ActionCancelAutorestore
	cancel.Target = "resolver"
	cancel.Ref = stopID
	reboot := request
	reboot.Action = wire.ActionReboot
	reboot.Target = "host"
	reboot.Ack = "reboot-confirmed"
	// A clock a few seconds fast is tolerated in either direction.
	skewed := request
	skewed.RequestedAt = requestTime(t).Add(4 * time.Second)

	tests := []struct {
		name    string
		request wire.Request
	}{
		{"restart", request},
		{"stop", stop},
		{"dns stop", dnsStop},
		{"cancel autorestore", cancel},
		{"reboot", reboot},
		{"requested_at slightly in the future", skewed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := wire.ValidateRequest(test.request, requestTime(t)); err != nil {
				t.Fatalf("ValidateRequest(%+v) = %v, want nil", test.request, err)
			}
		})
	}
}

func TestValidateRequest_RefusesByCode(t *testing.T) {
	now := requestTime(t)
	base := func() wire.Request {
		return wire.Request{
			ID:          restartID,
			Action:      wire.ActionRestart,
			Target:      "bwmon",
			RequestedAt: now,
		}
	}

	tests := []struct {
		name   string
		change func(*wire.Request)
		code   wire.Code
	}{
		{"empty id", func(r *wire.Request) { r.ID = "" }, wire.CodeBadID},
		{"id is not a uuid", func(r *wire.Request) { r.ID = "not-a-uuid" }, wire.CodeBadID},
		{"id is uppercase", func(r *wire.Request) { r.ID = strings.ToUpper(restartID) }, wire.CodeBadID},
		{"id is a version 1 uuid", func(r *wire.Request) {
			r.ID = "c232ab00-9414-11ec-b3c8-9f6bdeced846"
		}, wire.CodeBadID},
		{"id has a path separator", func(r *wire.Request) { r.ID = "../../" + restartID }, wire.CodeBadID},
		{"empty action", func(r *wire.Request) { r.Action = "" }, wire.CodeBadAction},
		{"unknown action", func(r *wire.Request) { r.Action = "destroy" }, wire.CodeBadAction},
		{"action with a space", func(r *wire.Request) { r.Action = "re start" }, wire.CodeBadAction},
		{"empty target", func(r *wire.Request) { r.Target = "" }, wire.CodeBadTarget},
		{"target is uppercase", func(r *wire.Request) { r.Target = "Bwmon" }, wire.CodeBadTarget},
		{"target is a path", func(r *wire.Request) { r.Target = "../etc/passwd" }, wire.CodeBadTarget},
		{"target is too long", func(r *wire.Request) { r.Target = strings.Repeat("a", 33) }, wire.CodeBadTarget},
		{"ref on an action that takes none", func(r *wire.Request) { r.Ref = stopID }, wire.CodeBadRef},
		{"cancel without a ref", func(r *wire.Request) { r.Action = wire.ActionCancelAutorestore }, wire.CodeBadRef},
		{"cancel with a malformed ref", func(r *wire.Request) {
			r.Action = wire.ActionCancelAutorestore
			r.Ref = "stop"
		}, wire.CodeBadRef},
		{"no requested_at", func(r *wire.Request) { r.RequestedAt = time.Time{} }, wire.CodeBadRequest},
		{"requested_at far future", func(r *wire.Request) { r.RequestedAt = now.Add(6 * time.Second) }, wire.CodeBadRequest},
		{"ack is too long", func(r *wire.Request) { r.Ack = strings.Repeat("a", wire.MaxAckSize+1) }, wire.CodeBadAck},
		{"ack holds a NUL", func(r *wire.Request) { r.Ack = "stop\x00confirmed" }, wire.CodeBadAck},
		{"ack holds a newline", func(r *wire.Request) { r.Ack = "stop\nconfirmed" }, wire.CodeBadAck},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base()
			test.change(&request)
			err := wire.ValidateRequest(request, now)
			if err == nil {
				t.Fatalf("ValidateRequest(%+v) = nil, want a refusal", request)
			}
			var refusal *wire.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v, want a *wire.Refusal", err)
			}
			if refusal.Code != test.code {
				t.Errorf("code = %q, want %q", refusal.Code, test.code)
			}
			if refusal.Error() == "" {
				t.Error("Refusal.Error() is empty, want a description")
			}
		})
	}
}

func TestRequiredAck(t *testing.T) {
	tests := []struct {
		action wire.Action
		dns    bool
		want   string
	}{
		{wire.ActionStart, false, ""},
		{wire.ActionRestart, false, ""},
		{wire.ActionUpdate, false, ""},
		{wire.ActionStop, false, "stop-confirmed"},
		{wire.ActionStop, true, "dns-stop-confirmed"},
		{wire.ActionReboot, true, "reboot-confirmed"},
		{wire.ActionCancelAutorestore, true, ""},
		{wire.Action("unknown"), true, ""},
	}
	for _, test := range tests {
		t.Run(string(test.action), func(t *testing.T) {
			if got := wire.RequiredAck(test.action, test.dns); got != test.want {
				t.Errorf("RequiredAck(%q, %v) = %q, want %q", test.action, test.dns, got, test.want)
			}
		})
	}
}

func TestActionTableIsClosed(t *testing.T) {
	for _, action := range wire.KnownActions() {
		if !wire.IsAction(action) {
			t.Errorf("KnownActions lists %q, but IsAction says no", action)
		}
	}
	for _, action := range []wire.Action{"", "destroy", "Start", "restart "} {
		if wire.IsAction(action) {
			t.Errorf("IsAction(%q) = true, want false", action)
		}
	}
	for _, action := range wire.TargetActions() {
		if !wire.IsAction(action) {
			t.Errorf("TargetActions lists %q, which is not a known action", action)
		}
	}
}

func TestCodeSentences_CoverEveryCode(t *testing.T) {
	seen := make(map[wire.Code]bool)
	_ = seen
	for _, code := range wire.KnownCodes() {
		sentence := code.Sentence()
		if sentence == "" {
			t.Errorf("code %q has no sentence", code)
			continue
		}
		if strings.ContainsAny(sentence, "!\u2014") {
			t.Errorf("code %q sentence %q breaks the copy rules", code, sentence)
		}
	}
	if got := wire.Code("made-up").Sentence(); got == "" {
		t.Error("an unknown code has no fallback sentence")
	}
}

func TestDecodeRequest_ReadsTheGolden(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "wire", "request.golden.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	request, err := wire.DecodeRequest(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if request.ID != restartID || request.Action != wire.ActionRestart || request.Target != "bwmon" {
		t.Errorf("request = %+v, want the restart in the golden", request)
	}
	if !request.RequestedAt.Equal(requestTime(t)) {
		t.Errorf("RequestedAt = %v, want %v", request.RequestedAt, requestTime(t))
	}
	if err := wire.ValidateRequest(request, requestTime(t)); err != nil {
		t.Errorf("the golden does not validate: %v", err)
	}

	encoded, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, path, encoded)
}

func TestDecodeRequest_RefusesAnythingElse(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{"empty", ""},
		{"not an object", `[]`},
		{"unknown field", `{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"` + requestAt + `","extra":1}`},
		{"bad timestamp", `{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"yesterday"}`},
		{"trailing value", `{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"` + requestAt + `"} 1`},
		{"trailing garbage", `{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"` + requestAt + `"}x`},
		{"wrong type for id", `{"id":7,"action":"restart","target":"bwmon","requested_at":"` + requestAt + `"}`},
		{"too large", `{"id":"` + restartID + `","action":"restart","target":"bwmon","ack":"` +
			strings.Repeat("a", wire.MaxRequestSize) + `","requested_at":"` + requestAt + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := wire.DecodeRequest(strings.NewReader(test.document))
			if err == nil {
				t.Fatal("DecodeRequest returned no error, want one")
			}
			if !errors.Is(err, wire.ErrInvalidRequest) {
				t.Errorf("error = %v, want it to wrap ErrInvalidRequest", err)
			}
		})
	}
}

func TestDecodeResult_ReadsTheGolden(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "wire", "result.golden.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	result, err := wire.DecodeResult(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if result.ID != restartID || !result.OK || result.Action != wire.ActionRestart {
		t.Errorf("result = %+v, want the successful restart in the golden", result)
	}
	if result.DurationMS != 4820 || result.ExitCode != 0 {
		t.Errorf("duration/exit = %d/%d, want 4820/0", result.DurationMS, result.ExitCode)
	}
	if result.FinishedAt.Before(result.StartedAt) {
		t.Errorf("FinishedAt %v is before StartedAt %v", result.FinishedAt, result.StartedAt)
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, path, encoded)
}

func TestDecodeResult_RefusesAnythingElse(t *testing.T) {
	good := `{"id":"` + restartID + `","action":"restart","target":"bwmon","ok":true,"code":"",` +
		`"exit_code":0,"started_at":"` + requestAt + `","finished_at":"` + requestAt + `","duration_ms":1,` +
		`"output":"done","note":""}`
	if _, err := wire.DecodeResult(strings.NewReader(good)); err != nil {
		t.Fatalf("the well-formed result was refused: %v", err)
	}

	tests := []struct {
		name     string
		document string
	}{
		{"empty", ""},
		{"unknown field", strings.Replace(good, `"note":""`, `"note":"","extra":1`, 1)},
		{"missing id", strings.Replace(good, `"id":"`+restartID+`",`, "", 1)},
		{"missing started_at", strings.Replace(good, `"started_at":"`+requestAt+`",`, "", 1)},
		{"trailing data", good + " {}"},
		{"too large", strings.Replace(good, `"output":"done"`, `"output":"`+strings.Repeat("a", wire.MaxResultSize)+`"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := wire.DecodeResult(strings.NewReader(test.document))
			if err == nil {
				t.Fatal("DecodeResult returned no error, want one")
			}
			if !errors.Is(err, wire.ErrInvalidResult) {
				t.Errorf("error = %v, want it to wrap ErrInvalidResult", err)
			}
		})
	}
}

func TestDecodeAuditLine(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "wire", "audit.golden.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	line := strings.TrimSuffix(string(data), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("the audit golden is %d lines, want one", strings.Count(string(data), "\n"))
	}

	audit, err := wire.DecodeAuditLine([]byte(line))
	if err != nil {
		t.Fatalf("DecodeAuditLine: %v", err)
	}
	if audit.ID != restartID || audit.By != "tailnet" || !audit.OK {
		t.Errorf("audit = %+v, want the restart by the tailnet in the golden", audit)
	}
	if audit.DurationMS != 4820 {
		t.Errorf("DurationMS = %d, want 4820", audit.DurationMS)
	}

	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, path, encoded)

	for _, bad := range []string{"", "{}", "not json", `{"at":"yesterday"}`} {
		if _, err := wire.DecodeAuditLine([]byte(bad)); !errors.Is(err, wire.ErrInvalidAuditLine) {
			t.Errorf("DecodeAuditLine(%q) = %v, want ErrInvalidAuditLine", bad, err)
		}
	}
}

func TestRequestJSON_RoundTripsTheSameValue(t *testing.T) {
	request := validRequest(t)
	request.Ack = "stop-confirmed"

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := wire.DecodeRequest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeRequest(own output): %v", err)
	}
	if !sameRequest(request, again) {
		t.Errorf("round trip changed the request: %+v -> %+v", request, again)
	}
}

func sameRequest(left, right wire.Request) bool {
	return left.ID == right.ID &&
		left.Action == right.Action &&
		left.Target == right.Target &&
		left.Ack == right.Ack &&
		left.Ref == right.Ref &&
		left.RequestedAt.Equal(right.RequestedAt)
}

// FuzzDecodeRequest holds the request reader to three properties: a decode failure wraps the one
// sentinel, a decoded request always gets a verdict (a refusal from the closed table, never a panic),
// and re-encoding round-trips to the same value with the same verdict.
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"` + requestAt + `"}`))
	f.Add([]byte(`{"id":"` + restartID + `","action":"stop","target":"resolver","ack":"dns-stop-confirmed","requested_at":"` + requestAt + `"}`))
	f.Add([]byte(`{"id":"` + restartID + `","action":"cancel-autorestore","target":"resolver","ref":"` + stopID + `","requested_at":"` + requestAt + `"}`))
	f.Add([]byte(`{"id":"","action":"","target":"","requested_at":"` + requestAt + `"}`))
	f.Add([]byte(`{"id":"\u0000","action":"restart","target":"\u0000","requested_at":"` + requestAt + `"}`))
	f.Add([]byte(`{"id":"` + restartID + `","action":"restart","target":"bwmon","ack":"\u0000","requested_at":"0001-01-01T00:00:00Z"}`))
	f.Add([]byte(`{"id":"` + restartID + `","action":"restart","target":"bwmon","requested_at":"` + requestAt + `","nested":{"a":{"b":{"c":1}}}}`))

	now := time.Date(2026, 10, 23, 18, 4, 11, 0, time.UTC)
	f.Fuzz(func(t *testing.T, data []byte) {
		request, err := wire.DecodeRequest(bytes.NewReader(data))
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidRequest) {
				t.Fatalf("error %v does not wrap ErrInvalidRequest", err)
			}
			return
		}

		// Whatever decodes, ValidateRequest can pass judgement on without panicking, and its
		// verdict is always a refusal or acceptance, never some other error.
		if err := wire.ValidateRequest(request, now); err != nil {
			var refusal *wire.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("ValidateRequest error %v is not a *wire.Refusal", err)
			}
			if !wire.IsCode(refusal.Code) {
				t.Fatalf("refusal code %q is not in the closed table", refusal.Code)
			}
		}

		// A decoded request re-marshals to bytes that decode to the same value, and the copy keeps
		// the same verdict.
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		again, err := wire.DecodeRequest(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("re-decode of own output: %v", err)
		}
		if !sameRequest(request, again) {
			t.Fatalf("round trip changed the request: %+v -> %+v", request, again)
		}
		firstErr := wire.ValidateRequest(request, now)
		secondErr := wire.ValidateRequest(again, now)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("round trip changed the verdict: %v then %v", firstErr, secondErr)
		}
	})
}

// FuzzDecodeResult holds the result reader to two properties: a decode failure wraps the one
// sentinel, and whatever decodes round-trips through its own encoding field for field.
func FuzzDecodeResult(f *testing.F) {
	f.Add([]byte(`{"id":"` + restartID + `","action":"restart","target":"bwmon","ok":true,"code":"","exit_code":0,` +
		`"started_at":"` + requestAt + `","finished_at":"` + requestAt + `","duration_ms":1,"output":"done","note":""}`))
	f.Add([]byte(`{"id":"` + restartID + `","action":"stop","target":"resolver","ok":false,"code":"timeout",` +
		`"exit_code":124,"started_at":"` + requestAt + `","finished_at":"` + requestAt + `","duration_ms":90000,` +
		`"output":"","note":"timed out","restore_at":"2026-10-23T18:14:11Z","ref":"` + stopID + `","in_progress":true}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"id":"\u0000","output":"\u0000\u001b[2J"}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := wire.DecodeResult(bytes.NewReader(data))
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidResult) {
				t.Fatalf("error %v does not wrap ErrInvalidResult", err)
			}
			return
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		again, err := wire.DecodeResult(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("re-decode of own output: %v", err)
		}
		if result.ID != again.ID || result.Action != again.Action || result.Target != again.Target ||
			result.OK != again.OK || result.Code != again.Code || result.ExitCode != again.ExitCode ||
			result.DurationMS != again.DurationMS || result.Output != again.Output ||
			result.Note != again.Note || result.Ref != again.Ref ||
			result.InProgress != again.InProgress ||
			!result.StartedAt.Equal(again.StartedAt) ||
			!result.FinishedAt.Equal(again.FinishedAt) ||
			(result.RestoreAt == nil) != (again.RestoreAt == nil) ||
			(result.RestoreAt != nil && !result.RestoreAt.Equal(*again.RestoreAt)) {
			t.Fatalf("round trip changed the result: %+v -> %+v", result, again)
		}
	})
}

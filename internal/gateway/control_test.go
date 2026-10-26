package gateway_test

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// controlDirs are the directories a mounted control plane has, and the paths inside them.
type controlDirs struct {
	inbox   string
	outbox  string
	results string
	queue   string
}

// mountControl gives the gateway a real control plane on temporary directories, the way the compose
// mounts give it one on the box. It returns the directories so a test can read what was written.
func (hub *testGateway) mountControl(t *testing.T) controlDirs {
	t.Helper()
	root := t.TempDir()
	dirs := controlDirs{
		inbox:   filepath.Join(root, "inbox"),
		outbox:  filepath.Join(root, "outbox"),
		results: filepath.Join(root, "outbox", "results"),
		queue:   filepath.Join(root, "inbox", "queue"),
	}
	for _, dir := range []string{filepath.Join(dirs.inbox, "tmp"), dirs.queue, dirs.results} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	hub.cfg.Control.Reboot = true
	hub.cfg.Control.QueueDepth = 4
	hub.cfg.Control.AuditLines = 20
	hub.cfg.Control.AuditRetention = config.Duration(365 * 24 * time.Hour)
	hub.cfg.Control.InboxDir = dirs.inbox
	hub.cfg.Control.OutboxDir = dirs.outbox
	hub.cfg.Services[0].Control = &config.ServiceControl{
		Target: "one", Actions: []string{"start", "stop", "restart", "update"},
	}
	hub.deps.Control = &control.Plane{
		Config: func() *config.Config {
			current, _ := hub.Current()
			return current
		},
		Now: func() time.Time { return testNow },
	}
	return dirs
}

// postControl drives POST /api/control with the headers a same-origin page would send.
func (hub *testGateway) postControl(t *testing.T, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://hub.example.org/api/control", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	hub.deps.Handler().ServeHTTP(recorder, request)
	return recorder
}

// errorCode reads the {"code": ...} shape every control refusal uses.
func errorCode(t *testing.T, response *httptest.ResponseRecorder) wire.Code {
	t.Helper()
	var body struct {
		Code wire.Code `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not the error shape: %v", response.Body.String(), err)
	}
	return body.Code
}

func TestControl_AcceptsATapAndQueuesIt(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)

	response := hub.postControl(t, `{"action":"restart","target":"one"}`, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", response.Code, response.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", response.Body.String(), err)
	}
	if _, err := wire.ParseID(body.ID); err != nil {
		t.Fatalf("id %q is not canonical: %v", body.ID, err)
	}

	entries, err := os.ReadDir(dirs.queue)
	if err != nil {
		t.Fatalf("read the queue: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != body.ID+".json" {
		t.Fatalf("queue contents = %v, want only %s.json", entries, body.ID)
	}
	file, err := os.Open(filepath.Join(dirs.queue, body.ID+".json"))
	if err != nil {
		t.Fatalf("open the queued request: %v", err)
	}
	defer func() { _ = file.Close() }()
	request, err := wire.DecodeRequest(file)
	if err != nil {
		t.Fatalf("the queued file is not a request: %v", err)
	}
	if request.Action != wire.ActionRestart || request.Target != "one" {
		t.Errorf("request = %+v, want a restart of one", request)
	}
	if !request.RequestedAt.Equal(testNow) {
		t.Errorf("RequestedAt = %v, want the gateway's clock %v", request.RequestedAt, testNow)
	}
}

func TestControl_AnswersRefusalsWithTheirCode(t *testing.T) {
	tests := []struct {
		name string
		body string
		fill int
		want int
		code wire.Code
	}{
		{
			name: "unknown target",
			body: `{"action":"restart","target":"nope"}`,
			want: http.StatusBadRequest, code: wire.CodeUnknownTarget,
		},
		{
			name: "unknown action",
			body: `{"action":"destroy","target":"one"}`,
			want: http.StatusBadRequest, code: wire.CodeBadAction,
		},
		{
			name: "target that is not a slug",
			body: `{"action":"restart","target":"One/../x"}`,
			want: http.StatusBadRequest, code: wire.CodeBadTarget,
		},
		{
			name: "stop without an ack",
			body: `{"action":"stop","target":"one"}`,
			want: http.StatusBadRequest, code: wire.CodeMissingAck,
		},
		{
			name: "unknown field",
			body: `{"action":"restart","target":"one","run":"rm -rf /"}`,
			want: http.StatusBadRequest, code: wire.CodeBadRequest,
		},
		{
			name: "not JSON at all",
			body: `rm -rf /`,
			want: http.StatusBadRequest, code: wire.CodeBadRequest,
		},
		{
			name: "queue full",
			body: `{"action":"start","target":"one"}`,
			fill: 4,
			want: http.StatusTooManyRequests, code: wire.CodeQueueFull,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub := newTestGateway(t)
			dirs := hub.mountControl(t)
			for i := 0; i < test.fill; i++ {
				id, err := wire.NewID(testNow.Add(-time.Duration(i+1)*time.Second), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				pending := wire.Request{ID: id, Action: wire.ActionRestart, Target: "one", RequestedAt: testNow}
				data, err := json.Marshal(pending)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dirs.queue, id+".json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			response := hub.postControl(t, test.body, nil)
			if response.Code != test.want {
				t.Fatalf("status = %d (%s), want %d", response.Code, response.Body.String(), test.want)
			}
			if code := errorCode(t, response); code != test.code {
				t.Errorf("code = %q, want %q", code, test.code)
			}
		})
	}
}

func TestControl_RefusesAnIdenticalRequestWithAConflict(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)

	if response := hub.postControl(t, `{"action":"restart","target":"one"}`, nil); response.Code != http.StatusAccepted {
		t.Fatalf("the first request = %d, want 202", response.Code)
	}
	// Remove the first request from the queue and write a fake pending one with the same choice:
	// the container refuses it before it ever reaches the executor.
	id, err := wire.NewID(testNow.Add(-time.Minute), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pending := wire.Request{ID: id, Action: wire.ActionRestart, Target: "one", RequestedAt: testNow.Add(-time.Minute)}
	data, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.queue, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	response := hub.postControl(t, `{"action":"restart","target":"one"}`, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", response.Code, response.Body.String())
	}
	if code := errorCode(t, response); code != wire.CodeCoalesced {
		t.Errorf("code = %q, want coalesced", code)
	}
}

func TestControl_WithoutAPlaneBothRoutesSaySo(t *testing.T) {
	hub := newTestGateway(t)

	post := hub.postControl(t, `{"action":"restart","target":"one"}`, nil)
	if post.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST status = %d, want 503", post.Code)
	}
	if code := errorCode(t, post); code != wire.CodeUnavailable {
		t.Errorf("POST code = %q, want unavailable", code)
	}

	get := hub.request(t, http.MethodGet, "http://hub.example.org/api/control/status", nil)
	if get.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET status = %d, want 503", get.Code)
	}
	if code := errorCode(t, get); code != wire.CodeUnavailable {
		t.Errorf("GET code = %q, want unavailable", code)
	}
}

func TestControl_TheMutatingRouteGoesThroughTheBrowserGuard(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		headers map[string]string
	}{
		{name: "not JSON", headers: map[string]string{"Content-Type": "text/plain"}},
		{name: "another site", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{name: "another origin", headers: map[string]string{"Origin": "https://evil.example.org"}},
		{name: "over the body cap", body: `{"action":"` + strings.Repeat("a", 9<<10) + `"}`},
	}
	const bareRequest = `{"action":"restart","target":"one"}`
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub := newTestGateway(t)
			dirs := hub.mountControl(t)
			body := test.body
			if body == "" {
				body = bareRequest
			}
			response := hub.postControl(t, body, test.headers)
			if response.Code == http.StatusAccepted {
				t.Fatalf("a guarded request was accepted: %s", response.Body.String())
			}
			if entries, err := os.ReadDir(dirs.queue); err != nil || len(entries) != 0 {
				t.Fatalf("a guarded request wrote a file: %v, %v", entries, err)
			}
		})
	}

	// The same page's GET needs none of it: reading is not an action.
	hub := newTestGateway(t)
	hub.mountControl(t)
	if response := hub.request(t, http.MethodGet, "http://hub.example.org/api/control/status", nil); response.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200", response.Code)
	}
}

func TestControl_StatusReturnsTheView(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)

	result := wire.Result{
		ID: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44", Action: wire.ActionRestart, Target: "one",
		OK: true, StartedAt: testNow.Add(-time.Minute), FinishedAt: testNow.Add(-time.Minute + 2*time.Second),
		DurationMS: 2000, Output: "active (running)",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.results, result.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	audit := wire.AuditLine{
		At: result.FinishedAt, ID: result.ID, Action: wire.ActionRestart, Target: "one",
		OK: true, DurationMS: 2000, By: wire.AuditBy, Output: "active (running)",
	}
	auditData, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.outbox, "audit.jsonl"), append(auditData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	response := hub.request(t, http.MethodGet, "http://hub.example.org/api/control/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", response.Code, response.Body.String())
	}
	var view control.StatusView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("body %q is not a control view: %v", response.Body.String(), err)
	}
	if len(view.Results) != 1 || view.Results[0].ID != result.ID {
		t.Fatalf("results = %+v, want the written result", view.Results)
	}
	if view.Results[0].Sentence != "" {
		t.Errorf("sentence for a successful result = %q, want empty", view.Results[0].Sentence)
	}
	if len(view.Audit) != 1 || view.Audit[0].ID != result.ID {
		t.Errorf("audit = %+v, want the written line", view.Audit)
	}
	if view.Executor.State != control.ExecutorUnknown {
		t.Errorf("executor = %+v, want unknown without a host snapshot", view.Executor)
	}
	if len(view.Pending) != 0 {
		t.Errorf("pending = %+v, want none", view.Pending)
	}
}

func TestControl_StatusGolden(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)
	hub.deps.Control.Host = func() (wire.HostSnapshot, bool) {
		return wire.HostSnapshot{
			At: testNow.Add(-5 * time.Second),
			Units: map[string]wire.UnitState{
				control.ExecutorUnit: {Active: true, Since: testNow.Add(-90 * time.Minute)},
			},
		}, true
	}
	hub.cfg.HostStatus = &config.HostStatus{MaxAge: config.Duration(2 * time.Minute)}

	// One request waiting, one result with an auto-restore counting down, and one audit line.
	pending := wire.Request{
		ID: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e10", Action: wire.ActionStop, Target: "one",
		Ack: "stop-confirmed", RequestedAt: testNow.Add(-20 * time.Second),
	}
	pendingData, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.queue, pending.ID+".json"), pendingData, 0o600); err != nil {
		t.Fatal(err)
	}

	restoreAt := testNow.Add(9 * time.Minute)
	stop := wire.Result{
		ID: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44", Action: wire.ActionStop, Target: "one",
		OK: true, Code: "", StartedAt: testNow.Add(-90 * time.Second), FinishedAt: testNow.Add(-88 * time.Second),
		DurationMS: 2000, Output: "inactive (dead)", Note: "auto-restore scheduled",
		RestoreAt: &restoreAt,
	}
	stopData, err := json.Marshal(stop)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.results, stop.ID+".json"), stopData, 0o600); err != nil {
		t.Fatal(err)
	}
	refused := wire.Result{
		ID: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e45", Action: wire.ActionUpdate, Target: "one",
		OK: false, Code: wire.CodeActionNotAllowed, ExitCode: 0,
		StartedAt: testNow.Add(-4 * time.Minute), FinishedAt: testNow.Add(-4 * time.Minute),
	}
	refusedData, err := json.Marshal(refused)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.results, refused.ID+".json"), refusedData, 0o600); err != nil {
		t.Fatal(err)
	}

	audit := wire.AuditLine{
		At: stop.FinishedAt, ID: stop.ID, Action: wire.ActionStop, Target: "one", OK: true,
		DurationMS: 2000, By: wire.AuditBy, Output: "inactive (dead)",
	}
	auditData, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.outbox, "audit.jsonl"), append(auditData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	response := hub.request(t, http.MethodGet, "http://hub.example.org/api/control/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", response.Code, response.Body.String())
	}

	// The control view is not /api/status, so its golden lives beside its own contract rather
	// than in testdata/api, which the web client parses as status documents.
	path := filepath.Join("..", "..", "testdata", "control", "status.golden.json")
	encoded := response.Body.Bytes()
	if *updateGoldens {
		pretty, err := json.MarshalIndent(json.RawMessage(encoded), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(pretty, '\n'), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/gateway -update to create it)", path, err)
	}
	if string(want) != prettyJSON(t, encoded) {
		t.Fatalf("%s does not match the contract\n got:\n%s\nwant:\n%s", path, prettyJSON(t, encoded), want)
	}
}

func prettyJSON(t *testing.T, data []byte) string {
	t.Helper()
	pretty, err := json.MarshalIndent(json.RawMessage(data), "", "  ")
	if err != nil {
		t.Fatalf("indent %q: %v", data, err)
	}
	return string(pretty) + "\n"
}

// TestStatus_ReportsThePlaneItActuallyHas keeps the launcher's control block honest: availability is
// read from the directories, not from a flag somebody set at startup.
func TestStatus_ReportsThePlaneItActuallyHas(t *testing.T) {
	hub := newTestGateway(t)
	hub.mountControl(t)
	if status := pollStatus(t, hub, ""); !status.Control.Available || status.Control.Reason != "" {
		t.Fatalf("control = %+v, want available with no reason", status.Control)
	}

	without := newTestGateway(t)
	status := pollStatus(t, without, "")
	if status.Control.Available || status.Control.Reason != "control plane not installed" {
		t.Fatalf("control = %+v, want the not-installed reason", status.Control)
	}
}

package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/updater"
	"github.com/autumnsgrove/meridian/internal/wire"
)

func withUpdate(t *testing.T, hub *testGateway) *config.Config {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatalf("parse the update config: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the update config should not warn: %v", warnings)
	}
	parsed.Services[0].Control = &config.ServiceControl{Target: "one", Actions: []string{"restart", "update"}}
	parsed.Update = &config.Update{
		Image:      "ghcr.io/owner/name",
		PublishURL: "https://api.example.org/runs",
		SignalDir:  "/update-signal",
		Target:     "one",
	}
	hub.cfg = parsed
	return parsed
}

func decodeUpdate(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("decode /api/update: %v", err)
	}
	return document
}

func TestUpdate_ReportsNotConfiguredWithoutAnUpdateBlock(t *testing.T) {
	hub := newTestGateway(t)
	called := false
	hub.deps.Update = func(context.Context, *config.Update) (updater.Resolution, error) {
		called = true
		return updater.Resolution{}, nil
	}

	recorder := hub.request(t, http.MethodGet, "http://hub.example.org/api/update", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	document := decodeUpdate(t, recorder.Body.Bytes())
	if document["configured"] != false {
		t.Errorf("configured = %v, want false", document["configured"])
	}
	if document["version"] != "test-version" {
		t.Errorf("version = %v, want test-version", document["version"])
	}
	if called {
		t.Error("the resolver ran although the config has no update block")
	}
}

func TestUpdate_ReportsTheResolvedDigest(t *testing.T) {
	hub := newTestGateway(t)
	withUpdate(t, hub)
	checkedAt := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	hub.deps.Update = func(_ context.Context, settings *config.Update) (updater.Resolution, error) {
		if settings == nil || settings.Image != "ghcr.io/owner/name" {
			t.Fatalf("resolver settings = %+v, want the update block", settings)
		}
		return updater.Resolution{
			Digest:    wire.Digest("sha256:" + strings.Repeat("a", 64)),
			Commit:    strings.Repeat("1", 40),
			CheckedAt: checkedAt,
			Available: true,
		}, nil
	}

	recorder := hub.request(t, http.MethodGet, "http://hub.example.org/api/update", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	document := decodeUpdate(t, recorder.Body.Bytes())
	if document["configured"] != true {
		t.Errorf("configured = %v, want true", document["configured"])
	}
	if document["digest"] != "sha256:"+strings.Repeat("a", 64) {
		t.Errorf("digest = %v", document["digest"])
	}
	if document["commit"] != strings.Repeat("1", 40) {
		t.Errorf("commit = %v", document["commit"])
	}
	if document["available"] != true {
		t.Errorf("available = %v, want true", document["available"])
	}
	if document["checked_at"] != checkedAt.Format(time.RFC3339) {
		t.Errorf("checked_at = %v, want %v", document["checked_at"], checkedAt.Format(time.RFC3339))
	}
	if _, present := document["error"]; present {
		t.Errorf("error = %v, want the key absent on success", document["error"])
	}
}

func TestUpdate_ReportsAFailedCheckWithoutFailingTheRoute(t *testing.T) {
	hub := newTestGateway(t)
	withUpdate(t, hub)
	hub.deps.Update = func(context.Context, *config.Update) (updater.Resolution, error) {
		return updater.Resolution{}, updater.ErrRegistry
	}

	recorder := hub.request(t, http.MethodGet, "http://hub.example.org/api/update", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the route reports the failure, it does not become one", recorder.Code)
	}
	document := decodeUpdate(t, recorder.Body.Bytes())
	if document["configured"] != true {
		t.Errorf("configured = %v, want true", document["configured"])
	}
	message, _ := document["error"].(string)
	if message == "" {
		t.Fatal("error is empty, want a short sentence")
	}
	if strings.Contains(strings.ToLower(message), "updater:") {
		t.Errorf("error = %q, want a sentence without an internal prefix", message)
	}
	if document["available"] != false {
		t.Errorf("available = %v, want false after a failed check", document["available"])
	}
}

func TestUpdate_IsReadOnly(t *testing.T) {
	hub := newTestGateway(t)
	recorder := hub.request(t, http.MethodPost, "http://hub.example.org/api/update", map[string]string{
		"Content-Type":   "application/json",
		"Sec-Fetch-Site": "same-origin",
	})
	// The method-specific route does not match, so the request falls through to the app shell, which
	// refuses an API path: the only thing that matters is that no resolver ran and nothing changed.
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("POST /api/update status = %d, want 404: the tap is POST /api/control", recorder.Code)
	}
}

func selfUpdateConfig(t *testing.T, hub *testGateway) (signalDir string, digest wire.Digest) {
	t.Helper()
	signalDir = filepath.Join(t.TempDir(), "update-signal")
	if err := os.Mkdir(signalDir, 0o700); err != nil {
		t.Fatalf("create the signal directory: %v", err)
	}
	digest = wire.Digest("sha256:" + strings.Repeat("c", 64))
	hub.cfg.Update = &config.Update{
		Image:      "ghcr.io/owner/name",
		PublishURL: "https://api.example.org/runs",
		SignalDir:  signalDir,
		Target:     "one",
	}
	return signalDir, digest
}

func TestControl_SelfUpdateWritesTheHintBeforeQueueing(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)
	signalDir, digest := selfUpdateConfig(t, hub)
	called := false
	hub.deps.Update = func(_ context.Context, settings *config.Update) (updater.Resolution, error) {
		called = true
		if settings == nil || settings.Target != "one" {
			t.Fatalf("resolver settings = %+v, want the update block for one", settings)
		}
		return updater.Resolution{Digest: digest, Available: true}, nil
	}

	response := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", response.Code, response.Body.String())
	}
	if !called {
		t.Fatal("the update request was queued without resolving a digest")
	}
	data, err := os.ReadFile(filepath.Join(signalDir, "requested"))
	if err != nil {
		t.Fatalf("read the hint: %v", err)
	}
	if got := string(data); got != digest.String() {
		t.Errorf("hint = %q, want %q", got, digest)
	}
	entries, err := os.ReadDir(dirs.queue)
	if err != nil {
		t.Fatalf("read the queue: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("queue holds %d requests, want 1", len(entries))
	}
}

func TestControl_SelfUpdateRefusesWhenTheCheckFails(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)
	signalDir, _ := selfUpdateConfig(t, hub)
	hub.deps.Update = func(context.Context, *config.Update) (updater.Resolution, error) {
		return updater.Resolution{}, updater.ErrRegistry
	}

	response := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d (%s), want 503", response.Code, response.Body.String())
	}
	if code := errorCode(t, response); code != wire.CodeUpdateFailed {
		t.Errorf("code = %q, want %q", code, wire.CodeUpdateFailed)
	}
	if entries, _ := os.ReadDir(dirs.queue); len(entries) != 0 {
		t.Errorf("a failed check still queued %d requests", len(entries))
	}
	if _, err := os.Stat(filepath.Join(signalDir, "requested")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed check still wrote the hint: %v", err)
	}
}

// TestControl_SelfUpdateResolvesNothingForATapThatWillBeRefused: resolving a digest can wait out a
// four minute build and writes the hint the host reads, so a tap the queue is going to refuse must be
// refused first.
func TestControl_SelfUpdateResolvesNothingForATapThatWillBeRefused(t *testing.T) {
	hub := newTestGateway(t)
	dirs := hub.mountControl(t)
	signalDir, digest := selfUpdateConfig(t, hub)
	calls := 0
	hub.deps.Update = func(context.Context, *config.Update) (updater.Resolution, error) {
		calls++
		return updater.Resolution{Digest: digest, Available: true}, nil
	}

	first := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d (%s), want 202", first.Code, first.Body.String())
	}
	if err := os.Remove(filepath.Join(signalDir, "requested")); err != nil {
		t.Fatalf("clear the first hint: %v", err)
	}

	second := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d (%s), want 409 for a duplicate", second.Code, second.Body.String())
	}
	if calls != 1 {
		t.Errorf("the resolver ran %d times, want once: the duplicate was refused before it", calls)
	}
	if _, err := os.Stat(filepath.Join(signalDir, "requested")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused tap still wrote the hint: %v", err)
	}
	if entries, _ := os.ReadDir(dirs.queue); len(entries) != 1 {
		t.Errorf("queue holds %d requests, want 1", len(entries))
	}
}

func TestControl_OtherUpdatesDoNotResolveADigest(t *testing.T) {
	hub := newTestGateway(t)
	hub.mountControl(t)
	called := false
	hub.deps.Update = func(context.Context, *config.Update) (updater.Resolution, error) {
		called = true
		return updater.Resolution{}, nil
	}

	response := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", response.Code, response.Body.String())
	}
	if called {
		t.Error("the resolver ran for a target no update block names")
	}
}

func TestControl_SelfUpdateWithoutAResolverIsRefused(t *testing.T) {
	hub := newTestGateway(t)
	hub.mountControl(t)
	selfUpdateConfig(t, hub)

	response := hub.postControl(t, `{"action":"update","target":"one"}`, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d (%s), want 503", response.Code, response.Body.String())
	}
	if code := errorCode(t, response); code != wire.CodeUpdateFailed {
		t.Errorf("code = %q, want %q", code, wire.CodeUpdateFailed)
	}
}

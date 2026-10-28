package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/updater"
	"github.com/autumnsgrove/meridian/internal/wire"
)

const updateConfig = `
update:
  image: ghcr.io/owner/name
  publish_url: https://api.example.org/runs
`

func withUpdate(t *testing.T, hub *testGateway) *config.Config {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(testConfig + updateConfig))
	if err != nil {
		t.Fatalf("parse the update config: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the update config should not warn: %v", warnings)
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

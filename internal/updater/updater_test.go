package updater_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/updater"
	"github.com/autumnsgrove/meridian/internal/wire"
)

var (
	resolvedAt   = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	runningSHA   = strings.Repeat("1", 40)
	newerSHA     = strings.Repeat("2", 40)
	olderDigest  = wire.Digest("sha256:" + strings.Repeat("a", 64))
	newestDigest = wire.Digest("sha256:" + strings.Repeat("b", 64))
)

// fakeClock is the time seam: sleeps move the clock instead of waiting, and the test can hook one
// to flip the fake registry's state at the moment the resolver decides to wait.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  []time.Duration
	onSleep func(time.Duration)
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: resolvedAt}
}

func (clock *fakeClock) seams() updater.Clock {
	return updater.Clock{Now: clock.Now, Sleep: clock.Sleep}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Sleep(ctx context.Context, delay time.Duration) error {
	clock.mu.Lock()
	clock.sleeps = append(clock.sleeps, delay)
	clock.now = clock.now.Add(delay)
	hook := clock.onSleep
	clock.mu.Unlock()
	if hook != nil {
		hook(delay)
	}
	return ctx.Err()
}

func (clock *fakeClock) recorded() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.sleeps...)
}

func (clock *fakeClock) totalSlept() time.Duration {
	var total time.Duration
	for _, delay := range clock.recorded() {
		total += delay
	}
	return total
}

// registryStub is GHCR's two-step protocol: an anonymous token, then a manifest HEAD whose digest is
// in the header. It records what the client sent so the protocol is asserted, not assumed.
type registryStub struct {
	server      *httptest.Server
	digest      atomic.Value
	tokenHits   atomic.Int64
	manifestHit atomic.Int64
	method      atomic.Value
	authorize   atomic.Value
	accept      atomic.Value
	status      atomic.Int64
}

func newRegistryStub(t *testing.T, digest wire.Digest) *registryStub {
	t.Helper()
	stub := &registryStub{}
	stub.digest.Store(digest.String())
	stub.status.Store(http.StatusOK)
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("service") != "ghcr.io" {
				t.Errorf("token service = %q, want ghcr.io", r.URL.Query().Get("service"))
			}
			if want := "repository:autumnsgrove/meridian:pull"; r.URL.Query().Get("scope") != want {
				t.Errorf("token scope = %q, want %q", r.URL.Query().Get("scope"), want)
			}
			stub.tokenHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		case "/v2/autumnsgrove/meridian/manifests/latest":
			stub.manifestHit.Add(1)
			stub.method.Store(r.Method)
			stub.authorize.Store(r.Header.Get("Authorization"))
			stub.accept.Store(r.Header.Get("Accept"))
			if status := int(stub.status.Load()); status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Docker-Content-Digest", stub.digest.Load().(string))
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected registry path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// publishStub is the repository's status endpoint. The first response in the list is served, then
// the last one repeats, so a test can script "in progress, then complete".
type publishStub struct {
	server *httptest.Server
	hits   atomic.Int64
	mu     sync.Mutex
	bodies []string
	status int
}

func newPublishStub(t *testing.T, bodies ...string) *publishStub {
	t.Helper()
	stub := &publishStub{bodies: bodies, status: http.StatusOK}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(stub.hits.Add(1)) - 1
		stub.mu.Lock()
		if index >= len(stub.bodies) {
			index = len(stub.bodies) - 1
		}
		body, status := stub.bodies[index], stub.status
		stub.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *publishStub) respond(body string) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.bodies = append(stub.bodies, body)
}

func publishRun(sha, status string) string {
	return `{"workflow_runs":[{"head_sha":"` + sha + `","status":"` + status + `"}]}`
}

func newSource(registry *registryStub, publish *publishStub) updater.Source {
	return updater.Source{
		Repo:        wire.ImageRepo("ghcr.io/autumnsgrove/meridian"),
		RegistryURL: registry.server.URL,
		PublishURL:  publish.server.URL,
	}
}

func fetcher() probe.Fetcher {
	return probe.NewHTTPFetcher(nil, "meridian-test", 1<<20)
}

func TestResolve_ResolvesTheManifestDigest(t *testing.T) {
	registry := newRegistryStub(t, newestDigest)
	publish := newPublishStub(t, publishRun(runningSHA, "completed"))
	clock := newFakeClock()

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, clock.seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Digest != newestDigest {
		t.Errorf("digest = %q, want %q", result.Digest, newestDigest)
	}
	if result.Commit != runningSHA {
		t.Errorf("commit = %q, want %q", result.Commit, runningSHA)
	}
	if !result.Available {
		t.Error("available = false, want true for a different digest")
	}
	if !result.CheckedAt.Equal(resolvedAt) {
		t.Errorf("checked at = %v, want %v", result.CheckedAt, resolvedAt)
	}
	if got := registry.tokenHits.Load(); got != 1 {
		t.Errorf("token requests = %d, want 1", got)
	}
	if got := registry.manifestHit.Load(); got != 1 {
		t.Errorf("manifest requests = %d, want 1", got)
	}
	if got, _ := registry.method.Load().(string); got != http.MethodHead {
		t.Errorf("manifest method = %q, want HEAD", got)
	}
	if got, _ := registry.authorize.Load().(string); got != "Bearer registry-token" {
		t.Errorf("manifest Authorization = %q, want the token", got)
	}
	if got, _ := registry.accept.Load().(string); !strings.Contains(got, "application/vnd.oci.image.index.v1+json") {
		t.Errorf("manifest Accept = %q, want the OCI index type", got)
	}
	if got := len(clock.recorded()); got != 0 {
		t.Errorf("sleeps = %v, want none when the build is already published", clock.recorded())
	}
}

func TestResolve_ReportsAlreadyCurrent(t *testing.T) {
	registry := newRegistryStub(t, olderDigest)
	publish := newPublishStub(t, publishRun(runningSHA, "completed"))

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, newFakeClock().seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Available {
		t.Errorf("available = true with digest %q already running", result.Digest)
	}
}

func TestResolve_WaitsForAnInFlightBuild(t *testing.T) {
	registry := newRegistryStub(t, olderDigest)
	publish := newPublishStub(t, publishRun(newerSHA, "in_progress"))
	clock := newFakeClock()
	clock.onSleep = func(time.Duration) {
		// The build finishes while the resolver waits.
		publish.respond(publishRun(newerSHA, "completed"))
		registry.digest.Store(newestDigest.String())
	}

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, clock.seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Digest != newestDigest {
		t.Errorf("digest = %q, want the newly published %q", result.Digest, newestDigest)
	}
	if !result.Available {
		t.Error("available = false, want true after the build finished")
	}
	if got := publish.hits.Load(); got < 2 {
		t.Errorf("publish requests = %d, want at least 2 (the build was still running)", got)
	}
	if got := len(clock.recorded()); got == 0 {
		t.Error("Resolve did not wait for the in-flight build")
	}
	if got := clock.recorded()[0]; got <= 0 || got > updater.MaxWait {
		t.Errorf("first wait = %v, want a positive backoff inside %v", got, updater.MaxWait)
	}
}

func TestResolve_WaitsWhenTheDigestHasNotMovedYet(t *testing.T) {
	registry := newRegistryStub(t, olderDigest)
	publish := newPublishStub(t, publishRun(newerSHA, "completed"))
	clock := newFakeClock()
	clock.onSleep = func(time.Duration) {
		// The workflow reports complete just before the tag flips.
		registry.digest.Store(newestDigest.String())
	}

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, clock.seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Digest != newestDigest {
		t.Errorf("digest = %q, want %q after the tag moved", result.Digest, newestDigest)
	}
	if got := registry.manifestHit.Load(); got < 2 {
		t.Errorf("manifest requests = %d, want at least 2", got)
	}
}

func TestResolve_GivesUpWaitingAtTheDeadline(t *testing.T) {
	registry := newRegistryStub(t, olderDigest)
	publish := newPublishStub(t, publishRun(newerSHA, "queued"))
	clock := newFakeClock()

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, clock.seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Digest != olderDigest {
		t.Errorf("digest = %q, want the still-published %q", result.Digest, olderDigest)
	}
	if result.Available {
		t.Error("available = true although the only digest is the running one")
	}
	if got, limit := clock.totalSlept(), updater.MaxWait; got > limit {
		t.Errorf("Resolve waited %v, more than the %v cap", got, limit)
	}
	if got := clock.totalSlept(); got < updater.MaxWait {
		t.Errorf("Resolve waited only %v, want the full %v cap before giving up", got, updater.MaxWait)
	}
}

func TestResolve_FallsBackWhenThePublishEndpointFails(t *testing.T) {
	registry := newRegistryStub(t, newestDigest)
	publish := newPublishStub(t, `{"message":"rate limited"}`)
	publish.status = http.StatusInternalServerError

	result, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, newFakeClock().seams())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Digest != newestDigest {
		t.Errorf("digest = %q, want %q (the manifest is still authoritative)", result.Digest, newestDigest)
	}
	if !result.Available {
		t.Error("available = false, want true for a different digest")
	}
}

func TestResolve_RejectsAMalformedManifestDigest(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"uppercase hex", "sha256:" + strings.Repeat("A", 64)},
		{"short", "sha256:" + strings.Repeat("a", 63)},
		{"other algorithm", "sha512:" + strings.Repeat("a", 64)},
		{"a tag", "latest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := newRegistryStub(t, newestDigest)
			registry.digest.Store(test.value)
			publish := newPublishStub(t, publishRun(runningSHA, "completed"))

			_, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
				updater.Running{Commit: runningSHA, Digest: olderDigest}, newFakeClock().seams())
			if err == nil {
				t.Fatalf("Resolve accepted the manifest digest %q", test.value)
			}
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Errorf("error = %v, want ErrInvalidDigest", err)
			}
		})
	}
}

func TestResolve_ReportsRegistryFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"manifest unauthorized", http.StatusUnauthorized},
		{"manifest missing", http.StatusNotFound},
		{"registry unavailable", http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := newRegistryStub(t, newestDigest)
			registry.status.Store(int64(test.status))
			publish := newPublishStub(t, publishRun(runningSHA, "completed"))

			_, err := updater.Resolve(context.Background(), fetcher(), newSource(registry, publish),
				updater.Running{Commit: runningSHA, Digest: olderDigest}, newFakeClock().seams())
			if !errors.Is(err, updater.ErrRegistry) {
				t.Fatalf("error = %v, want ErrRegistry", err)
			}
		})
	}
}

func TestResolve_RefusesARepositoryThatWasNotValidated(t *testing.T) {
	registry := newRegistryStub(t, newestDigest)
	publish := newPublishStub(t, publishRun(runningSHA, "completed"))
	source := newSource(registry, publish)
	source.Repo = wire.ImageRepo("docker.io/autumnsgrove/meridian")

	_, err := updater.Resolve(context.Background(), fetcher(), source,
		updater.Running{Commit: runningSHA}, newFakeClock().seams())
	if !errors.Is(err, updater.ErrRegistry) {
		t.Fatalf("error = %v, want ErrRegistry", err)
	}
	if got := registry.tokenHits.Load(); got != 0 {
		t.Errorf("the unvalidated repository still reached the registry (%d token requests)", got)
	}
}

func TestResolve_StopsWhenTheRequestIsCancelled(t *testing.T) {
	registry := newRegistryStub(t, olderDigest)
	publish := newPublishStub(t, publishRun(newerSHA, "in_progress"))
	clock := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	clock.onSleep = func(time.Duration) { cancel() }

	_, err := updater.Resolve(ctx, fetcher(), newSource(registry, publish),
		updater.Running{Commit: runningSHA, Digest: olderDigest}, clock.seams())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestManifestDigest_RequiresExactlyOneValidHeader(t *testing.T) {
	valid := http.Header{"Docker-Content-Digest": []string{newestDigest.String()}}
	if got, err := updater.ManifestDigest(valid); err != nil || got != newestDigest {
		t.Fatalf("ManifestDigest(valid) = %q, %v", got, err)
	}

	tests := []struct {
		name   string
		header http.Header
	}{
		{"nil", nil},
		{"empty", http.Header{}},
		{"empty value", http.Header{"Docker-Content-Digest": []string{""}}},
		{"two values", http.Header{"Docker-Content-Digest": []string{newestDigest.String(), newerSHA}}},
		{"uppercase", http.Header{"Docker-Content-Digest": []string{"sha256:" + strings.Repeat("A", 64)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := updater.ManifestDigest(test.header); !errors.Is(err, wire.ErrInvalidDigest) {
				t.Fatalf("error = %v, want ErrInvalidDigest", err)
			}
		})
	}
}

func TestRequest_WritesOnlyTheDigest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "update-signal")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := updater.Request(dir, newestDigest); err != nil {
		t.Fatalf("Request: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "requested"))
	if err != nil {
		t.Fatalf("read the signal: %v", err)
	}
	if got := string(data); got != newestDigest.String() {
		t.Errorf("signal = %q, want exactly the digest", got)
	}
	info, err := os.Stat(filepath.Join(dir, "requested"))
	if err != nil {
		t.Fatalf("stat the signal: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("signal mode = %o, want 600", mode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the signal", len(entries))
	}
}

func TestRequest_RejectsAnInvalidDigestAndAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := updater.Request(dir, wire.Digest("sha256:short")); !errors.Is(err, wire.ErrInvalidDigest) {
		t.Fatalf("invalid digest error = %v, want ErrInvalidDigest", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "requested")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a rejected digest still wrote the signal: %v", err)
	}

	if err := updater.Request(filepath.Join(dir, "missing"), newestDigest); err == nil {
		t.Error("Request wrote into a directory that does not exist")
	}
}

// FuzzManifestHeader treats the registry's response header as untrusted: the value is either the
// typed digest error or a digest that passes the same validator the update path uses.
func FuzzManifestHeader(f *testing.F) {
	f.Add(newestDigest.String())
	f.Add("")
	f.Add("sha256:" + strings.Repeat("A", 64))
	f.Add("sha256:" + strings.Repeat("a", 63))
	f.Add("sha512:" + strings.Repeat("a", 64))
	f.Add("latest")
	f.Add(newestDigest.String() + "\n")
	f.Fuzz(func(t *testing.T, value string) {
		header := http.Header{"Docker-Content-Digest": []string{value}}
		digest, err := updater.ManifestDigest(header)
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Fatalf("ManifestDigest(%q) error = %v, want ErrInvalidDigest", value, err)
			}
			return
		}
		if !digest.IsValid() {
			t.Fatalf("ManifestDigest(%q) accepted a value its own validator rejects", value)
		}
	})
}

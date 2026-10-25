package probe_test

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// TestScheduler_StageDone is the stage's acceptance test: one scheduler run over the portability
// fixture's probe shapes, pointed at httptest servers, covering a service that is up, one that is
// down, one that never answers, one that flaps, and a host_unit service whose snapshot is stale.
func TestScheduler_StageDone(t *testing.T) {
	cfg, _, err := config.Load(filepath.Join("..", "..", "testdata", "portable", "config.yaml"))
	if err != nil {
		t.Fatalf("load the portability fixture: %v", err)
	}
	// The fixture's own five seconds would make the slow service a five second test.
	cfg.Poll.Timeout = config.Duration(500 * time.Millisecond)

	up := newTestService(t, http.StatusOK)
	up.respond(http.StatusOK, `{"version":"r1"}`)
	films := newTestService(t, http.StatusOK)
	films.respond(http.StatusOK, `{"status":"ok"}`)
	down := newTestService(t, http.StatusInternalServerError)
	slow := newTestService(t, -1)
	flapping := newTestService(t, http.StatusOK)
	flapping.respond(http.StatusOK, `{"version":"r1"}`)

	// Every service keeps the fixture's shapes; only the URLs move to this test's servers. The three
	// added services are clones of the fixture's http_status service, not a new shape.
	var httpShape config.Service
	services := make([]config.Service, 0, len(cfg.Services)+3)
	for index := range cfg.Services {
		service := cfg.Services[index]
		switch service.ID {
		case "gitea":
			httpShape = service
			service = pointAt(service, up.url)
		case "jellyfin":
			service = pointAt(service, films.url)
		}
		services = append(services, service)
	}
	services = append(services,
		renamed(pointAt(httpShape, down.url), "gone"),
		renamed(pointAt(httpShape, slow.url), "slow"),
		renamed(pointAt(httpShape, flapping.url), "flapping"),
	)
	cfg.Services = services

	stale := wire.HostSnapshot{
		At:    polledAt.Add(-10 * time.Minute),
		Host:  wire.HostInfo{UptimeS: 90_000, Load1: 0.4},
		Units: map[string]wire.UnitState{"backup.service": {Active: true, Since: polledAt.Add(-time.Hour)}},
	}
	env := probe.Env{
		Fetch: newFetcher(),
		Host:  func() (wire.HostSnapshot, bool) { return stale, true },
	}

	store := &probe.Store{}
	clock := newFakeClock(polledAt)
	scheduler := probe.NewScheduler(env, store, clock.seams(), quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	scheduler.Start(ctx, cfg)

	eventually(t, "every poller to park", func() bool { return clock.waiting() >= len(cfg.Services) })
	clock.Advance(probe.Stagger(len(cfg.Services) - 1))
	eventually(t, "every service to be polled", func() bool { return len(store.Snapshot()) == len(cfg.Services) })

	snapshot := store.Snapshot()
	tests := []struct {
		name string
		id   string
		want probe.Status
		code string
	}{
		{
			name: "an http service answering what the config expects",
			id:   "gitea",
			want: probe.StatusOK,
		},
		{
			name: "an http_json service reporting ok",
			id:   "jellyfin",
			want: probe.StatusOK,
		},
		{
			name: "a unit probe whose snapshot is stale is never ok",
			id:   "backup",
			want: probe.StatusUnknown,
			code: probe.CodeHostStale,
		},
		{
			name: "a host_field probe whose snapshot is stale is never ok",
			id:   "metrics",
			want: probe.StatusUnknown,
			code: probe.CodeHostStale,
		},
		{
			name: "a service answering an unexpected status",
			id:   "gone",
			want: probe.StatusDown,
			code: probe.CodeBadStatus,
		},
		{
			name: "a service that never answers",
			id:   "slow",
			want: probe.StatusDown,
			code: probe.CodeTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, polled := snapshot[test.id]
			if !polled {
				t.Fatalf("%s was never polled", test.id)
			}
			if got := probe.Derive(state, true); got != test.want {
				t.Fatalf("status = %q, want %q (health %+v)", got, test.want, state.Health)
			}
			if test.code != "" && state.Health.Code != test.code {
				t.Fatalf("reason code = %q, want %q", state.Health.Code, test.code)
			}
		})
	}

	if state := snapshot["backup"]; !state.Health.Stale {
		t.Fatalf("a stale snapshot did not mark the health stale: %+v", state.Health)
	}
	if vitals := snapshot["gitea"].Vitals; len(vitals) != 1 || vitals[0].Value != "r1" || vitals[0].Failing {
		t.Fatalf("gitea's vital is %+v, want the value r1 from a healthy pass", vitals)
	}
	// The fixture puts the health probe and the vital on the same host, so the per-cycle memo means
	// one request, not two.
	if got := up.hits.Load(); got != 1 {
		t.Fatalf("the up service was asked %d times for one cycle, want 1", got)
	}

	// The flapping service answers well, then badly.
	flapping.respond(http.StatusInternalServerError, "")
	if !scheduler.Refresh(context.Background()) {
		t.Fatal("the refresh before the flap was refused")
	}
	eventually(t, "the flapping service to go down", func() bool {
		return probe.Derive(store.Snapshot()["flapping"], true) == probe.StatusDown
	})
	if got := store.Snapshot()["flapping"].Health.Code; got != probe.CodeBadStatus {
		t.Fatalf("the flapping service failed with %q, want %q", got, probe.CodeBadStatus)
	}
}

// pointAt rewrites every probe URL of a service to one test server, leaving the probe shapes exactly
// as the fixture wrote them. The vitals slice is cloned: the clone must not share it with the
// service it came from.
func pointAt(service config.Service, url string) config.Service {
	service.URL = url
	service.Probes.Health.URL = url
	service.Probes.Vitals = slices.Clone(service.Probes.Vitals)
	for index := range service.Probes.Vitals {
		service.Probes.Vitals[index].URL = url
	}
	return service
}

// renamed gives a cloned service its own id.
func renamed(service config.Service, id string) config.Service {
	service.ID = id
	service.Name = id
	return service
}

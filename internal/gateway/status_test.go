package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/gateway"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// updateGoldens regenerates the pinned contract files. It is never set by scripts/check.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files in testdata/api")

// contractConfig is a small config with two sections in use, one empty, a disabled service, an image
// icon and a control block: the shape the contract has to express.
const contractConfig = `
listen: 127.0.0.1:8891
server:
  allowed_hosts: [hub.example.org]
sections:
  - {id: first, title: First}
  - {id: empty, title: Empty}
  - {id: second, title: Second}
poll:
  interval: 15s
  idle_interval: 2m
  idle_after: 10m
  timeout: 4s
  refresh_min: 5s
host_status:
  file: /status/host.json
  max_age: 90s
search:
  buttons:
    - label: Search
      url_template: https://search.example.org/?q={q}
services:
  - {id: alpha, name: Alpha, section: first, icon: shield, description: first one,
     url: https://alpha.example.org, probes: {health: {kind: self}},
     control: {target: alpha, actions: [start, restart]}}
  - {id: beta, name: Beta, section: second, icon: {image: icons/beta.png},
     url: https://beta.example.org, disabled: true, probes: {health: {kind: self}}}
  - {id: gamma, name: Gamma, section: second, icon: mail,
     url: https://gamma.example.org, probes: {health: {kind: self}}}
`

func parseGatewayConfig(t *testing.T, data string) *config.Config {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse the config: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the config should not warn: %v", warnings)
	}
	return parsed
}

// healthy is one poll of a service that answered and has nothing to report.
func healthy() probe.State {
	return probe.State{Health: probe.Health{Status: probe.StatusOK}, CheckedAt: testNow.Add(-time.Second)}
}

func TestBuildStatus_KeepsConfigOrderAndOmitsAnEmptySection(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	status := gateway.BuildStatus(cfg, nil, nil, testNow, gateway.StatusInputs{})

	if len(status.Sections) != 2 {
		t.Fatalf("%d sections, want 2 (the empty one is omitted)", len(status.Sections))
	}
	if status.Sections[0].ID != "first" || status.Sections[1].ID != "second" {
		t.Fatalf("sections = %q and %q, want config order first then second", status.Sections[0].ID, status.Sections[1].ID)
	}
	if got := []string{status.Sections[1].Services[0].ID, status.Sections[1].Services[1].ID}; got[0] != "beta" || got[1] != "gamma" {
		t.Fatalf("second section services = %v, want config order [beta gamma]", got)
	}
}

func TestBuildStatus_CountsUpAndExcludesDisabledFromTheTotal(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	states := map[string]probe.State{
		"alpha": healthy(),
		"beta":  healthy(),
		"gamma": {Health: probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout}, CheckedAt: testNow},
	}

	status := gateway.BuildStatus(cfg, states, nil, testNow, gateway.StatusInputs{})

	// beta is disabled: it is neither up nor part of the total.
	if status.Summary.Up != 1 || status.Summary.Total != 2 {
		t.Fatalf("summary = %+v, want up 1 of 2", status.Summary)
	}
}

func TestBuildStatus_ExplainsAStatusWithOneSentence(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	tests := []struct {
		name   string
		state  probe.State
		status probe.Status
		reason string
	}{
		{
			name:   "healthy says nothing",
			state:  healthy(),
			status: probe.StatusOK,
			reason: "",
		},
		{
			name:   "down explains its health code",
			state:  probe.State{Health: probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout}, CheckedAt: testNow},
			status: probe.StatusDown,
			reason: probe.Reason(probe.CodeTimeout),
		},
		{
			name:   "unknown explains its health code",
			state:  probe.State{Health: probe.Health{Status: probe.StatusUnknown, Code: probe.CodeEnvMissing}, CheckedAt: testNow},
			status: probe.StatusUnknown,
			reason: probe.Reason(probe.CodeEnvMissing),
		},
		{
			name: "degraded explains the vital that failed",
			state: probe.State{
				Health:    probe.Health{Status: probe.StatusOK},
				CheckedAt: testNow,
				Vitals: []probe.VitalState{
					{Label: "needs you", Value: "4", At: testNow},
					{Label: "blocked", Value: "30", At: testNow.Add(-time.Minute), Failing: true, Code: probe.CodeMissingField},
				},
			},
			status: probe.StatusDegraded,
			reason: probe.Reason(probe.CodeMissingField),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			states := map[string]probe.State{"alpha": test.state}
			status := gateway.BuildStatus(cfg, states, nil, testNow, gateway.StatusInputs{})
			tile := status.Sections[0].Services[0]
			if tile.Status != test.status {
				t.Fatalf("status = %q, want %q", tile.Status, test.status)
			}
			if tile.Reason != test.reason {
				t.Fatalf("reason = %q, want %q", tile.Reason, test.reason)
			}
		})
	}
}

func TestBuildStatus_ServesTheIconAndControlShape(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	status := gateway.BuildStatus(cfg, nil, nil, testNow, gateway.StatusInputs{})

	first := status.Sections[0].Services[0]
	if first.Icon.Lucide != "shield" || first.Icon.Image != "" {
		t.Fatalf("icon = %+v, want the lucide name only", first.Icon)
	}
	if first.Control == nil {
		t.Fatal("alpha has a control block but the tile says null")
	}
	if first.Control.Target != "alpha" || len(first.Control.Actions) != 2 || first.Control.Actions[1] != "restart" {
		t.Fatalf("control = %+v, want alpha with its two actions in order", first.Control)
	}

	beta := status.Sections[1].Services[0]
	if beta.Icon.Image != "/icons/beta.png" || beta.Icon.Lucide != "" {
		t.Fatalf("icon = %+v, want the served image path", beta.Icon)
	}
	if beta.Status != probe.StatusDisabled {
		t.Fatalf("status = %q, want disabled for a service the config turns off", beta.Status)
	}
	if beta.Control != nil {
		t.Fatalf("control = %+v, want null on a read-only tile", beta.Control)
	}
	if len(beta.Vitals) != 0 {
		t.Fatalf("vitals = %+v, want an empty list", beta.Vitals)
	}
}

func TestBuildStatus_ReportsTheHostSnapshotAge(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)

	fresh := wire.HostSnapshot{At: testNow.Add(-20 * time.Second)}
	status := gateway.BuildStatus(cfg, nil, &fresh, testNow, gateway.StatusInputs{})
	if status.Host == nil || status.Host.AgeS != 20 || status.Host.Stale {
		t.Fatalf("host = %+v, want 20s old and fresh", status.Host)
	}

	old := wire.HostSnapshot{At: testNow.Add(-10 * time.Minute)}
	status = gateway.BuildStatus(cfg, nil, &old, testNow, gateway.StatusInputs{})
	if status.Host == nil || status.Host.AgeS != 600 || !status.Host.Stale {
		t.Fatalf("host = %+v, want 600s old and stale", status.Host)
	}

	status = gateway.BuildStatus(cfg, nil, nil, testNow, gateway.StatusInputs{})
	if status.Host != nil {
		t.Fatalf("host = %+v, want null when nothing has written a snapshot", status.Host)
	}

	withoutHostProbe := parseGatewayConfig(t, strings.Replace(contractConfig, "host_status:\n  file: /status/host.json\n  max_age: 90s\n", "", 1))
	status = gateway.BuildStatus(withoutHostProbe, nil, &fresh, testNow, gateway.StatusInputs{})
	if status.Host != nil {
		t.Fatalf("host = %+v, want null when host_status is not configured", status.Host)
	}
}

func TestBuildStatus_CarriesTheLastLoadStatus(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	loaded := testNow.Add(-time.Hour)

	good := gateway.BuildStatus(cfg, nil, nil, testNow, gateway.StatusInputs{Load: gateway.LoadStatus{LoadedAt: loaded}})
	if !good.Config.OK || good.Config.Error != "" || !good.Config.LoadedAt.Equal(loaded) {
		t.Fatalf("config = %+v, want an accepted load at %v", good.Config, loaded)
	}

	rejected := gateway.BuildStatus(cfg, nil, nil, testNow, gateway.StatusInputs{
		Load: gateway.LoadStatus{LoadedAt: loaded, Error: "services[1].probes.health: probe_kind_unknown"},
	})
	if rejected.Config.OK || rejected.Config.Error == "" {
		t.Fatalf("config = %+v, want a rejected reload with its reason", rejected.Config)
	}
	// The old config keeps serving, so its tiles are still there.
	if len(rejected.Sections) != 2 {
		t.Fatalf("%d sections after a rejected reload, want the old config's 2", len(rejected.Sections))
	}
}

func TestBuildStatus_AnswersWithoutAConfig(t *testing.T) {
	status := gateway.BuildStatus(nil, nil, nil, testNow, gateway.StatusInputs{
		Load: gateway.LoadStatus{Error: "config: decode"},
	})
	if status.Config.OK || status.Config.Error != "config: decode" {
		t.Fatalf("config = %+v, want the rejected load", status.Config)
	}
	if len(status.Sections) != 0 || status.Summary.Total != 0 {
		t.Fatalf("sections = %+v, want none", status.Sections)
	}
	if status.GeneratedAt.IsZero() {
		t.Fatal("the response has no generated_at")
	}
}

func TestBuildStatus_ClampsAFutureTimestampToZeroAge(t *testing.T) {
	cfg := parseGatewayConfig(t, contractConfig)
	states := map[string]probe.State{"alpha": {
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: testNow.Add(time.Minute),
		Vitals:    []probe.VitalState{{Label: "x", Value: "1", At: testNow.Add(time.Minute)}},
	}}

	status := gateway.BuildStatus(cfg, states, nil, testNow, gateway.StatusInputs{})
	vital := status.Sections[0].Services[0].Vitals[0]
	if vital.AgeS != 0 {
		t.Fatalf("a vital from the future has age_s %d, want 0", vital.AgeS)
	}
}

// The portable fixture is the contract's reference config: everything here is a service Meridian has
// never heard of (decision #42), so a golden file that mentions one of ours would fail review.
func fixtureConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := config.Load(filepath.Join("..", "..", "testdata", "portable", "config.yaml"))
	if err != nil {
		t.Fatalf("load the portability fixture: %v", err)
	}
	return cfg
}

// goldenCase is one pinned state of the contract.
type goldenCase struct {
	cfg    *config.Config
	states map[string]probe.State
	host   *wire.HostSnapshot
	in     gateway.StatusInputs
}

func goldenStatuses(t *testing.T) map[string]goldenCase {
	t.Helper()
	base := fixtureConfig(t)
	fresh := &wire.HostSnapshot{
		At:        testNow.Add(-20 * time.Second),
		Host:      wire.HostInfo{UptimeS: 90_000, Load1: 0.42},
		Units:     map[string]wire.UnitState{"backup.service": {Active: true, Since: testNow.Add(-time.Hour)}},
		Tailscale: &wire.TailscaleState{Online: true, PeersOnline: 3, PeersTotal: 3},
	}
	loaded := gateway.LoadStatus{LoadedAt: testNow.Add(-time.Hour)}

	okStates := func() map[string]probe.State {
		return map[string]probe.State{
			"gitea": {
				Health:    probe.Health{Status: probe.StatusOK},
				CheckedAt: testNow.Add(-2 * time.Second),
				Vitals: []probe.VitalState{
					{Label: "version", Value: "1.22.0", At: testNow.Add(-2 * time.Second)},
				},
			},
			"jellyfin": {Health: probe.Health{Status: probe.StatusOK}, CheckedAt: testNow.Add(-3 * time.Second)},
			"backup":   {Health: probe.Health{Status: probe.StatusOK}, CheckedAt: testNow.Add(-4 * time.Second)},
			"metrics": {
				Health:    probe.Health{Status: probe.StatusOK},
				CheckedAt: testNow.Add(-5 * time.Second),
				Vitals: []probe.VitalState{
					{Label: "peers", Value: "3 online", At: testNow.Add(-5 * time.Second)},
				},
			},
		}
	}

	cases := map[string]goldenCase{
		"all-ok": {cfg: base, states: okStates(), host: fresh, in: gateway.StatusInputs{Load: loaded}},
	}

	oneDown := okStates()
	oneDown["jellyfin"] = probe.State{
		Health:    probe.Health{Status: probe.StatusDown, Code: probe.CodeBadJSON},
		CheckedAt: testNow.Add(-3 * time.Second),
	}
	cases["one-down"] = goldenCase{cfg: base, states: oneDown, host: fresh, in: gateway.StatusInputs{Load: loaded}}

	degraded := okStates()
	degraded["gitea"] = probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: testNow.Add(-2 * time.Second),
		Vitals: []probe.VitalState{
			{Label: "version", Value: "1.22.0", At: testNow.Add(-9 * time.Minute), Failing: true, Code: probe.CodeTimeout},
		},
	}
	cases["degraded-stale-vital"] = goldenCase{cfg: base, states: degraded, host: fresh, in: gateway.StatusInputs{Load: loaded}}

	unknown := okStates()
	delete(unknown, "backup")
	cases["unknown-before-first-poll"] = goldenCase{cfg: base, states: unknown, host: fresh, in: gateway.StatusInputs{Load: loaded}}

	disabledCfg := fixtureConfig(t)
	for index := range disabledCfg.Services {
		if disabledCfg.Services[index].ID == "jellyfin" {
			disabledCfg.Services[index].Disabled = true
		}
	}
	cases["disabled-service"] = goldenCase{cfg: disabledCfg, states: okStates(), host: fresh, in: gateway.StatusInputs{Load: loaded}}

	emptySection := fixtureConfig(t)
	services := emptySection.Services[:0]
	for index := range emptySection.Services {
		if emptySection.Services[index].Section != "media" {
			services = append(services, emptySection.Services[index])
		}
	}
	emptySection.Services = services
	devOnly := map[string]probe.State{}
	for id, state := range okStates() {
		if id != "jellyfin" && id != "metrics" {
			devOnly[id] = state
		}
	}
	cases["empty-section-omitted"] = goldenCase{cfg: emptySection, states: devOnly, host: fresh, in: gateway.StatusInputs{Load: loaded}}

	staleHost := *fresh
	staleHost.At = testNow.Add(-10 * time.Minute)
	cases["host-stale"] = goldenCase{cfg: base, states: okStates(), host: &staleHost, in: gateway.StatusInputs{Load: loaded}}

	cases["config-rejected"] = goldenCase{
		cfg:    base,
		states: okStates(),
		host:   fresh,
		in: gateway.StatusInputs{
			Load: gateway.LoadStatus{LoadedAt: loaded.LoadedAt, Error: "services[2].probes.health: probe_kind_unknown"},
		},
	}
	return cases
}

func TestBuildStatus_GoldenFiles(t *testing.T) {
	for name, golden := range goldenStatuses(t) {
		t.Run(name, func(t *testing.T) {
			if golden.in.ControlReason == "" {
				golden.in.ControlReason = "control plane not installed"
			}
			response := gateway.BuildStatus(golden.cfg, golden.states, golden.host, testNow, golden.in)
			encoded, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				t.Fatalf("marshal the response: %v", err)
			}
			encoded = append(encoded, '\n')

			path := filepath.Join("..", "..", "testdata", "api", fmt.Sprintf("status.%s.golden.json", name))
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create the golden directory: %v", err)
				}
				if err := os.WriteFile(path, encoded, 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run go test ./internal/gateway -update to create it)", path, err)
			}
			if !bytes.Equal(want, encoded) {
				t.Fatalf("%s does not match the contract\n got:\n%s\nwant:\n%s", path, encoded, want)
			}
		})
	}
}

// stepClock is a probe.Clock the test advances by hand. Its timers never fire, so a probe pass only
// happens when something nudges the scheduler, which makes every count in these tests deterministic.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *stepClock) seams() probe.Clock {
	return probe.Clock{Now: clock.Now, After: clock.After}
}

func (clock *stepClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *stepClock) After(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

func (clock *stepClock) Advance(delay time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delay)
}

// liveGateway is a gateway over a real store and scheduler, with a clock the test controls.
func liveGateway(t *testing.T) (*testGateway, *probe.Store, *stepClock) {
	t.Helper()
	hub := newTestGateway(t)
	clock := &stepClock{now: testNow}
	store := &probe.Store{}
	scheduler := probe.NewScheduler(probe.Env{}, store, clock.seams(), log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	scheduler.Start(ctx, hub.cfg)

	hub.deps.Store = store
	hub.deps.Scheduler = scheduler
	hub.deps.Now = clock.Now
	return hub, store, clock
}

func pollStatus(t *testing.T, hub *testGateway, query string) gateway.StatusResponse {
	t.Helper()
	response := hub.hostRequest(t, "hub.example.org", "/api/status"+query)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/status%s = %d, want 200 (body %q)", query, response.Code, response.Body.String())
	}
	var status gateway.StatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatalf("the status body is not JSON: %v", err)
	}
	return status
}

// eventually waits for something the scheduler's goroutine does, bounded by a real deadline.
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for %s", what)
}

func checkedAt(status gateway.StatusResponse) time.Time {
	return status.Sections[0].Services[0].CheckedAt
}

func TestStatus_AnswersAFreshLoadAndServesTheCacheWhenItIsFresh(t *testing.T) {
	hub, _, clock := liveGateway(t)

	// Nothing has been polled, so the first load waits for a pass instead of showing an empty tile.
	first := pollStatus(t, hub, "")
	if got := checkedAt(first); !got.Equal(testNow) {
		t.Fatalf("the first load checked at %v, want %v: it did not wait for a pass", got, testNow)
	}
	if first.Sections[0].Services[0].Status != probe.StatusOK {
		t.Fatalf("status = %q, want ok", first.Sections[0].Services[0].Status)
	}

	// Twenty seconds later the result is stale, so the load waits for another pass.
	clock.Advance(20 * time.Second)
	stale := pollStatus(t, hub, "")
	if got := checkedAt(stale); !got.Equal(testNow.Add(20 * time.Second)) {
		t.Fatalf("a stale load checked at %v, want %v", got, testNow.Add(20*time.Second))
	}

	// One second after that it is fresh, so the answer is the cache and generated_at says how old.
	clock.Advance(time.Second)
	fresh := pollStatus(t, hub, "")
	if got := checkedAt(fresh); !got.Equal(testNow.Add(20 * time.Second)) {
		t.Fatalf("a fresh load checked at %v, want the cached %v", got, testNow.Add(20*time.Second))
	}
	if !fresh.GeneratedAt.Equal(testNow.Add(21 * time.Second)) {
		t.Fatalf("generated_at = %v, want the clock's now", fresh.GeneratedAt)
	}
}

func TestStatus_StartsAPassForAManualRefresh(t *testing.T) {
	hub, store, clock := liveGateway(t)
	if got := checkedAt(pollStatus(t, hub, "")); !got.Equal(testNow) {
		t.Fatalf("the first load checked at %v, want %v", got, testNow)
	}

	clock.Advance(time.Second)
	pollStatus(t, hub, "?fresh=1")

	// The response may still be the cache: a manual refresh starts the pass and does not hold the
	// page. The new result lands in the store.
	eventually(t, "the refreshed pass", func() bool {
		return store.Snapshot()["one"].CheckedAt.Equal(testNow.Add(time.Second))
	})
}

func TestStatus_CarriesTheControlBlockAndTheLoadStatus(t *testing.T) {
	hub := newTestGateway(t)
	hub.mountControl(t)
	hub.load = gateway.LoadStatus{LoadedAt: testNow, Error: "services[0].probes.health: probe_kind_unknown"}

	status := pollStatus(t, hub, "")
	if !status.Control.Available {
		t.Fatalf("control = %+v, want available", status.Control)
	}
	if status.Config.OK || status.Config.Error == "" {
		t.Fatalf("config = %+v, want the rejected reload", status.Config)
	}
}

// FuzzBuildStatus feeds arbitrary probe results through the contract: it must never panic, always
// marshal, and always answer with the closed status set in config order.
func FuzzBuildStatus(f *testing.F) {
	f.Add([]byte{0x00}, []byte{0x00}, int64(0))
	f.Add([]byte{0x01, 0x02, 0x03, 0x04}, []byte{0x05, 0x06}, int64(-1000))
	f.Add([]byte{0xff, 0xff, 0xff}, []byte{0xff}, int64(1<<40))

	cfg, _, err := config.Parse([]byte(contractConfig))
	if err != nil {
		f.Fatalf("parse the contract config: %v", err)
	}
	closedSet := []probe.Status{
		probe.StatusOK, probe.StatusDegraded, probe.StatusDown, probe.StatusUnknown, probe.StatusDisabled,
	}
	statuses := []probe.Status{probe.StatusOK, probe.StatusDown, probe.StatusUnknown, "", "bogus"}
	codes := []string{"", probe.CodeTimeout, probe.CodeBadStatus, probe.CodeMissingField, "not a code"}

	f.Fuzz(func(t *testing.T, ids, kinds []byte, offset int64) {
		states := map[string]probe.State{}
		for index, octet := range ids {
			service := cfg.Services[index%len(cfg.Services)]
			kindsAt := byte(0)
			if len(kinds) > 0 {
				kindsAt = kinds[index%len(kinds)]
			}
			states[service.ID] = probe.State{
				Health:    probe.Health{Status: statuses[int(octet)%len(statuses)], Code: codes[int(kindsAt)%len(codes)]},
				CheckedAt: testNow.Add(time.Duration(offset)),
				Vitals: []probe.VitalState{{
					Label:   string(rune('a' + index%26)),
					Value:   string(kinds),
					At:      testNow.Add(-time.Duration(octet) * time.Second),
					Failing: octet%3 == 0,
					Code:    codes[int(kindsAt)%len(codes)],
				}},
			}
		}

		response := gateway.BuildStatus(cfg, states, nil, testNow, gateway.StatusInputs{
			Load: gateway.LoadStatus{LoadedAt: testNow, Error: string(kinds)},
		})
		if _, err := json.Marshal(response); err != nil {
			t.Fatalf("the response does not marshal: %v", err)
		}
		for _, section := range response.Sections {
			for index := range section.Services {
				if status := section.Services[index].Status; !slices.Contains(closedSet, status) {
					t.Fatalf("status %q is outside the closed set", status)
				}
			}
		}
		// Section order is config order, with empties omitted.
		order := make([]string, 0, len(response.Sections))
		for _, section := range response.Sections {
			order = append(order, section.ID)
		}
		if !slices.Equal(order, []string{"first", "second"}) {
			t.Fatalf("sections = %v, want config order [first second]", order)
		}
	})
}

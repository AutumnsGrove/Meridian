package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

const exampleSnapshot = `{
  "at": "2026-10-23T18:04:11Z",
  "host": {"uptime_s": 1412033, "load1": 0.31},
  "units": {
    "AdGuardHome.service": {"active": true, "since": "2026-10-11T03:02:00Z"},
    "unbound.service": {"active": false, "since": "2026-10-11T03:02:01Z"}
  },
  "tailscale": {"online": true, "exit_node": true, "peers_online": 2, "peers_total": 3}
}`

func TestParseHostSnapshot_ReadsTheDocumentedShape(t *testing.T) {
	snapshot, err := wire.ParseHostSnapshot([]byte(exampleSnapshot))
	if err != nil {
		t.Fatalf("ParseHostSnapshot: %v", err)
	}

	wantAt := time.Date(2026, 10, 23, 18, 4, 11, 0, time.UTC)
	if !snapshot.At.Equal(wantAt) {
		t.Errorf("At = %v, want %v", snapshot.At, wantAt)
	}
	if snapshot.Host.UptimeS != 1412033 || snapshot.Host.Load1 != 0.31 {
		t.Errorf("Host = %+v, want uptime 1412033 and load 0.31", snapshot.Host)
	}
	adguard, ok := snapshot.Units["AdGuardHome.service"]
	if !ok || !adguard.Active {
		t.Errorf("units[AdGuardHome.service] = %+v, %v, want active", adguard, ok)
	}
	if unbound := snapshot.Units["unbound.service"]; unbound.Active {
		t.Errorf("units[unbound.service] = %+v, want inactive", unbound)
	}
	if snapshot.Tailscale == nil {
		t.Fatal("Tailscale = nil, want the block")
	}
	if want := (wire.TailscaleState{Online: true, ExitNode: true, PeersOnline: 2, PeersTotal: 3}); *snapshot.Tailscale != want {
		t.Errorf("Tailscale = %+v, want %+v", *snapshot.Tailscale, want)
	}
}

func TestParseHostSnapshot_RejectsAnythingItDoesNotUnderstand(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{"unknown top-level field", `{"at":"2026-10-23T18:04:11Z","extra":1}`},
		{"unknown nested field", `{"at":"2026-10-23T18:04:11Z","host":{"uptime_s":1,"load1":0.1,"extra":true}}`},
		{"unknown unit field", `{"at":"2026-10-23T18:04:11Z","units":{"a.service":{"active":true,"extra":1}}}`},
		{"unknown tailscale field", `{"at":"2026-10-23T18:04:11Z","tailscale":{"online":true,"extra":1}}`},
		{"missing at", `{"host":{"uptime_s":1,"load1":0.1}}`},
		{"null at", `{"at":null}`},
		{"bad timestamp", `{"at":"yesterday"}`},
		{"trailing value", `{"at":"2026-10-23T18:04:11Z"} {"at":"2026-10-23T18:04:11Z"}`},
		{"not an object", `[]`},
		{"empty", ``},
		{"too large", `{"at":"2026-10-23T18:04:11Z","units":{"` + strings.Repeat("a", 70<<10) + `":{}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := wire.ParseHostSnapshot([]byte(test.document))
			if err == nil {
				t.Fatal("ParseHostSnapshot returned no error, want one")
			}
			if !errors.Is(err, wire.ErrInvalidSnapshot) {
				t.Errorf("error = %v, want it to wrap ErrInvalidSnapshot", err)
			}
		})
	}
}

func TestParseHostSnapshot_RoundTripsItsOwnOutput(t *testing.T) {
	original, err := wire.ParseHostSnapshot([]byte(exampleSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wire.ParseHostSnapshot(encoded)
	if err != nil {
		t.Fatalf("ParseHostSnapshot(own output): %v", err)
	}
	if !decoded.At.Equal(original.At) || decoded.Host != original.Host {
		t.Errorf("round trip changed the snapshot: %+v vs %+v", decoded, original)
	}
}

func TestHostSnapshot_AgeAndFreshness(t *testing.T) {
	at := time.Date(2026, 10, 23, 18, 4, 11, 0, time.UTC)
	snapshot := wire.HostSnapshot{At: at}

	tests := []struct {
		name    string
		now     time.Time
		maxAge  time.Duration
		wantAge time.Duration
		fresh   bool
	}{
		{"same instant", at, 90 * time.Second, 0, true},
		{"inside the window", at.Add(89 * time.Second), 90 * time.Second, 89 * time.Second, true},
		{"exactly at the boundary", at.Add(90 * time.Second), 90 * time.Second, 90 * time.Second, true},
		{"one nanosecond past it", at.Add(90*time.Second + 1), 90 * time.Second, 90*time.Second + 1, false},
		{"a future timestamp counts as fresh", at.Add(-5 * time.Second), 90 * time.Second, -5 * time.Second, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := snapshot.Age(test.now); got != test.wantAge {
				t.Errorf("Age = %v, want %v", got, test.wantAge)
			}
			if got := snapshot.IsFresh(test.now, test.maxAge); got != test.fresh {
				t.Errorf("IsFresh = %v, want %v", got, test.fresh)
			}
		})
	}
}

func TestHostSnapshot_FieldWalksTheJSONForm(t *testing.T) {
	snapshot, err := wire.ParseHostSnapshot([]byte(exampleSnapshot))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path   string
		want   any
		wantOK bool
	}{
		{"tailscale.online", true, true},
		{"tailscale.peers_online", json.Number("2"), true},
		{"tailscale.exit_node", true, true},
		{"host.load1", json.Number("0.31"), true},
		{"host.uptime_s", json.Number("1412033"), true},
		{"at", "2026-10-23T18:04:11Z", true},
		{"host.missing", nil, false},
		{"nope", nil, false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, ok := snapshot.Field(test.path)
			if ok != test.wantOK {
				t.Fatalf("Field(%q) ok = %v, want %v (value %v)", test.path, ok, test.wantOK, got)
			}
			if ok && !jsonEqual(got, test.want) {
				t.Errorf("Field(%q) = %v (%T), want %v (%T)", test.path, got, got, test.want, test.want)
			}
		})
	}
}

func jsonEqual(got, want any) bool {
	gotText, gotErr := json.Marshal(got)
	wantText, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && bytes.Equal(gotText, wantText)
}

func TestHostSnapshot_FieldOnAMissingTailscaleBlock(t *testing.T) {
	snapshot, err := wire.ParseHostSnapshot([]byte(`{"at":"2026-10-23T18:04:11Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Tailscale != nil {
		t.Errorf("Tailscale = %+v, want nil", snapshot.Tailscale)
	}
	if _, ok := snapshot.Field("tailscale.online"); ok {
		t.Error("Field(tailscale.online) ok = true, want false when the block is absent")
	}
}

func TestNewID_ProducesUUIDv7(t *testing.T) {
	at := time.UnixMilli(0x0123456789ab)
	id, err := wire.NewID(at, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if want := "01234567-89ab-7000-8000-000000000000"; id != want {
		t.Errorf("NewID = %q, want %q", id, want)
	}
	if _, err := wire.ParseID(id); err != nil {
		t.Errorf("ParseID(NewID) = %v, want no error", err)
	}
}

func TestNewID_SortsByTime(t *testing.T) {
	base := time.Date(2026, 10, 23, 18, 4, 11, 0, time.UTC)
	earlier, err := wire.NewID(base, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	later, err := wire.NewID(base.Add(time.Millisecond), bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if earlier >= later {
		t.Errorf("ids do not sort by time: %q >= %q", earlier, later)
	}
}

func TestNewID_ReportsAFailingRandomSource(t *testing.T) {
	if _, err := wire.NewID(time.Now(), bytes.NewReader(nil)); err == nil {
		t.Fatal("NewID with an empty random source = nil, want an error")
	}
}

func TestParseID_AcceptsOnlyCanonicalLowercaseUUIDs(t *testing.T) {
	valid := []string{
		"01234567-89ab-7000-8000-000000000000",
		"00000000-0000-4000-8000-000000000000",
		"ffffffff-ffff-4fff-bfff-ffffffffffff",
	}
	for _, id := range valid {
		if got, err := wire.ParseID(id); err != nil || got != id {
			t.Errorf("ParseID(%q) = %q, %v, want the same string", id, got, err)
		}
	}

	invalid := []string{
		"",
		"01234567-89ab-7000-8000-00000000000",
		"01234567-89ab-7000-8000-0000000000000",
		"01234567-89AB-7000-8000-000000000000",
		"01234567x89ab-7000-8000-000000000000",
		"01234567-89ab-3000-8000-000000000000",
		"01234567-89ab-5000-8000-000000000000",
		"01234567-89ab-7000-c000-000000000000",
		"01234567-89ab-7000-0000-000000000000",
		"01234567-89ab-7000-8000-00000000000g",
		"00000000-0000-0000-0000-000000000000",
		"01234567-89ab-7000-8000-000000000000\n",
		" 1234567-89ab-7000-8000-000000000000",
	}
	for _, id := range invalid {
		t.Run(id, func(t *testing.T) {
			if _, err := wire.ParseID(id); !errors.Is(err, wire.ErrInvalidID) {
				t.Errorf("ParseID(%q) error = %v, want ErrInvalidID", id, err)
			}
		})
	}
}

func FuzzParseHostSnapshot(f *testing.F) {
	f.Add([]byte(exampleSnapshot))
	f.Add([]byte(`{"at":"2026-10-23T18:04:11Z"}`))
	f.Add([]byte(`{"at":"2026-10-23T18:04:11Z","tailscale":null}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add(bytes.Repeat([]byte("a"), 1<<16))

	f.Fuzz(func(t *testing.T, data []byte) {
		snapshot, err := wire.ParseHostSnapshot(data)
		if err != nil {
			return
		}
		if snapshot.At.IsZero() {
			t.Fatal("a parsed snapshot has no timestamp")
		}
		if _, err := json.Marshal(snapshot); err != nil {
			t.Fatalf("a parsed snapshot does not marshal: %v", err)
		}
	})
}

func FuzzParseID(f *testing.F) {
	f.Add("01234567-89ab-7000-8000-000000000000")
	f.Add("00000000-0000-4000-8000-000000000000")
	f.Add("01234567-89AB-7000-8000-000000000000")
	f.Add("")
	f.Add("01234567-89ab-7000-8000-00000000000\n")

	f.Fuzz(func(t *testing.T, id string) {
		parsed, err := wire.ParseID(id)
		if err != nil {
			return
		}
		if parsed != id || len(parsed) != 36 {
			t.Fatalf("ParseID(%q) = %q, want the same 36-character string", id, parsed)
		}
		for i := 0; i < len(parsed); i++ {
			character := parsed[i]
			if i == 8 || i == 13 || i == 18 || i == 23 {
				if character != '-' {
					t.Fatalf("ParseID(%q) accepted a bad hyphen at %d", id, i)
				}
				continue
			}
			if !strings.ContainsRune("0123456789abcdef", rune(character)) {
				t.Fatalf("ParseID(%q) accepted %q", id, character)
			}
		}
		if version := parsed[14]; version != '4' && version != '7' {
			t.Fatalf("ParseID(%q) accepted version %q", id, version)
		}
		if variant := parsed[19]; variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
			t.Fatalf("ParseID(%q) accepted variant %q", id, variant)
		}
		if _, err := wire.ParseID(parsed); err != nil {
			t.Fatalf("ParseID is not idempotent on %q: %v", id, err)
		}
	})
}

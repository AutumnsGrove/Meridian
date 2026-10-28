package hostprobe_test

import (
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostprobe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

func TestParseTailscaleStatus_ReadsTheBoxAndThePeers(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   wire.TailscaleState
	}{
		{
			name: "online, an exit node candidate, two of three peers up",
			status: `{
			  "Version": "1.80.3",
			  "Self": {"ID": "n1", "HostName": "potato", "Online": true, "ExitNodeOption": true},
			  "Peer": {
			    "n2": {"Online": true, "ExitNodeOption": false},
			    "n3": {"Online": false, "ExitNodeOption": false},
			    "n4": {"Online": true, "ExitNodeOption": true}
			  },
			  "MagicDNSSuffix": "taild95077.ts.net"
			}`,
			want: wire.TailscaleState{Online: true, ExitNode: true, PeersOnline: 2, PeersTotal: 3},
		},
		{
			name:   "offline, with no peers key at all",
			status: `{"Self": {"Online": false, "ExitNodeOption": false}}`,
			want:   wire.TailscaleState{},
		},
		{
			name:   "offline with an empty peer map",
			status: `{"Self": {"Online": false, "ExitNodeOption": true}, "Peer": {}}`,
			want:   wire.TailscaleState{ExitNode: true},
		},
		{
			name: "a newer schema with keys this build has never seen",
			status: `{
			  "Version": "1.99.0",
			  "TUN": true,
			  "Self": {
			    "ID": "n1", "Online": true, "ExitNodeOption": false,
			    "Capabilities": ["https://tailscale.com/cap/example"],
			    "TailscaleSSHEnabled": true
			  },
			  "Peer": {
			    "n2": {"Online": true, "Addrs": ["100.64.0.2"], "CurAddr": "1.2.3.4:41641", "OS": "linux"}
			  },
			  "User": {"1": {"LoginName": "autumn@example.org"}},
			  "Health": []
			}`,
			want: wire.TailscaleState{Online: true, PeersOnline: 1, PeersTotal: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, ok := hostprobe.ParseTailscaleStatus([]byte(test.status))

			if !ok {
				t.Fatalf("ParseTailscaleStatus() = not ok, want the tailnet state")
			}
			if state != test.want {
				t.Errorf("state = %+v, want %+v", state, test.want)
			}
		})
	}
}

func TestParseTailscaleStatus_RefusesWhatIsNotAStatus(t *testing.T) {
	for _, status := range []string{"", "   ", "not json", "null", "[]", `"online"`, `{"Self": "online"}`} {
		t.Run(status, func(t *testing.T) {
			if state, ok := hostprobe.ParseTailscaleStatus([]byte(status)); ok {
				t.Fatalf("ParseTailscaleStatus() = %+v, ok, want not ok", state)
			}
		})
	}
}

// FuzzParseTailscaleStatus holds the parser to the shape of its own type: never a panic, and a state
// it accepts has no negative counts and never reports more online peers than it counted.
func FuzzParseTailscaleStatus(f *testing.F) {
	f.Add([]byte(`{"Self":{"Online":true,"ExitNodeOption":true},"Peer":{"a":{"Online":true}}}`))
	f.Add([]byte(`{"Self":{"Online":false}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`{"Peer":{"a":{"Online":true},"b":{"Online":false}}}`))
	f.Add([]byte(`{"Peer":[]}`))

	f.Fuzz(func(t *testing.T, status []byte) {
		state, ok := hostprobe.ParseTailscaleStatus(status)
		if !ok {
			return
		}
		// The counts are the only numbers in the type, and a peer cannot be counted twice.
		if state.PeersOnline < 0 || state.PeersTotal < 0 {
			t.Fatalf("ParseTailscaleStatus(%q) = %+v, want no negative counts", status, state)
		}
		if state.PeersOnline > state.PeersTotal {
			t.Fatalf("ParseTailscaleStatus(%q) = %+v, want online peers within the total", status, state)
		}
	})
}

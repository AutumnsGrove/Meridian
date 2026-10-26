package hostprobe

import (
	"bytes"
	"encoding/json"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// ParseTailscaleStatus reads the parts of `tailscale status --json` the snapshot carries. It is
// lenient on purpose: a newer tailscale adds fields, and an unknown key must not blank the tailnet
// tile. Only bytes that are not a JSON object, or that contradict the shape of the fields it needs,
// are refused.
//
// ExitNode is tailscale's ExitNodeOption, "this box may offer itself as an exit node", which is the
// question a dashboard can answer from one snapshot; whether the box is currently *using* one lives
// in ExitNodeStatus and is not something the page shows.
func ParseTailscaleStatus(status []byte) (wire.TailscaleState, bool) {
	trimmed := bytes.TrimSpace(status)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return wire.TailscaleState{}, false
	}

	var document struct {
		Self struct {
			Online         bool `json:"Online"`
			ExitNodeOption bool `json:"ExitNodeOption"`
		} `json:"Self"`
		Peer map[string]struct {
			Online bool `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(trimmed, &document); err != nil {
		return wire.TailscaleState{}, false
	}

	state := wire.TailscaleState{
		Online:     document.Self.Online,
		ExitNode:   document.Self.ExitNodeOption,
		PeersTotal: len(document.Peer),
	}
	for _, peer := range document.Peer {
		if peer.Online {
			state.PeersOnline++
		}
	}
	return state, true
}

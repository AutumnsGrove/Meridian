// Package wire holds the JSON contracts shared by both binaries and the browser. It is standard
// library only: the host binary that imports it is built without any third-party code.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/jsonpath"
)

// MaxHostSnapshotSize is the largest host.json the probe accepts; a real one is a few hundred bytes.
const MaxHostSnapshotSize = 64 << 10

// ErrInvalidSnapshot means the bytes were not a host snapshot this build understands.
var ErrInvalidSnapshot = errors.New("wire: invalid host snapshot")

// ErrInvalidID means the text was not a canonical lowercase UUID.
var ErrInvalidID = errors.New("wire: invalid id")

// HostSnapshot is what meridian-probe.timer writes to status/host.json: the only way systemd and
// tailnet state reach the container (decision #27).
type HostSnapshot struct {
	At        time.Time            `json:"at"`
	Host      HostInfo             `json:"host"`
	Units     map[string]UnitState `json:"units"`
	Tailscale *TailscaleState      `json:"tailscale,omitempty"`
}

// HostInfo is the box itself.
type HostInfo struct {
	UptimeS int64   `json:"uptime_s"`
	Load1   float64 `json:"load1"`
}

// UnitState is one systemd unit as the host probe saw it.
type UnitState struct {
	Active bool      `json:"active"`
	Since  time.Time `json:"since"`
}

// TailscaleState is the tailnet as the host probe saw it. The whole block is absent when Tailscale
// is not installed, which is different from "reported offline".
type TailscaleState struct {
	Online      bool `json:"online"`
	ExitNode    bool `json:"exit_node"`
	PeersOnline int  `json:"peers_online"`
	PeersTotal  int  `json:"peers_total"`
}

// ParseHostSnapshot decodes a snapshot strictly: unknown fields anywhere, trailing data, a missing
// timestamp and anything over MaxHostSnapshotSize are refused. Errors wrap ErrInvalidSnapshot.
func ParseHostSnapshot(data []byte) (HostSnapshot, error) {
	if len(data) > MaxHostSnapshotSize {
		return HostSnapshot{}, fmt.Errorf("%w: %d bytes exceeds %d", ErrInvalidSnapshot, len(data), MaxHostSnapshotSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot HostSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return HostSnapshot{}, fmt.Errorf("%w: %w", ErrInvalidSnapshot, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return HostSnapshot{}, fmt.Errorf("%w: trailing data after the snapshot", ErrInvalidSnapshot)
	}
	if snapshot.At.IsZero() {
		return HostSnapshot{}, fmt.Errorf("%w: no at timestamp", ErrInvalidSnapshot)
	}
	return snapshot, nil
}

// Age is how long ago the snapshot was taken. A clock-skewed snapshot from the future is negative.
func (snapshot HostSnapshot) Age(now time.Time) time.Duration {
	return now.Sub(snapshot.At)
}

// IsFresh reports whether the snapshot is at most maxAge old. A snapshot from the future is fresh:
// the host's clock is wrong, but the data is real.
func (snapshot HostSnapshot) IsFresh(now time.Time, maxAge time.Duration) bool {
	return snapshot.Age(now) <= maxAge
}

// Field reads a dotted path (tailscale.online, host.load1) out of the snapshot's JSON form, which is
// what a host_field probe compares. Unit names contain dots, so reach them through Units directly.
func (snapshot HostSnapshot) Field(path string) (any, bool) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, false
	}
	document, err := jsonpath.Decode(bytes.NewReader(data), MaxHostSnapshotSize)
	if err != nil {
		return nil, false
	}
	return jsonpath.Get(document, path)
}

// NewID returns a UUIDv7 for the given time, taking the random bits from randomness. UUIDv7 ids sort
// by creation time, which makes a directory of request files readable. Errors come from randomness.
func NewID(now time.Time, randomness io.Reader) (string, error) {
	var id [16]byte
	milliseconds := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		id[i] = byte(milliseconds)
		milliseconds >>= 8
	}
	if _, err := io.ReadFull(randomness, id[6:]); err != nil {
		return "", fmt.Errorf("wire: random bytes for an id: %w", err)
	}
	id[6] = id[6]&0x0f | 0x70 // version 7
	id[8] = id[8]&0x3f | 0x80 // RFC 4122 variant
	return formatID(id), nil
}

// ParseID accepts only canonical lowercase UUID text with a version 4 or 7 nibble and the RFC 4122
// variant, and returns it unchanged so a caller can stop treating the input as untrusted.
func ParseID(text string) (string, error) {
	if len(text) != 36 {
		return "", fmt.Errorf("%w: %q is not 36 characters", ErrInvalidID, text)
	}
	for i := 0; i < len(text); i++ {
		character := text[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if character != '-' {
				return "", fmt.Errorf("%w: %q has no hyphen at position %d", ErrInvalidID, text, i)
			}
			continue
		}
		if !isLowerHex(character) {
			return "", fmt.Errorf("%w: %q contains %q", ErrInvalidID, text, character)
		}
	}
	if version := text[14]; version != '4' && version != '7' {
		return "", fmt.Errorf("%w: %q is version %q, want 4 or 7", ErrInvalidID, text, version)
	}
	switch variant := text[19]; variant {
	case '8', '9', 'a', 'b':
	default:
		return "", fmt.Errorf("%w: %q has variant %q", ErrInvalidID, text, variant)
	}
	return text, nil
}

func isLowerHex(character byte) bool {
	return '0' <= character && character <= '9' || 'a' <= character && character <= 'f'
}

func formatID(id [16]byte) string {
	const hexDigits = "0123456789abcdef"
	var text [36]byte
	position := 0
	for index, octet := range id {
		if index == 4 || index == 6 || index == 8 || index == 10 {
			text[position] = '-'
			position++
		}
		text[position] = hexDigits[octet>>4]
		text[position+1] = hexDigits[octet&0x0f]
		position += 2
	}
	return string(text[:])
}

// MaxUnitNameSize is systemd's own limit on a unit name, minus the terminating NUL.
const MaxUnitNameSize = 255

// unitNameGrammar is what the host accepts before it hands a name to systemctl: the characters
// systemd allows in a unit name, one of the six unit types this project watches, and never a leading
// dash, so a name from a root-owned file can never turn into a command-line flag.
var unitNameGrammar = regexp.MustCompile(`^[A-Za-z0-9:_.@-]+\.(service|socket|timer|target|path)$`)

// IsUnitName reports whether text is a systemd unit name the host may hand to systemctl.
func IsUnitName(text string) bool {
	return len(text) <= MaxUnitNameSize && !strings.HasPrefix(text, "-") && unitNameGrammar.MatchString(text)
}

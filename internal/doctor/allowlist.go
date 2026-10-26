package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxAllowlistSize caps the allowlist doctor will read: the real file is a few kilobytes, and a
// hijacked path must not be able to feed a gigabyte of JSON to the doctor.
const MaxAllowlistSize = 64 << 10

// ErrInvalidAllowlist means the bytes were not an allowlist this build understands.
var ErrInvalidAllowlist = errors.New("doctor: invalid allowlist")

// Allowlist is the part of the host's /etc/meridian/targets.json doctor compares the config with:
// which targets exist and which actions each one permits (docs/CONTROL.md "Allowlist"). The executor
// owns the whole file; doctor reads names and actions and ignores the rest.
type Allowlist struct {
	Targets map[string]Target `json:"targets"`
}

// Target is one allowlisted target, as far as doctor cares: the actions it permits.
type Target struct {
	Actions []string `json:"actions"`
}

// ParseAllowlist decodes an allowlist. Unknown fields are ignored on purpose: the executor's file
// carries units, commands and criticality that doctor has no business validating, and a newer
// executor must not make an older doctor fail.
func ParseAllowlist(data []byte) (*Allowlist, error) {
	if len(data) > MaxAllowlistSize {
		return nil, fmt.Errorf("%w: %d bytes is larger than %d", ErrInvalidAllowlist, len(data), MaxAllowlistSize)
	}

	var allow Allowlist
	if err := json.Unmarshal(data, &allow); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAllowlist, err)
	}
	if allow.Targets == nil {
		return nil, fmt.Errorf("%w: no targets", ErrInvalidAllowlist)
	}
	for name, target := range allow.Targets {
		if name == "" {
			return nil, fmt.Errorf("%w: a target has no name", ErrInvalidAllowlist)
		}
		if len(target.Actions) == 0 {
			return nil, fmt.Errorf("%w: target %q has no actions", ErrInvalidAllowlist, name)
		}
	}
	return &allow, nil
}

// ReadAllowlist reads an allowlist from r, capped at MaxAllowlistSize.
func ReadAllowlist(r io.Reader) (*Allowlist, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxAllowlistSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAllowlist, err)
	}
	return ParseAllowlist(data)
}

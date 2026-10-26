package hostprobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/autumnsgrove/meridian/internal/wire"
)

const (
	// maxTargetsSize is the cap the executor applies to targets.json too: a file on a path the probe
	// trusts must not be able to feed it a gigabyte of JSON.
	maxTargetsSize = 256 << 10
	// maxObserveSize is generous for a list of unit names and small enough to reject a stray binary.
	maxObserveSize = 64 << 10
)

// UnitList is what the host's two unit lists named: the names that passed the grammar, the ones the
// probe refused, and one line for each file it could not use. It is data rather than log calls so the
// merging rules are testable without a file system.
type UnitList struct {
	Valid    []string
	Dropped  []string
	Problems []string
}

// Units merges the unit lists from observe.json (written for this phase, decision #61) and every
// `unit` field in targets.json (the allowlist). Both files are optional: Phase 1 has no allowlist, a
// control-plane box has no observation list. Names are deduplicated and sorted, so two runs over the
// same files ask systemctl the same questions in the same order.
func Units(observe, targets []byte) UnitList {
	var (
		list  UnitList
		names []string
	)

	if len(observe) > 0 {
		parsed, err := parseObserve(observe)
		if err != nil {
			list.Problems = append(list.Problems, fmt.Sprintf("observe.json: %v", err))
		} else {
			names = append(names, parsed...)
		}
	}
	if len(targets) > 0 {
		parsed, err := parseTargetUnits(targets)
		if err != nil {
			list.Problems = append(list.Problems, fmt.Sprintf("targets.json: %v", err))
		} else {
			names = append(names, parsed...)
		}
	}

	wanted := make(map[string]bool, len(names))
	refused := make(map[string]bool)
	for _, name := range names {
		if !wire.IsUnitName(name) {
			if !refused[name] {
				refused[name] = true
				list.Dropped = append(list.Dropped, name)
			}
			continue
		}
		if !wanted[name] {
			wanted[name] = true
			list.Valid = append(list.Valid, name)
		}
	}
	sort.Strings(list.Valid)
	sort.Strings(list.Dropped)
	return list
}

// observeFile is /etc/meridian/observe.json: the units to watch before any control plane exists.
type observeFile struct {
	Units []string `json:"units"`
}

// parseObserve decodes observe.json strictly. It is our own shape, so a misspelled key ("unit" for
// "units") must be an error rather than a probe that quietly watches nothing.
func parseObserve(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var file observeFile
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the document")
	}
	return file.Units, nil
}

// parseTargetUnits reads the `unit` field of every target in the allowlist. Unknown fields are
// ignored on purpose: the executor owns the rest of the file, and a newer allowlist must not make an
// older probe watch nothing.
func parseTargetUnits(data []byte) ([]string, error) {
	var file struct {
		Targets map[string]struct {
			Unit string `json:"unit"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Targets == nil {
		return nil, errors.New("no targets")
	}

	units := make([]string, 0, len(file.Targets))
	for _, target := range file.Targets {
		if target.Unit != "" {
			units = append(units, target.Unit)
		}
	}
	return units, nil
}

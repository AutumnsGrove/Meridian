package probe

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/format"
	"github.com/autumnsgrove/meridian/internal/jsonpath"
)

// VitalRunner reads one number or short string for a tile. It returns the value already formatted,
// because the browser formats nothing but ages and search URLs (decision #59), or an error in the
// closed set so the merge step can name the reason.
type VitalRunner func(ctx context.Context, env Env, probe config.Probe) (string, error)

// VitalRunners is every vital kind. The registry test keeps it equal to config.VitalKinds.
var VitalRunners = map[string]VitalRunner{
	"json_field":     vitalJSONField,
	"json_count":     vitalJSONCount,
	"json_sum":       vitalJSONSum,
	"json_ratio_pct": vitalJSONRatioPct,
	"host_field":     vitalHostField,
}

// vitalJSONField renders the value at path, then the config's prefix and suffix around it.
func vitalJSONField(ctx context.Context, env Env, probe config.Probe) (string, error) {
	document, err := fetchJSON(ctx, env, probe)
	if err != nil {
		return "", err
	}
	value, found := jsonpath.Get(document, probe.Path)
	if !found {
		return "", fmt.Errorf("%w: %s", ErrMissingField, probe.Path)
	}
	formatted, err := formatValue(value, probe.Decimals)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, probe.Path)
	}
	return format.Compose(probe.Prefix, formatted, probe.Suffix), nil
}

// vitalJSONCount counts the elements of where that match field and equals, or the whole array when
// the config gives neither.
func vitalJSONCount(ctx context.Context, env Env, probe config.Probe) (string, error) {
	document, err := fetchJSON(ctx, env, probe)
	if err != nil {
		return "", err
	}
	elements, err := arrayAt(document, probe.Where)
	if err != nil {
		return "", err
	}

	count := len(elements)
	if probe.Has("field") && probe.Has("equals") {
		count = 0
		for _, element := range elements {
			value, found := jsonpath.Get(element, probe.Field)
			if found && jsonpath.Equal(value, probe.Equals) {
				count++
			}
		}
	}
	return format.Compose("", strconv.Itoa(count), probe.Suffix), nil
}

// vitalJSONSum adds the named numeric fields over the elements of where. A field that is not a number
// is skipped, but if no number was found at all the answer is missing_field: showing 0 would claim a
// measurement the service never reported.
func vitalJSONSum(ctx context.Context, env Env, probe config.Probe) (string, error) {
	document, err := fetchJSON(ctx, env, probe)
	if err != nil {
		return "", err
	}
	elements, err := arrayAt(document, probe.Where)
	if err != nil {
		return "", err
	}

	var sum float64
	found := false
	for _, element := range elements {
		for _, field := range probe.Fields {
			value, present := jsonpath.Get(element, field)
			if !present {
				continue
			}
			number, isNumber := jsonpath.Number(value)
			if !isNumber {
				continue
			}
			sum += number
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("%w: no number at %s", ErrMissingField, strings.Join(probe.Fields, ", "))
	}

	var formatted string
	switch probe.Scale {
	case "bytes":
		var ok bool
		formatted, ok = format.Bytes(sum, probe.Unit, probe.Decimals)
		if !ok {
			return "", fmt.Errorf("%w: %v bytes cannot be formatted", ErrBadValue, sum)
		}
	case "none":
		formatted = format.Number(sum, probe.Decimals)
	default:
		return "", fmt.Errorf("%w: scale %q is not bytes or none", ErrBadValue, probe.Scale)
	}
	return format.Compose(probe.Prefix, formatted, probe.Suffix), nil
}

// vitalJSONRatioPct reports numerator divided by denominator as a percentage. Zero over zero is 0,
// because a service that has seen no traffic has a well defined empty ratio; anything else over zero
// is bad_value rather than an infinity.
func vitalJSONRatioPct(ctx context.Context, env Env, probe config.Probe) (string, error) {
	document, err := fetchJSON(ctx, env, probe)
	if err != nil {
		return "", err
	}
	numerator, err := numberAt(document, probe.Numerator)
	if err != nil {
		return "", err
	}
	denominator, err := numberAt(document, probe.Denominator)
	if err != nil {
		return "", err
	}

	var percentage float64
	switch {
	case denominator == 0 && numerator == 0:
		percentage = 0
	case denominator == 0:
		return "", fmt.Errorf("%w: %v over zero has no percentage", ErrBadValue, numerator)
	default:
		percentage = numerator / denominator * 100
		if math.IsInf(percentage, 0) || math.IsNaN(percentage) {
			return "", fmt.Errorf("%w: the ratio is not a finite number", ErrBadValue)
		}
	}
	return format.Compose("", format.Number(percentage, probe.Decimals), probe.Suffix), nil
}

// vitalHostField renders a value from the host snapshot the way json_field renders JSON. A stale or
// missing snapshot is host_stale or host_unavailable, so the merge step can keep the last good value.
func vitalHostField(_ context.Context, env Env, probe config.Probe) (string, error) {
	snapshot, err := readHost(env)
	if err != nil {
		return "", err
	}
	value, found := snapshot.Field(probe.Path)
	if !found {
		return "", fmt.Errorf("%w: %s", ErrMissingField, probe.Path)
	}
	formatted, err := formatValue(value, probe.Decimals)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, probe.Path)
	}
	return format.Compose(probe.Prefix, formatted, probe.Suffix), nil
}

// arrayAt reads the array at a dotted path. A path that is not there is missing_field, and a value
// that is not an array is unexpected_type, so the config can tell those two apart.
func arrayAt(document any, path string) ([]any, error) {
	value, found := jsonpath.Get(document, path)
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrMissingField, path)
	}
	elements, isArray := value.([]any)
	if !isArray {
		return nil, fmt.Errorf("%w: %s is not an array", ErrUnexpectedType, path)
	}
	return elements, nil
}

// numberAt reads the number at a dotted path, with the same missing-versus-wrong-type distinction.
func numberAt(document any, path string) (float64, error) {
	value, found := jsonpath.Get(document, path)
	if !found {
		return 0, fmt.Errorf("%w: %s", ErrMissingField, path)
	}
	number, isNumber := jsonpath.Number(value)
	if !isNumber {
		return 0, fmt.Errorf("%w: %s is not a number", ErrUnexpectedType, path)
	}
	return number, nil
}

// formatValue renders one JSON value for a tile: strings as they are, booleans as yes or no, numbers
// with the config's decimals. Objects, arrays and null are types a vital does not format.
func formatValue(value any, decimals int) (string, error) {
	if text, isString := value.(string); isString {
		return text, nil
	}
	if flag, isBoolean := value.(bool); isBoolean {
		if flag {
			return "yes", nil
		}
		return "no", nil
	}
	number, isNumber := jsonpath.Number(value)
	if !isNumber {
		return "", ErrUnexpectedType
	}
	return format.Number(number, decimals), nil
}

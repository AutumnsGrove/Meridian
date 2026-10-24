// Package jsonpath reads values out of decoded JSON by dotted path, and compares them with the
// type discipline a probe needs: numbers compare numerically, and nothing else is coerced.
package jsonpath

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
)

// ErrTooLarge means the document was longer than the caller's limit.
var ErrTooLarge = errors.New("jsonpath: document too large")

// ErrTrailingData means there was more than one JSON value in the input.
var ErrTrailingData = errors.New("jsonpath: trailing data after the document")

const (
	maxPathLength = 256
	maxSegments   = 16
)

// Decode reads one JSON value of at most limit bytes, keeping numbers as json.Number so no
// precision is lost, and refuses anything after the value. limit must be positive.
func Decode(r io.Reader, limit int64) (any, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrTrailingData
	}
	return document, nil
}

// Get walks a dotted path through objects and arrays. Each segment is an object key or, inside an
// array, a non-negative index ("items.0.name"). The empty path returns the document. A missing key,
// an index out of range, a non-numeric index or walking into a scalar returns false, never a panic.
func Get(document any, path string) (any, bool) {
	if path == "" {
		return document, true
	}
	if len(path) > maxPathLength {
		return nil, false
	}
	segments := strings.Split(path, ".")
	if len(segments) > maxSegments {
		return nil, false
	}

	current := document
	for _, segment := range segments {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[segment]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, ok := arrayIndex(segment)
			if !ok || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// Elements returns the array at path.
func Elements(document any, path string) ([]any, bool) {
	value, ok := Get(document, path)
	if !ok {
		return nil, false
	}
	elements, ok := value.([]any)
	return elements, ok
}

// Number converts a JSON or YAML number to float64. Strings, booleans and NaN or infinity are not
// numbers here: a probe comparing a number to "1" must fail rather than quietly coerce.
func Number(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		if err != nil {
			return 0, false
		}
		return finite(parsed)
	case float64:
		return finite(number)
	case float32:
		return finite(float64(number))
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func finite(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// Equal compares two decoded values with no coercion: numbers to numbers, strings to strings,
// booleans to booleans, and nil to nil. Objects and arrays are never equal to anything.
func Equal(got, want any) bool {
	gotNumber, gotIsNumber := Number(got)
	if gotIsNumber {
		wantNumber, wantIsNumber := Number(want)
		return wantIsNumber && gotNumber == wantNumber
	}

	switch value := got.(type) {
	case string:
		other, ok := want.(string)
		return ok && value == other
	case bool:
		other, ok := want.(bool)
		return ok && value == other
	case nil:
		return want == nil
	default:
		return false
	}
}

// arrayIndex parses a non-negative decimal array index. Strings such as "+1" or "1e3" are not
// indexes, and a very long run of digits is refused before it can overflow.
func arrayIndex(segment string) (int, bool) {
	if segment == "" || len(segment) > 9 {
		return 0, false
	}
	index := 0
	for i := 0; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return 0, false
		}
		index = index*10 + int(segment[i]-'0')
	}
	return index, true
}

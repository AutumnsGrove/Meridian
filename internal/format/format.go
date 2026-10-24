// Package format turns numbers and durations into the strings the page shows. Every function is
// pure: values arrive here as data, and the browser formats nothing but ages and search URLs.
package format

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Number formats value with a fixed number of decimals, clamped to 0..6. There is no thousands
// separator, and a value that rounds to zero is never printed as negative zero.
func Number(value float64, decimals int) string {
	decimals = clampDecimals(decimals)
	formatted := strconv.FormatFloat(value, 'f', decimals, 64)
	if strings.HasPrefix(formatted, "-") && allZeroDigits(formatted[1:]) {
		return formatted[1:]
	}
	return formatted
}

// Bytes formats a byte count in decimal units (KB is 1000) and returns only the number: the unit is
// the config's suffix to name. An empty unit scales to the largest unit that leaves a value of at
// least 1; a named unit (B, KB, MB, GB, TB, PB, EB) forces that unit. Negative or non-finite values,
// and units it does not know, return false.
func Bytes(n float64, unit string, decimals int) (string, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return "", false
	}
	exponent, ok := byteExponent(n, unit)
	if !ok {
		return "", false
	}
	return Number(n/math.Pow(1000, float64(exponent)), decimals), true
}

// Age renders a duration as 12s, 2m, 3h or 4d, rounding down. A negative duration is 0s.
func Age(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	switch {
	case duration < time.Minute:
		return strconv.FormatInt(int64(duration/time.Second), 10) + "s"
	case duration < time.Hour:
		return strconv.FormatInt(int64(duration/time.Minute), 10) + "m"
	case duration < 24*time.Hour:
		return strconv.FormatInt(int64(duration/time.Hour), 10) + "h"
	default:
		return strconv.FormatInt(int64(duration/(24*time.Hour)), 10) + "d"
	}
}

// Compose joins the non-empty parts with one space: prefix, value, suffix.
func Compose(prefix, value, suffix string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{prefix, value, suffix} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

var byteUnitNames = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

func byteExponent(n float64, unit string) (int, bool) {
	if unit == "" {
		exponent := 0
		for exponent < len(byteUnitNames)-1 && n >= math.Pow(1000, float64(exponent+1)) {
			exponent++
		}
		return exponent, true
	}
	for i, name := range byteUnitNames {
		if strings.EqualFold(unit, name) {
			return i, true
		}
	}
	return 0, false
}

func clampDecimals(decimals int) int {
	if decimals < 0 {
		return 0
	}
	if decimals > 6 {
		return 6
	}
	return decimals
}

func allZeroDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] != '0' && text[i] != '.' {
			return false
		}
	}
	return true
}

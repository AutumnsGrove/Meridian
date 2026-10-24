package format_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/format"
)

func TestNumber_FixedDecimalsAndNoNegativeZero(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		decimals int
		want     string
	}{
		{"zero", 0, 0, "0"},
		{"negative zero", math.Copysign(0, -1), 0, "0"},
		{"negative zero with decimals", math.Copysign(0, -1), 2, "0.00"},
		{"rounds to negative zero", -0.0001, 2, "0.00"},
		{"integer with decimals", 2, 1, "2.0"},
		{"half rounds", 0.5, 0, "0"},
		{"half to even rounds up", 999.5, 0, "1000"},
		{"one decimal", 1.25, 1, "1.2"},
		{"huge", 1e21, 0, "1000000000000000000000"},
		{"negative", -1.5, 1, "-1.5"},
		{"clamps decimals high", 1, 10, "1.000000"},
		{"clamps decimals low", 1, -1, "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := format.Number(test.value, test.decimals); got != test.want {
				t.Errorf("Number(%v, %d) = %q, want %q", test.value, test.decimals, got, test.want)
			}
		})
	}
}

func TestBytes_DecimalUnits(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		unit     string
		decimals int
		want     string
		wantOK   bool
	}{
		{"below a kilobyte", 999, "", 0, "999", true},
		{"exactly a kilobyte", 1000, "", 0, "1", true},
		{"megabytes", 2.5e6, "", 1, "2.5", true},
		{"gigabytes", 5e8, "GB", 1, "0.5", true},
		{"unit is case-insensitive", 5e8, "gb", 1, "0.5", true},
		{"raw bytes", 1500, "B", 0, "1500", true},
		{"forced small unit", 5e6, "KB", 0, "5000", true},
		{"terabytes", 2e12, "", 0, "2", true},
		{"negative", -1, "", 0, "", false},
		{"NaN", math.NaN(), "", 0, "", false},
		{"infinity", math.Inf(1), "", 0, "", false},
		{"unknown unit", 1, "furlongs", 0, "", false},
		{"binary unit is refused", 1, "KiB", 0, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := format.Bytes(test.value, test.unit, test.decimals)
			if ok != test.wantOK {
				t.Fatalf("Bytes(%v, %q) ok = %v, want %v", test.value, test.unit, ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Errorf("Bytes(%v, %q, %d) = %q, want %q", test.value, test.unit, test.decimals, got, test.want)
			}
		})
	}
}

func TestAge_RoundsDown(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want string
	}{
		{0, "0s"},
		{12 * time.Second, "12s"},
		{59*time.Second + 999*time.Millisecond, "59s"},
		{time.Minute, "1m"},
		{90 * time.Minute, "1h"},
		{3 * time.Hour, "3h"},
		{23*time.Hour + 59*time.Minute, "23h"},
		{4 * 24 * time.Hour, "4d"},
		{-5 * time.Second, "0s"},
	}
	for _, test := range tests {
		if got := format.Age(test.age); got != test.want {
			t.Errorf("Age(%v) = %q, want %q", test.age, got, test.want)
		}
	}
}

func TestCompose_JoinsTheNonEmptyParts(t *testing.T) {
	tests := []struct {
		prefix, value, suffix string
		want                  string
	}{
		{"", "0.5", "GB today", "0.5 GB today"},
		{"~", "12", "", "~ 12"},
		{"", "12", "", "12"},
		{"", "", "", ""},
		{"a", "", "b", "a b"},
	}
	for _, test := range tests {
		if got := format.Compose(test.prefix, test.value, test.suffix); got != test.want {
			t.Errorf("Compose(%q, %q, %q) = %q, want %q", test.prefix, test.value, test.suffix, got, test.want)
		}
	}
}

func FuzzBytes(f *testing.F) {
	for _, seed := range []struct {
		value    float64
		unit     string
		decimals int
	}{
		{0, "", 0}, {999, "", 1}, {1000, "KB", 2}, {5e8, "GB", 1}, {-1, "", 0}, {math.Inf(1), "MB", 3},
	} {
		f.Add(seed.value, seed.unit, seed.decimals)
	}

	f.Fuzz(func(t *testing.T, value float64, unit string, decimals int) {
		out, ok := format.Bytes(value, unit, decimals)
		if !ok {
			return
		}
		got, err := strconv.ParseFloat(out, 64)
		if err != nil {
			t.Fatalf("Bytes(%v, %q, %d) = %q, which does not parse back: %v", value, unit, decimals, out, err)
		}

		clamped := decimals
		if clamped < 0 {
			clamped = 0
		}
		if clamped > 6 {
			clamped = 6
		}
		exponent := 0
		switch unit {
		case "":
			for exponent < 6 && value >= math.Pow(1000, float64(exponent+1)) {
				exponent++
			}
		default:
			for i, name := range []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"} {
				if equalFoldASCII(unit, name) {
					exponent = i
					break
				}
			}
		}
		want := value / math.Pow(1000, float64(exponent))
		tolerance := math.Pow(10, -float64(clamped))/2 + 1e-9*math.Max(1, math.Abs(want))
		if math.Abs(got-want) > tolerance {
			t.Fatalf("Bytes(%v, %q, %d) = %q, parses to %v, want about %v", value, unit, decimals, out, got, want)
		}
	})
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'a' <= ca && ca <= 'z' {
			ca -= 'a' - 'A'
		}
		if 'a' <= cb && cb <= 'z' {
			cb -= 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

package jsonpath_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/jsonpath"
)

func decode(t *testing.T, document string) any {
	t.Helper()
	doc, err := jsonpath.Decode(strings.NewReader(document), 1<<20)
	if err != nil {
		t.Fatalf("Decode(%q): %v", document, err)
	}
	return doc
}

func TestDecode_UsesNumbersAndRejectsBadInput(t *testing.T) {
	t.Run("numbers stay exact", func(t *testing.T) {
		doc := decode(t, `{"n": 123456789012345678901234567890}`)
		n, ok := jsonpath.Get(doc, "n")
		if !ok {
			t.Fatal("Get(n) = false")
		}
		if got, want := n.(json.Number).String(), "123456789012345678901234567890"; got != want {
			t.Errorf("number = %q, want %q", got, want)
		}
	})

	tests := []struct {
		name     string
		document string
		limit    int64
		wantErr  error
	}{
		{"trailing data", `{"a":1} {"b":2}`, 1 << 20, jsonpath.ErrTrailingData},
		{"trailing garbage", `{"a":1} nope`, 1 << 20, jsonpath.ErrTrailingData},
		{"too large", `{"a":"` + strings.Repeat("x", 64) + `"}`, 10, jsonpath.ErrTooLarge},
		{"empty", ``, 1 << 20, nil},
		{"invalid", `{`, 1 << 20, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := jsonpath.Decode(strings.NewReader(test.document), test.limit)
			if err == nil {
				t.Fatalf("Decode returned no error, want one")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Errorf("Decode error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestGet_WalksObjectsAndArrays(t *testing.T) {
	doc := decode(t, `{
		"items": [
			{"name": "first", "needs": true},
			{"name": "second", "needs": false}
		],
		"2024": {"total": 3},
		"nested": {"deep": {"deeper": {"value": null}}},
		"zero": 0,
		"text": "plain"
	}`)

	tests := []struct {
		name   string
		path   string
		want   any
		wantOK bool
	}{
		{"object key", "text", "plain", true},
		{"array index", "items.0.name", "first", true},
		{"array second element", "items.1.needs", false, true},
		{"numeric object key", "2024.total", json.Number("3"), true},
		{"deep path", "nested.deep.deeper.value", nil, true},
		{"zero", "zero", json.Number("0"), true},
		{"missing key", "nope", nil, false},
		{"index past the end", "items.2", nil, false},
		{"negative index", "items.-1", nil, false},
		{"non-numeric index", "items.first", nil, false},
		{"walk through a scalar", "text.length", nil, false},
		{"empty segment", "items..name", nil, false},
		{"leading dot", ".text", nil, false},
		{"trailing dot", "text.", nil, false},
		{"too many segments", strings.Repeat("a.", 20) + "b", nil, false},
		{"path too long", strings.Repeat("a", 257), nil, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := jsonpath.Get(doc, test.path)
			if ok != test.wantOK {
				t.Fatalf("Get(%q) ok = %v, want %v (value %v)", test.path, ok, test.wantOK, got)
			}
			if ok && !jsonpath.Equal(got, test.want) {
				t.Errorf("Get(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}

	t.Run("empty path returns the document itself", func(t *testing.T) {
		got, ok := jsonpath.Get(doc, "")
		if !ok {
			t.Fatal("Get(\"\") ok = false")
		}
		if _, isMap := got.(map[string]any); !isMap {
			t.Errorf("Get(\"\") = %T, want the decoded map", got)
		}
	})

	t.Run("nil document", func(t *testing.T) {
		if _, ok := jsonpath.Get(nil, "a"); ok {
			t.Error("Get(nil, a) ok = true, want false")
		}
		if got, ok := jsonpath.Get(nil, ""); !ok || got != nil {
			t.Errorf("Get(nil, \"\") = %v, %v, want nil, true", got, ok)
		}
	})
}

func TestNumber_AcceptsNumbersOnly(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		want   float64
		wantOK bool
	}{
		{"json integer", json.Number("42"), 42, true},
		{"json decimal", json.Number("1.5"), 1.5, true},
		{"json exponent", json.Number("1e3"), 1000, true},
		{"float64", float64(2.25), 2.25, true},
		{"float32", float32(0.5), 0.5, true},
		{"int", 7, 7, true},
		{"int64", int64(-7), -7, true},
		{"uint", uint(7), 7, true},
		{"numeric string", "1", 0, false},
		{"boolean", true, 0, false},
		{"nil", nil, 0, false},
		{"array", []any{1}, 0, false},
		{"invalid json number", json.Number("nope"), 0, false},
		{"NaN", math.NaN(), 0, false},
		{"positive infinity", math.Inf(1), 0, false},
		{"negative infinity", math.Inf(-1), 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := jsonpath.Number(test.value)
			if ok != test.wantOK {
				t.Fatalf("Number(%v) ok = %v, want %v", test.value, ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Errorf("Number(%v) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestEqual_IsTypeAwareAndCoercesNothing(t *testing.T) {
	tests := []struct {
		name  string
		got   any
		want  any
		equal bool
	}{
		{"numbers compare numerically", json.Number("1"), 1, true},
		{"int and float", 2, 2.0, true},
		{"different numbers", json.Number("1.5"), 2, false},
		{"numeric string is not a number", "1", 1, false},
		{"boolean string is not a boolean", "true", true, false},
		{"strings", "ok", "ok", true},
		{"different strings", "ok", "OK", false},
		{"booleans", false, false, true},
		{"different booleans", true, false, false},
		{"nil matches nil", nil, nil, true},
		{"nil is not zero", nil, 0, false},
		{"zero is not nil", 0, nil, false},
		{"arrays are not compared", []any{1}, []any{1}, false},
		{"maps are not compared", map[string]any{}, map[string]any{}, false},
		{"bool is not a number", true, 1, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := jsonpath.Equal(test.got, test.want); got != test.equal {
				t.Errorf("Equal(%v, %v) = %v, want %v", test.got, test.want, got, test.equal)
			}
			if got := jsonpath.Equal(test.want, test.got); got != test.equal {
				t.Errorf("Equal(%v, %v) = %v, want %v (symmetric)", test.want, test.got, got, test.equal)
			}
		})
	}
}

func TestElements_ReturnsArraysOnly(t *testing.T) {
	doc := decode(t, `{"list": [1, 2, 3], "object": {}, "empty": []}`)

	if got, ok := jsonpath.Elements(doc, "list"); !ok || len(got) != 3 {
		t.Errorf("Elements(list) = %v, %v, want three elements", got, ok)
	}
	if got, ok := jsonpath.Elements(doc, "empty"); !ok || len(got) != 0 {
		t.Errorf("Elements(empty) = %v, %v, want an empty slice", got, ok)
	}
	for _, path := range []string{"object", "nope", ""} {
		if _, ok := jsonpath.Elements(doc, path); ok {
			t.Errorf("Elements(%q) ok = true, want false", path)
		}
	}
}

// FuzzGet holds the lookup to JSON's own value set: whatever document decodes, a found value is one
// of the types JSON can produce, and a miss says so instead of inventing a zero value.
func FuzzGet(f *testing.F) {
	for _, seed := range []string{
		`{"a": {"b": [1, 2, {"c": true}]}}`,
		`[{"name": "x"}]`,
		`null`,
		`{"2024": 1}`,
		`{"": ""}`,
	} {
		f.Add(seed, "a.b.0.c")
		f.Add(seed, "")
		f.Add(seed, "items.0.name")
	}
	f.Add(strings.Repeat("[", 64)+strings.Repeat("]", 64), "0.0")
	f.Add(`{"a":1}`, strings.Repeat("a.", 30))

	f.Fuzz(func(t *testing.T, document, path string) {
		doc, err := jsonpath.Decode(strings.NewReader(document), 1<<20)
		if err != nil {
			return
		}
		value, ok := jsonpath.Get(doc, path)
		if !ok {
			return
		}
		switch value.(type) {
		case nil, bool, string, json.Number, []any, map[string]any:
		default:
			t.Fatalf("Get returned %T, which JSON cannot produce", value)
		}
	})
}

// FuzzEqual holds the comparison to the properties a probe rule relies on: symmetry for any pair of
// decoded documents, and reflexivity for the scalars it is documented to compare.
func FuzzEqual(f *testing.F) {
	for _, seed := range [][2]string{
		{"1", "1.0"}, {`"1"`, "1"}, {"true", `"true"`}, {"null", "null"}, {"0", "0.0"}, {"[1]", "[1]"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, leftJSON, rightJSON string) {
		left, leftErr := jsonpath.Decode(strings.NewReader(leftJSON), 1<<20)
		right, rightErr := jsonpath.Decode(strings.NewReader(rightJSON), 1<<20)
		if leftErr != nil || rightErr != nil {
			return
		}
		if jsonpath.Equal(left, right) != jsonpath.Equal(right, left) {
			t.Fatalf("Equal is not symmetric for %q and %q", leftJSON, rightJSON)
		}
		// Equal compares scalars; a rule that matches objects or arrays would need deep equality.
		switch left.(type) {
		case nil, bool, string, json.Number:
			if !jsonpath.Equal(left, left) {
				t.Fatalf("Equal is not reflexive for %q", leftJSON)
			}
		}
	})
}

// FuzzNumber holds the numeric coercion to a finite result: whatever decodes, a number it reports is
// never NaN or an infinity.
func FuzzNumber(f *testing.F) {
	for _, seed := range []string{"0", "-0", "1", "1.5", "1e309", "-1e309", `"1"`, "true", "{}", "null"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, document string) {
		doc, err := jsonpath.Decode(strings.NewReader(document), 1<<20)
		if err != nil {
			return
		}
		value, ok := jsonpath.Number(doc)
		if ok && (math.IsNaN(value) || math.IsInf(value, 0)) {
			t.Fatalf("Number(%q) = %v, which is not finite", document, value)
		}
	})
}

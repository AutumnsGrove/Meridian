package config

// This file is the single place the probe vocabulary exists (decisions #40 and #46). Validation,
// doctor and the generated docs all read it, so they cannot disagree about what a kind takes.

// NeedKind is the shape of a cross-field rule inside one probe.
type NeedKind string

const (
	// NeedExactlyOne requires exactly one of Keys to be present.
	NeedExactlyOne NeedKind = "exactly_one"
	// NeedAtMostOne allows zero or one of Keys, but not two.
	NeedAtMostOne NeedKind = "at_most_one"
	// NeedBothOrNeither requires all of Keys together, or none of them.
	NeedBothOrNeither NeedKind = "both_or_neither"
	// NeedOneOfValues requires the value of Keys[0] to be one of Values.
	NeedOneOfValues NeedKind = "one_of_values"
)

// Need is one cross-field rule: which keys, and which values when the rule checks a value.
type Need struct {
	Kind   NeedKind
	Keys   []string
	Values []string
}

// KindRules is everything config validation needs to know about one probe or vital kind.
type KindRules struct {
	Doc      string
	Required []string
	Optional []string
	Needs    []Need
}

// HealthKinds is every health probe kind and what it takes.
var HealthKinds = map[string]KindRules{
	"http_status": {
		Doc:      "status code in expect or expect_class",
		Required: []string{"url"},
		Optional: []string{"expect", "expect_class", "auth"},
		Needs:    []Need{{Kind: NeedAtMostOne, Keys: []string{"expect", "expect_class"}}},
	},
	"http_json": {
		Doc:      "JSON value at path matches equals, one_of or exists",
		Required: []string{"url", "path"},
		Optional: []string{"equals", "one_of", "exists", "auth"},
		Needs:    []Need{{Kind: NeedExactlyOne, Keys: []string{"equals", "one_of", "exists"}}},
	},
	"dns": {
		Doc:      "a DNS query to addr for qname gets expect_rcode",
		Required: []string{"addr", "qname"},
		Optional: []string{"expect_rcode"},
	},
	"host_unit": {
		Doc:      "systemd unit active in the host snapshot",
		Required: []string{"unit"},
	},
	"host_field": {
		Doc:      "value at path in the host snapshot",
		Required: []string{"path"},
		Optional: []string{"equals"},
	},
	"self": {
		Doc: "always ok; this process is answering",
	},
}

// VitalKinds is every vital kind and what it takes.
var VitalKinds = map[string]KindRules{
	"json_field": {
		Doc:      "the JSON value at path, formatted",
		Required: []string{"url", "path"},
		Optional: []string{"prefix", "suffix", "decimals", "auth"},
	},
	"json_count": {
		Doc:      "array length, or elements whose field equals equals",
		Required: []string{"url", "where"},
		Optional: []string{"field", "equals", "suffix", "auth"},
		Needs:    []Need{{Kind: NeedBothOrNeither, Keys: []string{"field", "equals"}}},
	},
	"json_sum": {
		Doc:      "sum of fields over the elements of where",
		Required: []string{"url", "where", "fields", "scale"},
		Optional: []string{"unit", "decimals", "prefix", "suffix", "auth"},
		Needs:    []Need{{Kind: NeedOneOfValues, Keys: []string{"scale"}, Values: []string{"bytes", "none"}}},
	},
	"json_ratio_pct": {
		Doc:      "numerator divided by denominator, as a percentage",
		Required: []string{"url", "numerator", "denominator"},
		Optional: []string{"suffix", "decimals", "auth"},
	},
	"host_field": {
		Doc:      "value at path in the host snapshot, formatted",
		Required: []string{"path"},
		Optional: []string{"prefix", "suffix", "decimals"},
	},
}

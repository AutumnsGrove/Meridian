package config

import (
	"fmt"
	"sort"
	"strings"
)

// kindColumn is the width the kind name is padded to. The longest name is json_ratio_pct (14), plus
// two spaces of separation from the sentence that follows.
const kindColumn = 16

// KindsDoc returns the probe and vital vocabulary as plain text: the same content `meridian docs
// kinds` prints and the modelled block in config.yaml.example carries. It is generated from
// HealthKinds and VitalKinds so the template, the CLI and the validators cannot disagree.
func KindsDoc() string {
	var builder strings.Builder
	builder.WriteString("Health kinds\n\n")
	writeKindTable(&builder, HealthKinds)
	builder.WriteString("\nVital kinds\n\n")
	writeKindTable(&builder, VitalKinds)
	return builder.String()
}

// writeKindTable writes one family, one kind per two lines: what it takes, then what it means. Kinds
// are sorted so the output is stable without a sort at the call site.
func writeKindTable(builder *strings.Builder, table map[string]KindRules) {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		rules := table[name]
		fmt.Fprintf(builder, "  %-*s%s\n", kindColumn, name, kindTakes(rules))
		fmt.Fprintf(builder, "  %-*s%s\n", kindColumn, "", rules.Doc)
	}
}

// kindTakes turns a kind's field rules into one phrase: what is required, the cross-field rules, then
// what is optional. Keys a cross-field rule already governs are not repeated in the optional list, so
// a kind that needs "exactly one of a, b" does not also read "optional a, b".
func kindTakes(rules KindRules) string {
	parts := make([]string, 0, 2+len(rules.Needs))
	if len(rules.Required) > 0 {
		parts = append(parts, "requires "+strings.Join(rules.Required, ", "))
	}
	governed := make(map[string]bool, len(rules.Needs)*2)
	for _, need := range rules.Needs {
		parts = append(parts, needSentence(need))
		for _, key := range need.Keys {
			governed[key] = true
		}
	}
	optional := make([]string, 0, len(rules.Optional))
	for _, key := range rules.Optional {
		if !governed[key] {
			optional = append(optional, key)
		}
	}
	if len(optional) > 0 {
		parts = append(parts, "optional "+strings.Join(optional, ", "))
	}
	if len(parts) == 0 {
		return "no fields"
	}
	return strings.Join(parts, "; ")
}

// needSentence renders one cross-field rule in the same words validation uses when it refuses.
func needSentence(need Need) string {
	switch need.Kind {
	case NeedExactlyOne:
		return "exactly one of " + strings.Join(need.Keys, ", ")
	case NeedAtMostOne:
		return "at most one of " + strings.Join(need.Keys, ", ")
	case NeedBothOrNeither:
		return strings.Join(need.Keys, " and ") + " together, or neither"
	case NeedOneOfValues:
		return need.Keys[0] + " is one of " + strings.Join(need.Values, ", ")
	}
	return ""
}

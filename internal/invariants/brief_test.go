package invariants_test

// The review brief's fuzz-target table is generated from the code it describes, so it cannot drift:
// a target that is added, renamed or loses its doc comment fails this test until the brief is
// regenerated. The property column is the comment above the Fuzz function, which is why every target
// needs one.
//
// Regenerate after a change to the targets:
//
//	go test ./internal/invariants -run TestReviewBrief -update

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	briefPath      = "../../docs/REVIEW-BRIEF.md"
	fuzzBegin      = "<!-- fuzz-targets:begin -->"
	fuzzEnd        = "<!-- fuzz-targets:end -->"
	updateBriefDoc = "go test ./internal/invariants -run TestReviewBrief -update"
)

// updateBrief rewrites the generated section of docs/REVIEW-BRIEF.md. It is never set by
// scripts/check, exactly like the wire goldens' -update flag.
var updateBrief = flag.Bool("update", false, "rewrite the generated fuzz-target table in docs/REVIEW-BRIEF.md")

var fuzzFuncLine = regexp.MustCompile(`^func (Fuzz[A-Za-z0-9_]+)\(`)

// fuzzTarget is one row of the table: where the target lives, what it is called, and the property its
// doc comment states.
type fuzzTarget struct {
	pkg      string
	target   string
	property string
}

// TestReviewBrief_FuzzTargetsAreGenerated is the drift gate. Without -update it fails when the table
// in the brief differs from the table the code produces; with -update it rewrites that section and
// fails only if a target has no doc comment to describe.
func TestReviewBrief_FuzzTargetsAreGenerated(t *testing.T) {
	targets := collectFuzzTargets(t)

	want := renderFuzzSection(targets)
	document, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read %s: %v", briefPath, err)
	}

	text := string(document)
	start := bytes.Index(document, []byte(fuzzBegin))
	end := bytes.Index(document, []byte(fuzzEnd))
	if start < 0 || end < start {
		t.Fatalf("%s is missing its %q and %q markers", briefPath, fuzzBegin, fuzzEnd)
	}
	end += len(fuzzEnd)
	got := text[start:end]

	if *updateBrief {
		updated := text[:start] + want + text[end:]
		if err := os.WriteFile(briefPath, []byte(updated), 0o644); err != nil { //nolint:gosec // a checked-in document, not input
			t.Fatalf("write %s: %v", briefPath, err)
		}
		t.Logf("rewrote the fuzz-target table: %d targets in %d packages", len(targets), packageCount(targets))
		return
	}

	if got != want {
		t.Errorf("%s's fuzz-target table no longer matches the code: %s\n%s",
			briefPath, updateBriefDoc, firstDifference(got, want))
	}
}

// collectFuzzTargets finds every Fuzz function under the repo root and the comment block above it.
// A target without a doc comment is a hard failure: an empty property cell is worse than no table.
func collectFuzzTargets(t *testing.T) []fuzzTarget {
	t.Helper()

	var targets []fuzzTarget
	root := repoRoot
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path) //nolint:gosec // a path from the tree being walked
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}

		var comment []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "//") {
				comment = append(comment, strings.TrimSpace(strings.TrimPrefix(line, "//")))
				continue
			}
			match := fuzzFuncLine.FindStringSubmatch(line)
			if match == nil {
				comment = nil
				continue
			}
			property := strings.Join(strings.Fields(strings.Join(comment, " ")), " ")
			if property == "" {
				t.Fatalf("%s: %s has no doc comment above it, so the brief cannot say what it asserts", path, match[1])
			}
			targets = append(targets, fuzzTarget{
				pkg:      filepath.ToSlash(rel),
				target:   match[1],
				property: strings.ReplaceAll(property, "|", `\|`),
			})
			comment = nil
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repo for fuzz targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("found no Fuzz targets; the scan is looking in the wrong place")
	}

	sort.Slice(targets, func(i, j int) bool {
		if targets[i].pkg != targets[j].pkg {
			return targets[i].pkg < targets[j].pkg
		}
		return targets[i].target < targets[j].target
	})
	return targets
}

// renderFuzzSection is the exact text between the markers, markers included.
func renderFuzzSection(targets []fuzzTarget) string {
	var builder strings.Builder
	builder.WriteString(fuzzBegin)
	builder.WriteString("\n")
	fmt.Fprintf(&builder, "%d fuzz targets in %d packages.\n\n", len(targets), packageCount(targets))
	builder.WriteString("| Package | Target | Property asserted |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, target := range targets {
		fmt.Fprintf(&builder, "| `%s` | `%s` | %s |\n", target.pkg, target.target, target.property)
	}
	builder.WriteString(fuzzEnd)
	return builder.String()
}

func packageCount(targets []fuzzTarget) int {
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		seen[target.pkg] = true
	}
	return len(seen)
}

// firstDifference names the first line that disagrees, so a failure reads like a diff rather than 34
// rows of table.
func firstDifference(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return fmt.Sprintf("line %d:\n  brief: %s\n  code:  %s", i+1, gotLines[i], wantLines[i])
		}
	}
	if len(gotLines) != len(wantLines) {
		return fmt.Sprintf("the brief has %d lines, the code produces %d", len(gotLines), len(wantLines))
	}
	return "the tables differ beyond the lines compared"
}

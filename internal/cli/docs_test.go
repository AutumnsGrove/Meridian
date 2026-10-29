package cli_test

import (
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/config"
)

// TestDocs_KindsPrintsTheVocabulary is the CLI's half of the contract: `meridian docs kinds` prints
// exactly what config.KindsDoc generates, which is what config.yaml.example's generated block must
// match. A command that prints anything else makes the example unverifiable.
func TestDocs_KindsPrintsTheVocabulary(t *testing.T) {
	code, stdout, stderr := doctorRun(t, "", "docs", "kinds")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if stdout != config.KindsDoc() {
		t.Errorf("docs kinds output does not match config.KindsDoc():\n%s", stdout)
	}
	if !strings.HasPrefix(stdout, "Health kinds\n") || !strings.Contains(stdout, "\nVital kinds\n") {
		t.Errorf("output does not name both families:\n%s", stdout)
	}
}

package invariants_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// TestContract_TheBrowserSendsTheAcksTheExecutorExpects: the acknowledgement tokens are defined once,
// in wire's action table, and the control page keeps a courtesy copy in web/src/lib/state/control.ts
// (decision #16). If the two drift, every stop and reboot is refused with bad_ack, and nothing but a
// person pressing the button would notice. This reads the client source for each token the table
// can demand.
func TestContract_TheBrowserSendsTheAcksTheExecutorExpects(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "web", "src", "lib", "state", "control.ts"))
	if err != nil {
		t.Fatalf("read the control page's state module: %v", err)
	}
	client := string(data)

	seen := map[string]bool{}
	for _, action := range wire.KnownActions() {
		for _, dns := range []bool{false, true} {
			token := wire.RequiredAck(action, dns)
			if token == "" || seen[token] {
				continue
			}
			seen[token] = true
			if !strings.Contains(client, "'"+token+"'") {
				t.Errorf("the executor requires ack %q for %s (dns %v), but control.ts never sends it", token, action, dns)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("the action table demands no acknowledgement at all; this test is guarding nothing")
	}
}

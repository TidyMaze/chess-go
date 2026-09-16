package engine

import (
	"strings"
	"testing"
)

// The info line must carry nodes, nps and the time spent, not just depth
// and score. A GUI shows them, and any comparison against another engine
// is a comparison of nodes at a depth: without them the only way to read
// our tree size was a Go benchmark, which cannot be pointed at the same
// position another engine just searched.
func TestUCIInfoReportsNodesAndSpeed(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader("uci\nposition startpos moves e2e4\ngo movetime 20\nquit\n")
	ServeUCI(in, &out, Strong(2))
	info := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "info ") {
			info = line
		}
	}
	if info == "" {
		t.Fatalf("no info line:\n%s", out.String())
	}
	for _, field := range []string{"depth ", "score cp ", "nodes ", "nps ", "time "} {
		if !strings.Contains(info, field) {
			t.Errorf("info line has no %q: %s", field, info)
		}
	}
}

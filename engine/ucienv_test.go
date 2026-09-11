package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The harness caps its concurrent games with GOMAXPROCS, and a child engine
// must not inherit that cap: a four-thread engine started under GOMAXPROCS=2
// runs its four searchers on two processors. The 100 ms SMP race measured
// that, not the threads.
func TestAUCIChildDoesNotInheritTheHarnessProcessorCap(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "env")
	script := filepath.Join(dir, "env-uci.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"${GOMAXPROCS-unset}\" > "+seen+"\nwhile read -r line; do\n  case \"$line\" in\n    uci*) echo uciok;;\n    isready) echo readyok;;\n    quit) exit 0;;\n  esac\ndone\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMAXPROCS", "2")
	e, err := NewStockfish(script, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(string(got)); s != "unset" {
		t.Errorf("the child saw GOMAXPROCS=%q; it must not inherit the harness cap", s)
	}
}

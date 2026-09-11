package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// The match driver rebuilds the gauntlet before it races. Three races in
// one evening ran on a gauntlet-bin older than the engine; the last one
// raced a second-layer network through a loader that could not read it and
// reported "chunk failed" as the result of a 400-game screen. Zero games
// runs the driver's preamble and nothing else.
func TestTheMatchDriverRebuildsTheGauntletBeforeRacing(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool on PATH")
	}
	start := time.Now()
	cmd := exec.Command("sh", "scripts/chunked_match.sh", "0", "40")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "CHESS_SPRT=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver failed on zero games: %v\n%s", err, out)
	}
	info, err := os.Stat("../gauntlet-bin")
	if err != nil {
		t.Fatalf("the driver left no gauntlet-bin behind: %v", err)
	}
	if info.ModTime().Before(start) {
		t.Errorf("gauntlet-bin dates from %s, before this run at %s: the driver raced whatever binary was lying around",
			info.ModTime().Format("15:04:05"), start.Format("15:04:05"))
	}
}

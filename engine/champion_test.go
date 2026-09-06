package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChampionRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "champion.json")
	want := Champion{Label: "book", Depth: 6, Elo: 2100, Margin: 40, HandBlend: 0.45}
	if err := WriteChampion(path, want); err != nil {
		t.Fatal(err)
	}
	got := ReadChampion(path)
	if got.Label != want.Label || got.Depth != want.Depth || got.HandBlend != want.HandBlend {
		t.Errorf("got %+v, want label/depth/blend from %+v", got, want)
	}
	if got.Adopted == "" {
		t.Error("save should stamp an adoption time")
	}
}

// A missing or corrupt descriptor must not stop a human playing, so it
// falls back rather than failing.
func TestChampionFallsBackWhenUnreadable(t *testing.T) {
	dir := t.TempDir()
	if c := ReadChampion(filepath.Join(dir, "absent.json")); c.Depth != 5 {
		t.Errorf("missing file gave depth %d, want the default 5", c.Depth)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if c := ReadChampion(bad); c.Depth != 5 {
		t.Errorf("corrupt file gave depth %d, want the default 5", c.Depth)
	}
}

// The whole point of the watcher: an adoption mid-campaign must reach the
// browser without restarting the server.
func TestChampionWatcherSeesAnAdoption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "champion.json")
	if err := WriteChampion(path, Champion{Label: "before", Depth: 4}); err != nil {
		t.Fatal(err)
	}
	w := NewChampionWatcher(path)
	if c, _ := w.Current(); c.Label != "before" {
		t.Fatalf("first read gave %q", c.Label)
	}
	// The watcher only restats once a second, so wait past that: the
	// alternative is a stat on every move, which is the cost this avoids.
	time.Sleep(1100 * time.Millisecond)
	if err := WriteChampion(path, Champion{Label: "after", Depth: 6}); err != nil {
		t.Fatal(err)
	}
	c, p := w.Current()
	if c.Label != "after" {
		t.Errorf("after adoption the watcher still reports %q", c.Label)
	}
	if p.Depth != 6 {
		t.Errorf("player depth %d, want the adopted 6", p.Depth)
	}
}

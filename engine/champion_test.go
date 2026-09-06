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

// A champion that names a network which cannot be loaded must say so, not
// quietly play without it.
//
// This is the failure that would make the bootstrap ladder a no-op loop:
// each rung labels its training positions with the current champion, so a
// silent fall back to the hand-written evaluation makes every rung produce
// the labels of rung zero. Nothing in the loss, the smoothness or the Elo
// would reveal it; the ladder would simply never climb.
func TestChampionReportsAMissingNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "champion.json")
	if err := WriteChampion(path, Champion{
		Label: "names a net that is not there", Depth: 4,
		NetFile: filepath.Join(t.TempDir(), "absent.json"), HandBlend: 0.45,
	}); err != nil {
		t.Fatal(err)
	}
	c := ReadChampion(path)
	p, err := c.PlayerOrError()
	if err == nil {
		t.Fatal("a champion naming an absent network loaded without error")
	}
	if p.HalfKP != nil {
		t.Error("the degraded player carries a network anyway")
	}
	// Player() still degrades, because the browser must keep working.
	if got := c.Player(); got.HalfKP != nil {
		t.Error("Player() should degrade to the hand evaluation, not carry a half-loaded net")
	}
}

// And a champion whose network does load must carry it, or the ladder is
// labelling with something weaker than it believes.
func TestChampionCarriesALoadableNetwork(t *testing.T) {
	dir := t.TempDir()
	netPath := filepath.Join(dir, "net.json")
	h := 8
	n := &HalfKPNet{
		H: h, W1: make([]float32, HalfKPInputsFor(halfKPKingBuckets)*h),
		B1: make([]float32, h), W2: make([]float32, 2*h), B2: 0.1, Scale: 1,
	}
	for i := range n.W1 {
		n.W1[i] = 0.01
	}
	if err := n.Save(netPath); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "champion.json")
	if err := WriteChampion(path, Champion{
		Label: "with net", Depth: 4, NetFile: netPath, HandBlend: 0.45,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := ReadChampion(path).PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	if p.HalfKP == nil {
		t.Fatal("a loadable network was not carried onto the player")
	}
	if p.HalfKPBlend != 0.45 {
		t.Errorf("blend %v, want 0.45", p.HalfKPBlend)
	}
}

package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Persistence is the difference between a restart costing a minute and
// costing an afternoon, and it is only worth having if it is exactly
// right: a checkpoint that silently loads the wrong thing is worse than
// no checkpoint, because the run continues and looks fine.

func sampleFixture() []sample {
	return []sample{
		{own: []int32{1, 2, 3}, opp: []int32{4, 5}, target: 1.25, static: -0.5, game: 7},
		{own: []int32{5119, 0}, opp: []int32{2560, 17, 42}, target: -3.5, static: 2.25, game: 8},
		{own: nil, opp: nil, target: 0, static: 0, game: 9},
	}
}

func TestPoolRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.bin")
	want := sampleFixture()
	if err := appendPool(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d samples, wrote %d", len(got), len(want))
	}
	for i := range want {
		if got[i].game != want[i].game {
			t.Errorf("sample %d: game %d, want %d", i, got[i].game, want[i].game)
		}
		// Targets are stored as float32, so exact equality is wrong; the
		// tolerance is float32's resolution, not a fudge factor.
		if math.Abs(got[i].target-want[i].target) > 1e-6 {
			t.Errorf("sample %d: target %v, want %v", i, got[i].target, want[i].target)
		}
		if math.Abs(got[i].static-want[i].static) > 1e-6 {
			t.Errorf("sample %d: static %v, want %v", i, got[i].static, want[i].static)
		}
		if len(got[i].own) != len(want[i].own) || len(got[i].opp) != len(want[i].opp) {
			t.Fatalf("sample %d: feature counts %d/%d, want %d/%d",
				i, len(got[i].own), len(got[i].opp), len(want[i].own), len(want[i].opp))
		}
		for j := range want[i].own {
			if got[i].own[j] != want[i].own[j] {
				t.Errorf("sample %d own[%d]: %d, want %d", i, j, got[i].own[j], want[i].own[j])
			}
		}
		for j := range want[i].opp {
			if got[i].opp[j] != want[i].opp[j] {
				t.Errorf("sample %d opp[%d]: %d, want %d", i, j, got[i].opp[j], want[i].opp[j])
			}
		}
	}
}

// Appending twice must accumulate, since that is how a generation adds
// to the pool without rewriting it.
func TestPoolAppendsAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.bin")
	if err := appendPool(path, sampleFixture()); err != nil {
		t.Fatal(err)
	}
	if err := appendPool(path, sampleFixture()); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Errorf("read %d samples after two appends of 3, want 6", len(got))
	}
}

// The limit is the sliding window, and it must keep the most recent
// positions: those came from the strongest champion.
func TestPoolLimitKeepsTheNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.bin")
	var all []sample
	for i := 0; i < 50; i++ {
		all = append(all, sample{own: []int32{int32(i)}, opp: []int32{1}, game: int32(i)})
	}
	if err := appendPool(path, all); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("read %d samples, want 10", len(got))
	}
	if got[0].game != 40 || got[9].game != 49 {
		t.Errorf("kept games %d..%d, want 40..49", got[0].game, got[9].game)
	}
}

// A process killed mid-write leaves a partial record. That must cost the
// last position, not the whole file: the point of the file is that a
// crash is cheap.
func TestPoolToleratesATruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.bin")
	if err := appendPool(path, sampleFixture()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Chop the last few bytes, as an interrupted write would.
	if err := os.WriteFile(path, data[:len(data)-5], 0644); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(path, 0)
	if err != nil {
		t.Fatalf("a truncated tail should not be an error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("recovered %d samples from a truncated file, want the 2 complete ones", len(got))
	}
}

func TestNetCheckpointRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "net.gob")
	n := newNetForTest(8)
	n.w1[100] = 1.5
	n.b1[3] = -0.25
	n.w2[5] = 0.75
	n.b2 = 0.125
	// Adam's state is the part most easily forgotten, and losing it
	// restarts every per-weight step size from scratch.
	n.v1[100] = 0.5
	n.vb1[3] = 0.25
	n.v2[5] = 0.0625
	n.vb2 = 0.03125

	if err := saveNet(path, n, 42, 137); err != nil {
		t.Fatal(err)
	}
	got, gen, cum, err := loadNet(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if gen != 42 || cum != 137 {
		t.Errorf("generation %d cumulative %d, want 42 and 137", gen, cum)
	}
	for _, c := range []struct {
		name      string
		got, want float32
	}{
		{"w1", got.w1[100], 1.5},
		{"b1", got.b1[3], -0.25},
		{"w2", got.w2[5], 0.75},
		{"b2", got.b2, 0.125},
		{"v1 (Adam)", got.v1[100], 0.5},
		{"vb1 (Adam)", got.vb1[3], 0.25},
		{"v2 (Adam)", got.v2[5], 0.0625},
		{"vb2 (Adam)", got.vb2, 0.03125},
	} {
		if c.got != c.want {
			t.Errorf("%s: %v, want %v", c.name, c.got, c.want)
		}
	}
}

// Loading a checkpoint from a different architecture must fail loudly.
// Silently accepting it would train a network whose weights mean
// something else, and the run would look healthy while learning nothing.
func TestNetCheckpointRejectsAMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "net.gob")
	if err := saveNet(path, newNetForTest(8), 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadNet(path, 16); err == nil {
		t.Error("a checkpoint with 8 hidden units was accepted for a 16-unit network")
	}
}

func TestMissingCheckpointIsNotAnError(t *testing.T) {
	_, _, _, err := loadNet(filepath.Join(t.TempDir(), "absent.gob"), 16)
	if !os.IsNotExist(err) {
		t.Errorf("a missing checkpoint should report as not-exist so a fresh run proceeds, got %v", err)
	}
}

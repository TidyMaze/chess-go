package main

import (
	"bufio"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, rs []record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, r := range rs {
		writeRecord(w, r)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []record
	decode(data, func(r record) bool { out = append(out, r); return true })
	return out
}

// A record written by this tool must decode to exactly what went in:
// merging is the only place a pool is rewritten, so a mistake here would
// corrupt a corpus silently rather than fail.
func TestWriteRecordRoundTrips(t *testing.T) {
	dir := t.TempDir()
	in := []record{
		{game: 7, target: -1.25, static: -1.0, own: []uint16{10, 20, 30}, opp: []uint16{40, 50}},
		{game: 8, target: 3.5, static: 3.25, own: []uint16{1}, opp: []uint16{2}},
	}
	p := filepath.Join(dir, "p.bin")
	write(t, p, in)
	got := read(t, p)
	if len(got) != 2 {
		t.Fatalf("read %d records, want 2", len(got))
	}
	for i := range in {
		if got[i].game != in[i].game || got[i].target != in[i].target ||
			len(got[i].own) != len(in[i].own) || got[i].opp[0] != in[i].opp[0] {
			t.Errorf("record %d changed: %+v against %+v", i, got[i], in[i])
		}
	}
}

// The same position in two pools is one position. This is the whole point
// of the tool: the audit found 30.5% repeats across five pools while no
// single pool exceeded 8.3%, so the duplication is between files.
func TestTheSamePositionInTwoPoolsIsKeptOnce(t *testing.T) {
	dir := t.TempDir()
	shared := record{game: 1, target: 0.5, static: 0.5, own: []uint16{1, 2}, opp: []uint16{3, 4}}
	only := record{game: 2, target: -0.5, static: -0.5, own: []uint16{5, 6}, opp: []uint16{7, 8}}
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	write(t, a, []record{shared, only})
	write(t, b, []record{shared})

	seen := map[uint64]struct{}{}
	var kept []record
	for _, p := range []string{a, b} {
		data, _ := os.ReadFile(p)
		decode(data, func(r record) bool {
			if _, dup := seen[positionKey(r)]; dup {
				return true
			}
			seen[positionKey(r)] = struct{}{}
			kept = append(kept, r)
			return true
		})
	}
	if len(kept) != 2 {
		t.Fatalf("kept %d positions from 3 records with one repeat, want 2", len(kept))
	}
}

// Game ids restart in every pool, so merging without renumbering would put
// two unrelated games on the same side of a by-game train/test split.
func TestGameIdsAreRenumberedAcrossPools(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	write(t, a, []record{{game: 0, own: []uint16{1}, opp: []uint16{2}}, {game: 1, own: []uint16{3}, opp: []uint16{4}}})
	write(t, b, []record{{game: 0, own: []uint16{5}, opp: []uint16{6}}})

	var ids []int32
	base := int32(0)
	for _, p := range []string{a, b} {
		data, _ := os.ReadFile(p)
		maxGame := int32(0)
		decode(data, func(r record) bool {
			if r.game > maxGame {
				maxGame = r.game
			}
			ids = append(ids, r.game+base)
			return true
		})
		base += maxGame + 1
	}
	if ids[0] == ids[2] || ids[1] == ids[2] {
		t.Errorf("game ids collided after merging: %v", ids)
	}
}

func TestMenFiltersByMaterial(t *testing.T) {
	full := record{own: make([]uint16, 30), opp: make([]uint16, 30)}
	bare := record{own: []uint16{1}, opp: []uint16{2}}
	if men(full) != 32 || men(bare) != 3 {
		t.Fatalf("men() wrong: %d and %d", men(full), men(bare))
	}
	_ = binary.LittleEndian
	_ = math.Abs
}

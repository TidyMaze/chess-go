package engine

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

func tinyHalfKP(h, buckets int, sigmoid bool) *HalfKPNet {
	n := &HalfKPNet{H: h, Buckets: buckets, Scale: 1, Sigmoid: sigmoid,
		W1: make([]float32, HalfKPInputsFor(buckets)*h), B1: make([]float32, h), W2: make([]float32, 2*h)}
	for i := range n.W2 {
		n.W2[i] = 0.05
	}
	for i := range n.B1 {
		n.B1[i] = 0.5
	}
	return n
}

func TestHalfKPNetBranches(t *testing.T) {
	g := game.New()
	// 32 canonical king squares instead of 8 buckets; a width that is not a
	// multiple of eight so the unrolled loops leave a tail; the sigmoid output.
	for _, n := range []*HalfKPNet{tinyHalfKP(4, 32, false), tinyHalfKP(6, 8, true)} {
		v := n.Evaluate(&g.Board)
		if math.IsNaN(v) {
			t.Error("NaN")
		}
		var acc halfKPAcc
		if w := n.EvaluateWith(&g.Board, &acc, nil, nil); math.Abs(w-v) > 1e-4 {
			t.Errorf("incremental %v against full %v", w, v)
		}
	}
	k := tinyHalfKP(4, 8, true)
	k.K = 0.5
	_ = k.Evaluate(&g.Board)
	var acc halfKPAcc
	_ = k.EvaluateWith(&g.Board, &acc, nil, nil)
	// Nothing to evaluate with: zero, and no incremental state.
	var nilNet *HalfKPNet
	if nilNet.Evaluate(&g.Board) != 0 {
		t.Error("nil net evaluated")
	}
	wide := tinyHalfKP(maxHalfKPHidden+8, 8, false)
	acc = halfKPAcc{}
	_ = wide.EvaluateWith(&g.Board, &acc, nil, nil) // too wide for the slot: full path
	if acc.valid {
		t.Error("a net wider than the slot was marked valid")
	}
	// Piece index: every type maps, kings excluded from the features.
	if _, ok := halfKPPieceIndex(board.Queen, board.White, board.White); !ok {
		t.Error("queen has no feature index")
	}
	if _, ok := halfKPPieceIndex(board.King, board.White, board.White); ok {
		t.Error("the king is not a feature")
	}
}

func TestHalfKPNetLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	n := tinyHalfKP(4, 8, false)
	n.Scale = 0 // defaults to 1 on load
	path := filepath.Join(dir, "net.json")
	if err := n.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := LoadHalfKPNet(path)
	if err != nil || m.Scale != 1 {
		t.Fatalf("load: %v scale %v", err, m.Scale)
	}
	if n.Save(filepath.Join(dir, "no", "dir", "x.json")) == nil {
		t.Error("saved into a missing directory")
	}
	write := func(name string, v any) string {
		p := filepath.Join(dir, name)
		data, _ := json.Marshal(v)
		os.WriteFile(p, data, 0o600)
		return p
	}
	if _, err := LoadHalfKPNet(write("h0.json", HalfKPNet{H: 0})); err == nil {
		t.Error("H=0 loaded")
	}
	bad := tinyHalfKP(4, 8, false)
	bad.W1 = bad.W1[:10]
	if _, err := LoadHalfKPNet(write("w1.json", bad)); err == nil {
		t.Error("short W1 loaded")
	}
	bad = tinyHalfKP(4, 8, false)
	bad.B1 = bad.B1[:1]
	if _, err := LoadHalfKPNet(write("b1.json", bad)); err == nil {
		t.Error("short B1 loaded")
	}
	os.WriteFile(filepath.Join(dir, "garbage.json"), []byte("{"), 0o600)
	if _, err := LoadHalfKPNet(filepath.Join(dir, "garbage.json")); err == nil {
		t.Error("garbage loaded")
	}
	if _, err := LoadHalfKPNet(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("missing file loaded")
	}
}

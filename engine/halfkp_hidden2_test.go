package engine

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

// randomHalfKP builds a network with varied weights, and a second hidden
// layer when h2 is not zero.
//
// Varied rather than constant on purpose: a transposed second-layer
// export, or a loop that reads the wrong half of the accumulator, gives
// the right answer for weights that are all the same number.
func randomHalfKP(h, h2, buckets int, seed int64) *HalfKPNet {
	rng := rand.New(rand.NewSource(seed))
	n := &HalfKPNet{H: h, H2: h2, Buckets: buckets, Scale: 1,
		W1: make([]float32, HalfKPInputsFor(buckets)*h), B1: make([]float32, h)}
	for i := range n.W1 {
		n.W1[i] = float32(rng.NormFloat64() * 0.05)
	}
	for i := range n.B1 {
		n.B1[i] = float32(rng.NormFloat64()*0.3) + 0.4
	}
	last := 2 * h
	if h2 != 0 {
		last = h2
		n.WH2 = make([]float32, 2*h*h2)
		n.BH2 = make([]float32, h2)
		for i := range n.WH2 {
			n.WH2[i] = float32(rng.NormFloat64() * 0.2)
		}
		for i := range n.BH2 {
			n.BH2[i] = float32(rng.NormFloat64() * 0.2)
		}
	}
	n.W2 = make([]float32, last)
	for i := range n.W2 {
		n.W2[i] = float32(rng.NormFloat64() * 0.5)
	}
	n.B2 = float32(rng.NormFloat64() * 0.1)
	return n
}

// referenceEvaluate is the forward pass written out plainly in float64,
// from what the struct fields claim to mean rather than from the engine's
// own loops. Two implementations that share a bug agree happily, which is
// how this project once shipped a network that trained one function and
// played another.
func referenceEvaluate(n *HalfKPNet, b *board.Board) float64 {
	h := n.H
	acc := make([]float64, 2*h)
	for side, persp := range [2]board.Color{board.White, board.Black} {
		for i := 0; i < h; i++ {
			acc[side*h+i] = float64(n.B1[i])
		}
		for _, f := range AppendHalfKPFeaturesN(nil, b, persp, n.Buckets) {
			for i := 0; i < h; i++ {
				acc[side*h+i] += float64(n.W1[int(f)*h+i])
			}
		}
	}
	clip := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	for i := range acc {
		acc[i] = clip(acc[i])
	}
	last := acc
	if n.H2 != 0 {
		mid := make([]float64, n.H2)
		for j := 0; j < n.H2; j++ {
			s := float64(n.BH2[j])
			for i := 0; i < 2*h; i++ {
				// Input-major: the weights leaving accumulator unit i sit
				// together, the same way the first layer stores a feature.
				s += float64(n.WH2[i*n.H2+j]) * acc[i]
			}
			mid[j] = clip(s)
		}
		last = mid
	}
	out := float64(n.B2)
	for i, v := range last {
		out += float64(n.W2[i]) * v
	}
	if n.Sigmoid {
		k := n.K
		if k == 0 {
			k = 0.30
		}
		return probabilityToPawns(out, k)
	}
	return out * float64(n.Scale)
}

var hidden2Positions = []string{
	"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
	"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/8/4k3/8/8/8/8/K6R w - - 0 1",
}

// A second hidden layer is the one architecture change that alters what
// the network can express: a single clipped-linear layer can only add up
// one opinion per piece, and tactics are interactions between pieces.
// Both of the engine's forward passes have to run it, the standalone one
// and the one the search drives off the incremental accumulator.
func TestSecondHiddenLayerRunsInBothForwardPasses(t *testing.T) {
	for _, shape := range []struct{ h, h2, buckets int }{
		{16, 8, 8},
		{8, 5, 32},  // a width that is not a multiple of eight, on 32 king slots
		{16, 1, 8},  // a second layer of one unit still has to be applied
		{136, 4, 8}, // wider than the stack bound: Evaluate borrows a buffer
		{520, 2, 8}, // wider than the pooled buffer too, so it has to grow
	} {
		n := randomHalfKP(shape.h, shape.h2, shape.buckets, int64(shape.h*100+shape.h2))
		for _, fen := range hidden2Positions {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			want := referenceEvaluate(n, &g.Board)
			if got := n.Evaluate(&g.Board); math.Abs(got-want) > 1e-5 {
				t.Errorf("h %d h2 %d %s: Evaluate %.8f, reference %.8f",
					shape.h, shape.h2, fen, got, want)
			}
			var slot halfKPAcc
			if got := n.EvaluateWith(&g.Board, &slot, nil, nil); math.Abs(got-want) > 1e-5 {
				t.Errorf("h %d h2 %d %s: EvaluateWith %.8f, reference %.8f",
					shape.h, shape.h2, fen, got, want)
			}
		}
	}
}

// The layer has to survive the round trip to disk, and a network that
// emits win probabilities still inverts the sigmoid at the very end.
func TestSecondHiddenLayerSurvivesSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	n := randomHalfKP(12, 6, 8, 5)
	n.Sigmoid, n.K = true, 0.4
	path := filepath.Join(dir, "net.json")
	if err := n.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := LoadHalfKPNet(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.H2 != n.H2 || len(m.WH2) != len(n.WH2) || len(m.BH2) != len(n.BH2) {
		t.Fatalf("loaded h2 %d with %d and %d weights, saved %d with %d and %d",
			m.H2, len(m.WH2), len(m.BH2), n.H2, len(n.WH2), len(n.BH2))
	}
	g := game.New()
	want := referenceEvaluate(m, &g.Board)
	if got := m.Evaluate(&g.Board); math.Abs(got-want) > 1e-5 {
		t.Errorf("after load %.8f, reference %.8f", got, want)
	}
}

// A network without a second layer must serialise exactly as it always
// did, because every file on disk was written by the old exporter and the
// new fields must not appear in it.
func TestASingleLayerNetworkKeepsItsOldJSONShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "net.json")
	if err := randomHalfKP(4, 0, 8, 1).Save(path); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"h2", "wh2", "bh2"} {
		if _, ok := raw[k]; ok {
			t.Errorf("a single-layer network wrote %q", k)
		}
	}
}

// Every shape mismatch has to be caught at load. A network whose weights
// do not match its declared shape used to load happily and evaluate every
// position as exactly zero, and that failure mode cost a whole capacity
// experiment: nothing errors and nothing logs.
func TestLoadRefusesABrokenSecondLayer(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n *HalfKPNet) string {
		p := filepath.Join(dir, name)
		data, _ := json.Marshal(n)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]func(*HalfKPNet){
		"short-wh2":   func(n *HalfKPNet) { n.WH2 = n.WH2[:3] },
		"short-bh2":   func(n *HalfKPNet) { n.BH2 = n.BH2[:1] },
		"negative-h2": func(n *HalfKPNet) { n.H2 = -4 },
		"too-wide-h2": func(n *HalfKPNet) { n.H2 = maxHalfKPHidden + 1 },
		"w2-still-2h": func(n *HalfKPNet) { n.W2 = make([]float32, 2*n.H) },
	}
	for name, breakIt := range cases {
		n := randomHalfKP(8, 4, 8, 3)
		breakIt(n)
		if _, err := LoadHalfKPNet(write(name+".json", n)); err == nil {
			t.Errorf("%s loaded", name)
		}
	}
	// And the good one still loads, so the checks are not simply rejecting
	// everything with a second layer.
	if _, err := LoadHalfKPNet(write("good.json", randomHalfKP(8, 4, 8, 3))); err != nil {
		t.Errorf("a well-formed second layer was refused: %v", err)
	}
}

// The search evaluates millions of positions per move, so one allocation
// in the forward pass would cost more than a second layer can buy. The
// second layer's activations have to stay on the stack.
func TestSecondHiddenLayerForwardPassDoesNotAllocate(t *testing.T) {
	n := randomHalfKP(64, 32, 8, 11)
	g := game.New()
	var slot halfKPAcc
	n.EvaluateWith(&g.Board, &slot, nil, nil)
	if a := testing.AllocsPerRun(50, func() { _ = n.output(&slot) }); a != 0 {
		t.Errorf("%v allocations per evaluation with a second layer", a)
	}
}

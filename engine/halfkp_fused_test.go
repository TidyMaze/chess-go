package engine

import (
	"math"
	"math/rand"
	"testing"

	"chess/board"
	"chess/game"
)

// The fused update reads the parent and writes the child in one pass instead
// of a copy plus one pass per changed piece. Each element must still see the
// same operations in the same order, so the result is identical to the bit,
// not to a tolerance: golden evaluations and node counts depend on it.
func TestFusedDeltaIsBitIdenticalToCopyThenApply(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	h, buckets := net.H, net.buckets()
	rng := rand.New(rand.NewSource(11))
	compared := 0
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var parent, child halfKPAcc
		net.refresh(&g.Board, &parent, nil, nil)
		color := g.Turn
		for ply := 0; ply < 16; ply++ {
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, legal[rng.Intn(len(legal))])
			color = color.Other()
			net.refresh(&g.Board, &child, nil, nil) // snapshot of pieces and kings
			for side, persp := range [2]board.Color{board.White, board.Black} {
				if perspectiveKingSlot(parent.kings[side], persp, buckets) != perspectiveKingSlot(child.kings[side], persp, buckets) {
					continue
				}
				want := make([]float32, h)
				copy(want, parent.acc[side][:h])
				net.applyDelta(want, &parent, &child, persp, buckets)
				got := make([]float32, h)
				if !net.fusedDelta(got, parent.acc[side][:h], &parent, &child, persp, buckets) {
					continue
				}
				for i := range want {
					if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
						t.Fatalf("%s ply %d unit %d: fused %v, copy then apply %v", fen, ply, i, got[i], want[i])
					}
				}
				compared++
			}
			parent = child
		}
	}
	if compared == 0 {
		t.Fatal("no incremental update was compared")
	}
}

// A slot whose parent is several moves away changes more pieces than one
// batched update takes. refresh must fall back to copy then applyDelta,
// and still land on those sums to the bit.
func TestRefreshFromADistantParentFallsBackToCopyThenApply(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	h := net.H
	// Same king squares, five pawns apart: no perspective is rebuilt in
	// full, and the diff has five entries.
	from, err := game.ParseFEN("4k3/pppppppp/8/8/8/8/PPPPPPPP/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	to, err := game.ParseFEN("4k3/ppp5/8/8/8/8/PPPPPPPP/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	var parent, child halfKPAcc
	net.refresh(&from.Board, &parent, nil, nil)
	var st halfKPAccStats
	net.refresh(&to.Board, &child, &parent, &st)
	if st.incremental != 2 || st.full != 0 {
		t.Fatalf("want both perspectives derived from the parent, got %+v", st)
	}
	var d accDiff
	if diffAcc(&parent, &child, &d) {
		t.Fatal("five changed pawns fit in one batched update")
	}
	if net.fusedDelta(make([]float32, h), parent.acc[0][:h], &parent, &child, board.White, net.buckets()) {
		t.Fatal("fusedDelta batched five changed pawns")
	}
	for side, persp := range [2]board.Color{board.White, board.Black} {
		want := append([]float32(nil), parent.acc[side][:h]...)
		net.applyDelta(want, &parent, &child, persp, net.buckets())
		for i := range want {
			if math.Float32bits(child.acc[side][i]) != math.Float32bits(want[i]) {
				t.Fatalf("side %d unit %d: refresh %v, copy then apply %v", side, i, child.acc[side][i], want[i])
			}
		}
	}
}

// A full rebuild, with no parent to derive from, must be the bias plus
// every feature's row added one at a time in the feature list's order, to
// the bit, however the rows are batched.
func TestFullRefreshIsBitIdenticalToBiasPlusEachRow(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	h, buckets := net.H, net.buckets()
	rng := rand.New(rand.NewSource(5))
	var st halfKPAccStats
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		color := g.Turn
		for ply := 0; ply < 24; ply++ {
			var acc halfKPAcc
			net.refresh(&g.Board, &acc, nil, &st)
			for side, persp := range [2]board.Color{board.White, board.Black} {
				want := append([]float32(nil), net.B1...)
				for _, f := range AppendHalfKPFeaturesN(nil, &g.Board, persp, buckets) {
					net.addRow(want, f, 1)
				}
				for i := 0; i < h; i++ {
					if math.Float32bits(acc.acc[side][i]) != math.Float32bits(want[i]) {
						t.Fatalf("%s ply %d side %d unit %d: refresh %v, bias plus each row %v", fen, ply, side, i, acc.acc[side][i], want[i])
					}
				}
			}
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, legal[rng.Intn(len(legal))])
			color = color.Other()
		}
	}
	if st.full == 0 {
		t.Fatal("no full refresh was compared")
	}
}

// refresh works out which pieces changed once per node and applies that
// list to both perspectives. Each perspective it derives incrementally must
// still be the parent's accumulator plus applyDelta's rows, to the bit, over
// random lines long enough to include captures, castling and promotions.
func TestRefreshIsBitIdenticalToCopyThenApplyPerPerspective(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	h, buckets := net.H, net.buckets()
	rng := rand.New(rand.NewSource(23))
	var st halfKPAccStats
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var parent, child halfKPAcc
		net.refresh(&g.Board, &parent, nil, nil)
		color := g.Turn
		for ply := 0; ply < 40; ply++ {
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, legal[rng.Intn(len(legal))])
			color = color.Other()
			net.refresh(&g.Board, &child, &parent, &st)
			for side, persp := range [2]board.Color{board.White, board.Black} {
				if perspectiveKingSlot(parent.kings[side], persp, buckets) != perspectiveKingSlot(child.kings[side], persp, buckets) {
					continue
				}
				want := make([]float32, h)
				copy(want, parent.acc[side][:h])
				net.applyDelta(want, &parent, &child, persp, buckets)
				for i := range want {
					if math.Float32bits(child.acc[side][i]) != math.Float32bits(want[i]) {
						t.Fatalf("%s ply %d side %d unit %d: refresh %v, copy then apply %v", fen, ply, side, i, child.acc[side][i], want[i])
					}
				}
			}
			parent = child
		}
	}
	if st.incremental == 0 {
		t.Fatal("no incremental update was compared")
	}
}

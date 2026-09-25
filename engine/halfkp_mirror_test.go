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

// withMirror round-trips a network through its JSON file with "mirror" set,
// so the tests exercise the field a trainer writes, not a Go-only switch.
func withMirror(t *testing.T, n *HalfKPNet) *HalfKPNet {
	t.Helper()
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["mirror"] = true
	if data, err = json.Marshal(fields); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mirrored.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadHalfKPNet(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// With the king on g1 the board is flipped left-right, so a pawn on h2 is
// read as the pawn on a2 a king on b1 would see.
func TestMirroredNetReadsAKingsidePawnAsItsQueensideTwin(t *testing.T) {
	const a2 = 1*halfKPPerKing + 0*64 + 1*8 + 0 // bucket of g1, own pawn, a2
	w1 := make([]float32, HalfKPInputsFor(halfKPKingBuckets))
	w1[a2] = 0.5
	net := withMirror(t, &HalfKPNet{H: 1, W1: w1, B1: []float32{0}, W2: []float32{1, 0}, Scale: 1, Buckets: 8})
	g, err := game.ParseFEN("k7/8/8/8/8/8/7P/6K1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	if got := net.Evaluate(&g.Board); got != 0.5 {
		t.Errorf("full evaluation %v, want 0.5: the h2 pawn did not land on the a2 feature", got)
	}
	var acc halfKPAcc
	if got := net.EvaluateWith(&g.Board, &acc, nil, nil); got != 0.5 {
		t.Errorf("accumulator evaluation %v, want 0.5: the h2 pawn did not land on the a2 feature", got)
	}
}

// rebucket derives a coarser scheme's feature from a 64-slot one, the way a
// pool generated with -king-buckets 64 is re-bucketed offline.
func rebucket(f int32, buckets int, mirror bool) int32 {
	slot, kind, sq := int(f)/halfKPPerKing, int(f)%halfKPPerKing/64, int(f)%64
	kr, kf := slot/8, slot%8
	fold := kf
	if kf > 3 {
		fold = 7 - kf
		if mirror {
			sq = sq/8*8 + 7 - sq%8
		}
	}
	slot = kr*4 + fold
	if buckets == halfKPKingBuckets {
		slot = kr/4*4 + fold
	}
	return int32(slot*halfKPPerKing + kind*64 + sq)
}

// The engine's features for 8 and 32 king slots, mirrored or not, must be
// exactly what re-bucketing a 64-slot pool produces, or a network trained on
// the re-bucketed pool plays a different function from the one it learned.
func TestFeaturesMatchTheSchemesDerivedFromRawKingSlots(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	checked := 0
	for _, fen := range append(append([]string{}, mirrorWalkStarts...), correctnessPositions...) {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		color := g.Turn
		for ply := 0; ply < 30; ply++ {
			for _, persp := range [2]board.Color{board.White, board.Black} {
				raw := AppendHalfKPFeaturesN(nil, &g.Board, persp, halfKPRawKingSquares, false)
				for _, buckets := range []int{halfKPKingBuckets, halfKPKingSquares} {
					for _, mirror := range []bool{false, true} {
						got := AppendHalfKPFeaturesN(nil, &g.Board, persp, buckets, mirror)
						for i, f := range raw {
							if want := rebucket(f, buckets, mirror); got[i] != want {
								t.Fatalf("%s, %v perspective, %d buckets, mirror %v: feature %d is %d, the 64-slot re-bucketing gives %d",
									g.FEN(), persp, buckets, mirror, i, got[i], want)
							}
						}
						checked++
					}
				}
			}
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, pickKingHeavy(&g.Board, legal, rng))
			color = color.Other()
		}
	}
	t.Logf("%d feature lists checked against the 64-slot re-bucketing", checked)
}

// mirrorWalkStarts put both kings on e1/e8 or d1/d8, with castling rights on
// both wings where the rooks allow it.
var mirrorWalkStarts = []string{
	"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
	"r3k2r/pppq1ppp/2np1n2/2b1p3/2B1P3/2NP1N2/PPPQ1PPP/R3K2R w KQkq - 0 1",
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"r2k3r/pp3ppp/8/8/8/8/PP3PPP/R2K3R w - - 0 1",
	"4k3/pppppppp/8/8/8/8/PPPPPPPP/4K3 w - - 0 1",
}

// On a mirrored net the incremental accumulator must equal a full evaluation
// while the kings castle and cross the d/e boundary, where the 8-bucket slot
// stays the same but the whole board flips.
func TestMirroredIncrementalAccumulatorMatchesFullEvaluation(t *testing.T) {
	plain, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	net := withMirror(t, plain)
	castled, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Evaluate(&castled.Board) == net.Evaluate(&castled.Board) {
		t.Fatal("mirroring changed nothing with both kings on g1/g8")
	}

	const plies = 40
	worst := 0.0
	var stats halfKPAccStats
	var shortCastles, longCastles, centreCrossings int
	for i, fen := range mirrorWalkStarts {
		for walk := 0; walk < 25; walk++ {
			rng := rand.New(rand.NewSource(int64(1000*i + walk)))
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			var stack [plies + 1]halfKPAcc
			net.EvaluateWith(&g.Board, &stack[0], nil, &stats)
			color := g.Turn
			for ply := 1; ply <= plies; ply++ {
				legal := g.AllLegalMoves(color)
				if len(legal) == 0 {
					break
				}
				m := pickKingHeavy(&g.Board, legal, rng)
				if p, _ := g.Board.PieceAt(m.From); p.Type == board.King {
					switch {
					case m.From.File == 4 && m.To.File == 6:
						shortCastles++
					case m.From.File == 4 && m.To.File == 2:
						longCastles++
					case (m.From.File == 3 && m.To.File == 4) || (m.From.File == 4 && m.To.File == 3):
						centreCrossings++
					}
				}
				makeSearchMove(g, m)
				color = color.Other()
				full := net.Evaluate(&g.Board)
				inc := net.EvaluateWith(&g.Board, &stack[ply], &stack[ply-1], &stats)
				if d := math.Abs(full - inc); d > worst {
					worst = d
				}
			}
		}
	}
	t.Logf("worst %.2e pawns; %d short and %d long castles, %d d/e king crossings; %d incremental, %d full",
		worst, shortCastles, longCastles, centreCrossings, stats.incremental, stats.full)
	if worst > 1e-4 {
		t.Errorf("incremental accumulator disagrees with the full evaluation by %.2e pawns", worst)
	}
	if shortCastles == 0 || longCastles == 0 || centreCrossings == 0 {
		t.Error("the walks must castle on both wings and cross the d/e boundary")
	}
	if stats.incremental == 0 {
		t.Error("no incremental update happened: every node was recomputed in full")
	}
}

// pickKingHeavy plays a king move half the time when one exists, so random
// walks actually move the king across the boundary instead of rarely.
func pickKingHeavy(b *board.Board, legal []game.Move, rng *rand.Rand) game.Move {
	var king []game.Move
	for _, m := range legal {
		if p, _ := b.PieceAt(m.From); p.Type == board.King {
			king = append(king, m)
		}
	}
	if len(king) > 0 && rng.Intn(2) == 0 {
		return king[rng.Intn(len(king))]
	}
	return legal[rng.Intn(len(legal))]
}

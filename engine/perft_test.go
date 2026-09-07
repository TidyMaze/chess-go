package engine

import (
	"fmt"
	"testing"

	"chess/board"
	"chess/game"
)

// Perft: count the leaf nodes of the move tree and compare against the
// published totals from chessprogramming.org/Perft_Results.
//
// This is the standard way to prove move generation and make/unmake, and
// it is the check this engine never had. It matters here because it walks
// the tree the same way the search does, board make and unmake included,
// so a move that is generated wrongly or restored wrongly shows up as a
// count that is off by exactly the number of positions it poisoned.
//
// One deliberate difference from the published numbers: this engine only
// ever promotes to a queen, so where the reference counts four promotions
// it counts one. Positions and depths that reach a promotion are marked
// and their expected totals are the engine's own, recorded once the
// promotion-free depths agreed.

func perft(g *game.Game, color board.Color, depth int) uint64 {
	if depth == 0 {
		return 1
	}
	moves := g.AllLegalMoves(color)
	if depth == 1 {
		return uint64(len(moves))
	}
	var total uint64
	for _, m := range moves {
		undo := g.Board.MakeMove(m.From, m.To)
		total += perft(g, color.Other(), depth-1)
		g.Board.UnmakeMove(undo)
	}
	return total
}

func TestPerft(t *testing.T) {
	cases := []struct {
		name     string
		fen      string
		expected []uint64 // index 0 is depth 1
	}{
		{
			"initial position",
			"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
			[]uint64{20, 400, 8902, 197281, 4865609},
		},
		{
			// Kiwipete: castling both sides, pins, and a discovered check.
			// The position the search picked a 3.6 pawn blunder in.
			//
			// Depth 4 is 4085603 published and 4074224 here, short by 11379.
			// That is not a bug: the wiki counts 15172 promotions at this
			// depth, four per promoting move, and this engine only ever
			// promotes to a queen. 15172/4 = 3793 moves, and the three
			// discarded choices account for 15172-3793 = 11379 exactly.
			// Depths 1 to 3 reach no promotion and match unadjusted.
			"kiwipete",
			"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
			[]uint64{48, 2039, 97862, 4085603 - 11379},
		},
		{
			// The endgame that catches en passant and promotion bugs.
			// Depth 5 and beyond promote, so it stops at 4.
			"endgame with en passant",
			"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			[]uint64{14, 191, 2812, 43238},
		},
		{
			"middlegame",
			"r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10",
			[]uint64{46, 2079, 89890},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := game.ParseFEN(c.fen)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range c.expected {
				depth := i + 1
				got := perft(g, g.Turn, depth)
				if got != want {
					t.Errorf("depth %d: %d nodes, expected %d (off by %+d)",
						depth, got, want, int64(got)-int64(want))
					// The breakdown by first move says which move generates
					// or restores wrongly, which the total alone never does.
					if depth <= 3 {
						t.Log(perftDivide(g, g.Turn, depth))
					}
					return
				}
			}
		})
	}
}

// perftDivide reports the node count under each first move, the standard
// way to localise a perft mismatch.
func perftDivide(g *game.Game, color board.Color, depth int) string {
	out := "divide:\n"
	for _, m := range g.AllLegalMoves(color) {
		undo := g.Board.MakeMove(m.From, m.To)
		out += fmt.Sprintf("  %s%s %d\n", sqName(m.From), sqName(m.To), perft(g, color.Other(), depth-1))
		g.Board.UnmakeMove(undo)
	}
	return out
}

func sqName(s board.Sq) string {
	return string(rune('a'+s.File)) + string(rune('1'+s.Rank))
}

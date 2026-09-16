package engine

import (
	"testing"

	"chess/board"
	"chess/game"
	"chess/moves"
)

// kingSafetyPenalty counts attackers with target bitboards and nothing
// else, which holds only while every weighted attacker is a piece whose
// targets are a function of occupancy alone. Give pawns or kings a
// weight and this test says where to look.
func TestKingAttackersAreAllTableBacked(t *testing.T) {
	b := board.Initial()
	for pt, w := range kingAttackerWeight {
		if w == 0 {
			continue
		}
		if _, ok := b.TargetBitboard(board.Sq{File: 3, Rank: 3}, board.White, board.PieceType(pt)); !ok {
			t.Errorf("%v carries king-attacker weight %v but has no target bitboard", board.PieceType(pt), w)
		}
	}
}

// Mobility weights come from a fit and may weight pawns, whose targets
// depend on more than occupancy, so pawns keep the generated move list
// and must count the same squares the list has.
func TestPawnMobilityFallsBackToTheWalk(t *testing.T) {
	g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	if err != nil {
		t.Fatal(err)
	}
	weights := [6]float64{board.Pawn: 1}
	var buf [32]board.ColoredPiece
	pieces := g.Board.AppendAllPieces(buf[:0])
	for _, c := range []board.Color{board.White, board.Black} {
		want := 0.0
		var tb [28]board.Sq
		for _, p := range pieces {
			if p.Color == c && p.Type == board.Pawn {
				want += float64(len(moves.AppendLegalTargets(tb[:0], &g.Board, p.Sq, c, board.Pawn)))
			}
		}
		if got := mobilityScore(&g.Board, pieces, c, &weights); got != want {
			t.Errorf("%v pawn mobility %v, want %v from the walk", c, got, want)
		}
		if want == 0 {
			t.Errorf("%v has no pawn moves in this position, the test exercises nothing", c)
		}
	}
}

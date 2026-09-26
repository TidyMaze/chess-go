package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Lichess UAuzblWw, move 73: 73.Rg7+ Kh5 74.Kh8 traps the lone queen and wins
// (+9.9 for Stockfish), while b6 lets it check forever. A null move assumes
// passing is never better than moving, which is false for a lone piece, and
// that is what hid the win.
func TestNullPiecesFindsTheQueenTrap(t *testing.T) {
	c := ReadChampion("../champion.json")
	c.NetFile, c.Book = "../champion_net.json", ""
	c.Features += ",nullpieces"
	p, err := c.PlayerOrError()
	if err != nil {
		t.Skip(err)
	}
	// Depth 14: with the SPSA-tuned margins the trap shows from depth 14 (at 13
	// it plays b6); without nullpieces it still plays b6 at depth 14.
	p.TimeBudget, p.Threads, p.Depth = 0, 1, 14
	g, err := game.ParseFEN("6K1/1R3R2/8/1P2q3/6k1/8/8/8 w - - 5 73")
	if err != nil {
		t.Fatal(err)
	}
	SeedRandom(1)
	m, _ := PlayerPick(p, g)
	want := game.Move{From: board.Sq{File: 5, Rank: 6}, To: board.Sq{File: 6, Rank: 6}}
	if m.From != want.From || m.To != want.To {
		t.Errorf("played %v->%v, want Rg7+ (f7->g7)", m.From, m.To)
	}
}

func TestNullPiecesCountsNonPawnPieces(t *testing.T) {
	g, _ := game.ParseFEN("6K1/1R3R2/8/1P2q3/6k1/8/8/8 w - - 5 73")
	if !nullAllowedByMaterial(&g.Board, board.White) {
		t.Error("two rooks are enough material for a null move")
	}
	if nullAllowedByMaterial(&g.Board, board.Black) {
		t.Error("a lone queen must not null-move")
	}
}

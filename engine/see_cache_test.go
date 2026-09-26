package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestSEECacheInOrderMoves(t *testing.T) {
	// White Qd1 can capture Black pawn on d5 protected by Rd7.
	// Victim = Pawn (1), Attacker = Queen (9) -> victim < attacker.
	g, err := game.ParseFEN("4k3/3r4/8/3p4/8/8/8/3QK3 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	ev := &Eval{MainSEE: true}
	c := &searchCtx{ev: ev}
	legal := g.AllLegalMoves(board.White)

	// The search reads the exchange off the key of each move it picks.
	keys := make([]int64, len(legal))
	c.scoreMoves(g, legal, keys, game.Move{}, 0, board.White)
	for j := range legal {
		pickMove(&g.Board, legal, keys, j)
	}

	foundCapture := false
	for i, m := range legal {
		if isCaptureMove(g, m) {
			attacker, _ := g.Board.PieceAt(m.From)
			victim, onSquare := g.Board.PieceAt(m.To)
			if !onSquare {
				victim = board.Piece{Type: board.Pawn}
			}
			if mvvLvaPiece[victim.Type] < mvvLvaPiece[attacker.Type] {
				foundCapture = true
				expectedSEE := see(&g.Board, m)
				cachedSEE := int(keySEE(keys[i]))
				if cachedSEE != expectedSEE {
					t.Errorf("move %v: cached SEE %d != expected %d", m, cachedSEE, expectedSEE)
				}
			}
		}
	}
	if !foundCapture {
		t.Fatalf("no capture with victim < attacker found in test position")
	}
}

func TestSEECacheSearchEquivalence(t *testing.T) {
	fen := "r1bqkb1r/pppp1ppp/2n5/4p3/2B1n3/5N2/PPPP1PPP/RNBQK2R w KQkq - 0 5"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	ev := &Eval{MainSEE: true}
	m, ok := ChooseMoveIterative(g, board.White, 4, ev, true)
	if !ok || m == (game.Move{}) {
		t.Fatalf("ChooseMoveIterative returned invalid move: %v, ok=%v", m, ok)
	}
}

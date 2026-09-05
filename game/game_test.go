package game

import (
	"chess/board"
	"testing"
)

func TestNewGameWhiteToMove(t *testing.T) {
	g := New()
	if g.Turn != board.White {
		t.Errorf("expected white to move first")
	}
}

func TestApplyMoveSwitchesTurn(t *testing.T) {
	g := New()
	g.ApplyMove(board.Sq{4, 1}, board.Sq{4, 3})
	if g.Turn != board.Black {
		t.Errorf("turn did not switch")
	}
	if p, ok := g.Board.PieceAt(board.Sq{4, 3}); !ok || p.Type != board.Pawn {
		t.Errorf("pawn did not move")
	}
}

func TestAllLegalMovesTwentyAtStart(t *testing.T) {
	g := New()
	moves := g.AllLegalMoves(board.White)
	if len(moves) != 20 {
		t.Errorf("expected 20 legal moves at start, got %d", len(moves))
	}
}

func TestIllegalMoveExposingCheckFiltered(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{4, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{4, 3}, board.Piece{board.White, board.Pawn})
	b.Place(board.Sq{4, 7}, board.Piece{board.Black, board.Rook})
	b.Place(board.Sq{0, 0}, board.Piece{board.Black, board.King})
	g := From(b, board.White)
	moves := g.AllLegalMoves(board.White)
	for _, m := range moves {
		if m.From == (board.Sq{4, 3}) && m.To == (board.Sq{3, 4}) {
			t.Errorf("pinned pawn's illegal diagonal move should be filtered")
		}
	}
	found := false
	for _, m := range moves {
		if m.From == (board.Sq{4, 3}) && m.To == (board.Sq{4, 4}) {
			found = true
		}
	}
	if !found {
		t.Errorf("pinned pawn's legal forward move should remain")
	}
}

func TestBackRankCheckmate(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{7, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{6, 1}, board.Piece{board.White, board.Pawn})
	b.Place(board.Sq{7, 1}, board.Piece{board.White, board.Pawn})
	b.Place(board.Sq{0, 0}, board.Piece{board.Black, board.Rook})
	b.Place(board.Sq{0, 7}, board.Piece{board.Black, board.King})
	g := From(b, board.White)
	if !g.IsCheckmate(board.White) {
		t.Errorf("expected checkmate")
	}
}

func TestStalemate(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{1, 2}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{2, 1}, board.Piece{board.Black, board.Queen})
	g := From(b, board.White)
	if !g.IsStalemate(board.White) {
		t.Errorf("expected stalemate")
	}
	if g.IsCheckmate(board.White) {
		t.Errorf("stalemate is not checkmate")
	}
}

func TestThreefoldRepetition(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	g := From(b, board.White)
	g.EnableRepetitionTracking()
	type mv struct{ from, to board.Sq }
	moveSeq := []mv{
		{board.Sq{0, 0}, board.Sq{1, 0}}, {board.Sq{7, 7}, board.Sq{6, 7}},
		{board.Sq{1, 0}, board.Sq{0, 0}}, {board.Sq{6, 7}, board.Sq{7, 7}}, // back to start: seen twice
		{board.Sq{0, 0}, board.Sq{1, 0}}, {board.Sq{7, 7}, board.Sq{6, 7}},
		{board.Sq{1, 0}, board.Sq{0, 0}}, {board.Sq{6, 7}, board.Sq{7, 7}}, // seen three times
	}
	for i := 0; i < len(moveSeq)-1; i++ {
		g.ApplyMove(moveSeq[i].from, moveSeq[i].to)
		if g.IsThreefoldRepetition() {
			t.Fatalf("should not be threefold yet, after move %d", i)
		}
	}
	last := moveSeq[len(moveSeq)-1]
	g.ApplyMove(last.from, last.to)
	if !g.IsThreefoldRepetition() {
		t.Errorf("expected threefold repetition")
	}
}

func TestFiftyMoveRule(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	g := From(b, board.White)
	for i := 0; i < 24; i++ {
		g.ApplyMove(board.Sq{0, 0}, board.Sq{1, 0})
		g.ApplyMove(board.Sq{7, 7}, board.Sq{6, 7})
		g.ApplyMove(board.Sq{1, 0}, board.Sq{0, 0})
		g.ApplyMove(board.Sq{6, 7}, board.Sq{7, 7})
	}
	if g.IsFiftyMoveDraw() {
		t.Fatalf("should not be a fifty-move draw yet (96 halfmoves)")
	}
	g.ApplyMove(board.Sq{0, 0}, board.Sq{1, 0})
	g.ApplyMove(board.Sq{7, 7}, board.Sq{6, 7})
	g.ApplyMove(board.Sq{1, 0}, board.Sq{0, 0})
	g.ApplyMove(board.Sq{6, 7}, board.Sq{7, 7})
	if !g.IsFiftyMoveDraw() {
		t.Errorf("expected fifty-move draw at 100 halfmoves")
	}
}

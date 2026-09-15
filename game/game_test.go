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

func TestPawnPromotesToQueenOnLastRank(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{3, 6}, board.Piece{board.White, board.Pawn})
	g := From(b, board.White)
	g.ApplyMove(board.Sq{3, 6}, board.Sq{3, 7})
	p, ok := g.Board.PieceAt(board.Sq{3, 7})
	if !ok || p.Type != board.Queen || p.Color != board.White {
		t.Errorf("expected a white queen on promotion square, got %+v ok=%v", p, ok)
	}
}

func TestBlackPawnPromotesOnRankZero(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{3, 1}, board.Piece{board.Black, board.Pawn})
	g := From(b, board.Black)
	g.ApplyMove(board.Sq{3, 1}, board.Sq{3, 0})
	p, ok := g.Board.PieceAt(board.Sq{3, 0})
	if !ok || p.Type != board.Queen || p.Color != board.Black {
		t.Errorf("expected a black queen on promotion square, got %+v ok=%v", p, ok)
	}
}

// A pawn on the last rank must not generate a move off the board. The
// padded-array board makes an off-board square addressable rather than a
// crash at the point of the bad move, so it corrupts silently and blows
// up later, several plies deep, when a knight/king probe from there runs
// past the end of the array.
func TestPawnOnLastRankGeneratesNoForwardMove(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{3, 7}, board.Piece{board.White, board.Pawn})
	g := From(b, board.White)
	for _, m := range g.AllLegalMoves(board.White) {
		if m.From == (board.Sq{3, 7}) {
			t.Errorf("pawn on last rank should have no moves, got %+v", m)
		}
	}
}

func TestAppendLegalMovesGivenCheckMatches(t *testing.T) {
	g := New()
	var buf1, buf2 [96]Move
	m1, inCheck := g.AppendLegalMovesInCheck(buf1[:0], board.White)
	m2 := g.AppendLegalMovesGivenCheck(buf2[:0], board.White, inCheck)
	if len(m1) != len(m2) {
		t.Fatalf("length mismatch: %d vs %d", len(m1), len(m2))
	}
	for i := range m1 {
		if m1[i] != m2[i] {
			t.Errorf("move %d mismatch: %v vs %v", i, m1[i], m2[i])
		}
	}
}

func BenchmarkAppendLegalMoves(b *testing.B) {
	g := New()
	var buf [96]Move
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.AppendLegalMoves(buf[:0], board.White)
	}
}

func BenchmarkAppendQuiescenceMoves(b *testing.B) {
	g := New()
	var buf [96]Move
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = g.AppendQuiescenceMoves(buf[:0], board.White)
	}
}

func TestIsLegalMoveMatchesAllLegalMoves(t *testing.T) {
	testPositions := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
	}
	for _, fen := range testPositions {
		g, err := ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		legal := g.AllLegalMoves(g.Turn)
		legalMap := make(map[Move]bool)
		for _, m := range legal {
			legalMap[m] = true
			if !g.IsLegalMove(m) {
				t.Errorf("expected move %v to be legal in FEN %s", m, fen)
			}
		}
		// Test illegal moves: moving from empty squares or friendly piece destinations
		for fromRank := 0; fromRank < 8; fromRank++ {
			for fromFile := 0; fromFile < 8; fromFile++ {
				for toRank := 0; toRank < 8; toRank++ {
					for toFile := 0; toFile < 8; toFile++ {
						m := Move{From: board.Sq{File: fromFile, Rank: fromRank}, To: board.Sq{File: toFile, Rank: toRank}}
						if !legalMap[m] && g.IsLegalMove(m) {
							t.Errorf("expected move %v to be illegal in FEN %s", m, fen)
						}
					}
				}
			}
		}
	}
}

func BenchmarkIsLegalMove(b *testing.B) {
	g := New()
	m := Move{From: board.Sq{File: 4, Rank: 1}, To: board.Sq{File: 4, Rank: 3}} // e2-e4
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.IsLegalMove(m)
	}
}

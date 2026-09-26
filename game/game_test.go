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
		for fromRank := int8(0); fromRank < 8; fromRank++ {
			for fromFile := int8(0); fromFile < 8; fromFile++ {
				for toRank := int8(0); toRank < 8; toRank++ {
					for toFile := int8(0); toFile < 8; toFile++ {
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

// The search plays its moves on the board and never updates g.Turn, so it
// needs a legality test told whose move it is. IsLegalMove answers for
// g.Turn only, and turned every black move down at a node where white was
// the root's side.
func TestIsLegalMoveForIgnoresTurn(t *testing.T) {
	g := New()
	e7e5 := Move{From: board.Sq{File: 4, Rank: 6}, To: board.Sq{File: 4, Rank: 4}}
	if g.IsLegalMove(e7e5) {
		t.Fatal("setup: e7-e5 should be illegal for the side to move, white")
	}
	if !g.IsLegalMoveFor(e7e5, board.Black) {
		t.Error("e7-e5 is legal for black, though white is to move")
	}
	e2e4 := Move{From: board.Sq{File: 4, Rank: 1}, To: board.Sq{File: 4, Rank: 3}}
	if g.IsLegalMoveFor(e2e4, board.Black) {
		t.Error("e2-e4 moves a white pawn, not legal for black")
	}
}

func TestIsLegalMoveForMatchesIsLegalMoveWithThatTurn(t *testing.T) {
	testPositions := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"r1bqk1nr/pp1p1pb1/2nQp1pp/8/2B1P3/2N2N2/PP3PPP/R1B1K2R b KQkq - 0 1",
	}
	for _, fen := range testPositions {
		for _, c := range []board.Color{board.White, board.Black} {
			g, err := ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			ref := From(g.Board, c)
			g.Turn = c.Other()
			legal := 0
			for from := 0; from < 64; from++ {
				for to := 0; to < 64; to++ {
					m := Move{From: board.Sq{File: int8(from % 8), Rank: int8(from / 8)}, To: board.Sq{File: int8(to % 8), Rank: int8(to / 8)}}
					want := ref.IsLegalMove(m)
					if got := g.IsLegalMoveFor(m, c); got != want {
						t.Errorf("%s, %v to move, turn %v: IsLegalMoveFor(%v) = %v, want %v", fen, c, g.Turn, m, got, want)
					}
					if want {
						legal++
					}
				}
			}
			if legal == 0 {
				t.Errorf("%s: no legal move for %v, the comparison proved nothing", fen, c)
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

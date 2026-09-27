package game

import (
	"math/rand"
	"testing"

	"chess/board"
	"chess/moves"
)

// randomPosition scatters men weighted towards sliders around two kings, so
// that pins, checks, double checks and pinned men in check all turn up far
// more often than in random games. Pawns stay off the back ranks, the side
// that just moved is never left in check, castling rights are granted
// wherever king and rook stand at home, and an en passant square is set
// whenever the side that just moved could have made a double push.
func randomPosition(rng *rand.Rand) *Game {
	for {
		b := board.NewEmpty()
		wk := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
		bk := wk
		for bk == wk {
			bk = board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
		}
		b.Place(wk, board.Piece{Color: board.White, Type: board.King})
		b.Place(bk, board.Piece{Color: board.Black, Type: board.King})
		types := []board.PieceType{board.Pawn, board.Pawn, board.Knight, board.Bishop, board.Bishop, board.Rook, board.Rook, board.Queen}
		for n := 4 + rng.Intn(20); n > 0; n-- {
			sq := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
			if sq == wk || sq == bk {
				continue
			}
			pt := types[rng.Intn(len(types))]
			if pt == board.Pawn && (sq.Rank == 0 || sq.Rank == 7) {
				continue
			}
			b.Place(sq, board.Piece{Color: board.Color(rng.Intn(2)), Type: pt})
		}
		turn := board.Color(rng.Intn(2))
		home := func(c board.Color, file, rank int8, pt board.PieceType) bool {
			p, ok := b.PieceAt(board.Sq{File: file, Rank: rank})
			return ok && p.Color == c && p.Type == pt
		}
		// The pawn that just moved stands on its fourth rank with the two
		// squares it crossed empty. Half the time a pawn of the side to
		// move is put beside it, or en passant would hardly ever be legal.
		pushed, skipped, start := int8(4), int8(5), int8(6)
		if turn == board.Black {
			pushed, skipped, start = 3, 2, 1
		}
		for _, f := range rng.Perm(8) {
			if !home(turn.Other(), int8(f), pushed, board.Pawn) {
				continue
			}
			_, s1 := b.PieceAt(board.Sq{File: int8(f), Rank: skipped})
			_, s2 := b.PieceAt(board.Sq{File: int8(f), Rank: start})
			if s1 || s2 {
				continue
			}
			b.SetEPSquare(board.Sq{File: int8(f), Rank: skipped}, true)
			side := board.Sq{File: int8(f + 2*rng.Intn(2) - 1), Rank: pushed}
			if _, taken := b.PieceAt(side); rng.Intn(2) == 0 && side.File >= 0 && side.File < 8 && !taken {
				b.Place(side, board.Piece{Color: turn, Type: board.Pawn})
			}
			break
		}
		if b.IsInCheck(turn.Other()) {
			continue
		}
		var rights uint8
		if home(board.White, 4, 0, board.King) {
			if home(board.White, 7, 0, board.Rook) {
				rights |= board.WhiteKingSide
			}
			if home(board.White, 0, 0, board.Rook) {
				rights |= board.WhiteQueenSide
			}
		}
		if home(board.Black, 4, 7, board.King) {
			if home(board.Black, 7, 7, board.Rook) {
				rights |= board.BlackKingSide
			}
			if home(board.Black, 0, 7, board.Rook) {
				rights |= board.BlackQueenSide
			}
		}
		b.SetCastle(rights)
		return From(b, turn)
	}
}

// Over positions dense in pins and checks, every legality shortcut has to
// agree with playing the move: IsLegal and Screen through checkPseudoLegal,
// the quiescence list through compareQuiescence, and HasAnyLegalMove.
func TestFastLegalityMatchesMakeUnmakeOnRandomPositions(t *testing.T) {
	rng := rand.New(rand.NewSource(31))
	var pinnedMoves, kingMoves, epMoves, inCheck, mates int
	for trial := 0; trial < 40000; trial++ {
		g := randomPosition(rng)
		color := g.Turn
		where := g.FEN()
		checkPseudoLegal(t, g, where)
		compareQuiescence(t, g, where, 0)
		check := moves.IsInCheck(&g.Board, color)
		pseudo, _ := g.AppendPseudoLegalMoves(nil, color, check)
		pinned := moves.PinnedSquares(&g.Board, color)
		ep, hasEP := g.Board.EPSquare()
		want := false
		for _, m := range pseudo {
			if legalByMakeUnmake(g, color, m) {
				want = true
			}
			p, _ := g.Board.PieceAt(m.From)
			switch {
			case p.Type == board.King:
				kingMoves++
			case pinned.Has(m.From):
				pinnedMoves++
			}
			if hasEP && p.Type == board.Pawn && m.To == ep && m.From.File != m.To.File {
				epMoves++
			}
		}
		if got := g.HasAnyLegalMoveInCheck(color, check); got != want {
			t.Fatalf("HasAnyLegalMoveInCheck = %v, make/unmake says %v\n%s", got, want, where)
		}
		if got := g.HasAnyLegalMove(color); got != want {
			t.Fatalf("HasAnyLegalMove = %v, make/unmake says %v\n%s", got, want, where)
		}
		if check {
			inCheck++
			if !want {
				mates++
			}
		}
	}
	if pinnedMoves < 10000 || kingMoves < 10000 || epMoves < 500 || inCheck < 2000 || mates < 50 {
		t.Fatalf("too few cases: %d pinned-man moves, %d king moves, %d en passant, %d in check, %d mated",
			pinnedMoves, kingMoves, epMoves, inCheck, mates)
	}
	t.Logf("%d pinned-man moves, %d king moves, %d en passant, %d positions in check, %d mated",
		pinnedMoves, kingMoves, epMoves, inCheck, mates)
}

package board

import (
	"math/rand"
	"testing"
)

// pseudoMoves lists every pseudo-legal move of side c, king captures left
// out (the board tracks one king per side and a game never takes one), plus
// the two-file king steps MakeMove turns into castling whenever the right is
// held and the squares between are empty. Legality is not checked: make and
// unmake have to undo whatever they are given.
func pseudoMoves(b *Board, c Color) [][2]Sq {
	var out [][2]Sq
	for _, ps := range b.AppendPiecesOf(nil, c) {
		var targets []Sq
		switch ps.Type {
		case Pawn:
			targets = b.AppendPawnMoves(nil, ps.Sq, c)
		case Knight:
			targets = b.AppendKnightMoves(nil, ps.Sq, c)
		case Bishop:
			targets = b.AppendBishopMoves(nil, ps.Sq, c)
		case Rook:
			targets = b.AppendRookMoves(nil, ps.Sq, c)
		case Queen:
			targets = b.AppendQueenMoves(nil, ps.Sq, c)
		case King:
			targets = b.AppendKingMoves(nil, ps.Sq, c)
		}
		for _, to := range targets {
			if p, ok := b.PieceAt(to); ok && p.Type == King {
				continue
			}
			out = append(out, [2]Sq{ps.Sq, to})
		}
	}
	rank := int8(0)
	kingSide, queenSide := WhiteKingSide, WhiteQueenSide
	if c == Black {
		rank, kingSide, queenSide = 7, BlackKingSide, BlackQueenSide
	}
	king := Sq{File: 4, Rank: rank}
	empty := func(files ...int8) bool {
		for _, f := range files {
			if _, ok := b.PieceAt(Sq{File: f, Rank: rank}); ok {
				return false
			}
		}
		return true
	}
	if p, ok := b.PieceAt(king); ok && p.Type == King && p.Color == c {
		if b.castle&kingSide != 0 && empty(5, 6) {
			out = append(out, [2]Sq{king, {File: 6, Rank: rank}})
		}
		if b.castle&queenSide != 0 && empty(1, 2, 3) {
			out = append(out, [2]Sq{king, {File: 2, Rank: rank}})
		}
	}
	return out
}

// playMove makes a move the way the search does, promoting a pawn that
// reaches the last rank with SetPiece after MakeMove.
func playMove(b *Board, m [2]Sq, promo PieceType) Undo {
	u := b.MakeMove(m[0], m[1])
	if p, ok := b.PieceAt(m[1]); ok && p.Type == Pawn && (m[1].Rank == 7 || m[1].Rank == 0) {
		b.SetPiece(m[1], Piece{Color: p.Color, Type: promo})
	}
	return u
}

var promotionsForTest = []PieceType{Queen, Knight, Rook, Bishop}

// Every move of every position along random games must come back to the
// identical Board, byte for byte: cells, kings, occupied list and its stale
// tail, castling, en passant and every bitboard. Promotions go through
// SetPiece after MakeMove, as in the search, so unmake meets a piece on the
// target square that is not the one that moved there.
func TestMakeUnmakeRestoresEveryMoveOfRandomGames(t *testing.T) {
	rng := rand.New(rand.NewSource(51))
	var castles, epCaptures, promotions, captures int
	for game := 0; game < 300; game++ {
		b := Initial()
		side := White
		for ply := 0; ply < 120; ply++ {
			moves := pseudoMoves(&b, side)
			if len(moves) == 0 {
				break
			}
			for _, m := range moves {
				for _, promo := range promotionsForTest {
					before := b
					u := playMove(&b, m, promo)
					if u.WasCastling() {
						castles++
					}
					if u.WasEPCapture() {
						epCaptures++
					}
					if u.CapturedRaw() >= uint8(codePieceMin) {
						captures++
					}
					if p, _ := b.PieceAt(m[1]); decodePiece(cellCode(u.MovedCode())).Type != p.Type {
						promotions++
					}
					b.UnmakeMove(u)
					if b != before {
						t.Fatalf("game %d ply %d: %v -> %v (promo %v) not undone exactly", game, ply, m[0], m[1], promo)
					}
					if !isPromotion(&b, m) {
						break // only a promotion needs every piece tried
					}
				}
			}
			playMove(&b, moves[rng.Intn(len(moves))], promotionsForTest[rng.Intn(len(promotionsForTest))])
			side = side.Other()
		}
	}
	if castles == 0 || epCaptures == 0 || promotions == 0 || captures == 0 {
		t.Fatalf("random games missed a case: %d castles, %d en passant, %d promotions, %d captures",
			castles, epCaptures, promotions, captures)
	}
}

// AppendPawnMoves reads the en passant square directly now, so EPSquare is
// pinned here on its own: none at the start, the skipped square after a
// double step, none again after the reply.
func TestEPSquareFollowsADoubleStep(t *testing.T) {
	b := Initial()
	if _, ok := b.EPSquare(); ok {
		t.Fatalf("en passant square at the start")
	}
	b.MakeMove(Sq{File: 4, Rank: 1}, Sq{File: 4, Rank: 3})
	if ep, ok := b.EPSquare(); !ok || ep != (Sq{File: 4, Rank: 2}) {
		t.Fatalf("after e2e4: %v %v, want e3", ep, ok)
	}
	b.MakeMove(Sq{File: 6, Rank: 7}, Sq{File: 5, Rank: 5})
	if _, ok := b.EPSquare(); ok {
		t.Fatalf("en passant square survived the reply")
	}
}

func isPromotion(b *Board, m [2]Sq) bool {
	p, ok := b.PieceAt(m[0])
	return ok && p.Type == Pawn && (m[1].Rank == 7 || m[1].Rank == 0)
}

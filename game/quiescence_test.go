package game

import (
	"math/rand"
	"testing"

	"chess/board"
)

// Quiescence looks at captures, promotions and, when in check, every
// evasion. Generating all legal moves and dropping the quiet ones was more
// than half of the search's CPU, so the generator now produces that subset
// directly. It has to match the filtered full list exactly, and it must
// still say whether any legal move exists: a quiet position with no
// captures and no legal moves is stalemate, not a stand-pat.
func TestQuiescenceMovesMatchTheFilteredFullList(t *testing.T) {
	fens := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3", // en passant on offer
		"7k/8/8/8/8/8/6q1/7K w - - 0 1",                                 // in check
		"7k/5Q2/6K1/8/8/8/8/8 b - - 0 1",                                // stalemate
		"8/P6k/8/8/8/8/8/K7 w - - 0 1",                                  // quiet promotion
		"4k3/8/8/8/1b6/8/3N4/4K3 w - - 0 1",                             // pinned knight
	}
	rng := rand.New(rand.NewSource(7))
	checked := 0
	for _, fen := range fens {
		g, err := ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		for step := 0; step < 40; step++ {
			checked++
			compareQuiescence(t, g, fen, step)
			var buf [96]Move
			all, _ := g.AppendLegalMovesInCheck(buf[:0], g.Turn)
			if len(all) == 0 {
				break
			}
			m := all[rng.Intn(len(all))]
			g.Board.MakeMove(m.From, m.To)
			g.Turn = g.Turn.Other()
		}
	}
	if checked < 200 {
		t.Fatalf("only %d positions compared", checked)
	}
}

func compareQuiescence(t *testing.T, g *Game, fen string, step int) {
	var fullBuf, qBuf [96]Move
	full, inCheck := g.AppendLegalMovesInCheck(fullBuf[:0], g.Turn)
	got, gotCheck, anyLegal := g.AppendQuiescenceMoves(qBuf[:0], g.Turn)
	if gotCheck != inCheck || anyLegal != (len(full) > 0) {
		t.Errorf("%s +%d: inCheck %v want %v, anyLegal %v with %d legal moves",
			fen, step, gotCheck, inCheck, anyLegal, len(full))
	}
	ep, hasEP := g.Board.EPSquare()
	var want []Move
	for _, m := range full {
		_, occupied := g.Board.PieceAt(m.To)
		p, _ := g.Board.PieceAt(m.From)
		epCapture := hasEP && p.Type == board.Pawn && m.To == ep && m.From.File != m.To.File
		promotes := p.Type == board.Pawn && (m.To.Rank == 0 || m.To.Rank == 7)
		if inCheck || occupied || epCapture || promotes {
			want = append(want, m)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s +%d: %d quiescence moves, want %d", fen, step, len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s +%d: move %d is %v, want %v", fen, step, i, got[i], want[i])
		}
	}
}

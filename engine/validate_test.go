package engine

import (
	"math/rand"
	"os"
	"testing"

	"chess/board"
	"chess/game"
)

// TestMoveGenerationMatchesStockfish validates this engine's move
// generation against a reference implementation across positions reached
// by random play. Skipped when Stockfish is not installed.
//
// This engine implements a rule subset (no castling, no en passant,
// queen promotion only), and FEN() reports castling/en-passant as
// unavailable, so the reference is asked about exactly the same position
// and the move sets should agree exactly.
func TestMoveGenerationMatchesStockfish(t *testing.T) {
	const path = "/opt/homebrew/bin/stockfish"
	if _, err := os.Stat(path); err != nil {
		t.Skip("stockfish not installed")
	}
	sf, err := NewStockfish(path, 20, 0)
	if err != nil {
		t.Skipf("could not start stockfish: %v", err)
	}
	defer sf.Close()

	rng := rand.New(rand.NewSource(7))
	checked := 0
	for trial := 0; trial < 5; trial++ {
		g := game.New()
		for ply := 0; ply < 40 && !g.IsOver(); ply++ {
			mine := map[string]bool{}
			for _, m := range g.AllLegalMoves(g.Turn) {
				uci := m.UCI()
				if p, ok := g.Board.PieceAt(m.From); ok && p.Type == board.Pawn &&
					(m.To.Rank == 7 || m.To.Rank == 0) {
					uci += "q"
				}
				mine[uci] = true
			}
			theirs, err := sf.LegalMoves(g)
			if err != nil {
				t.Fatalf("uci error: %v", err)
			}
			checked++
			for mv := range theirs {
				if !mine[mv] {
					t.Fatalf("missing legal move %s at %s", mv, g.FEN())
				}
			}
			for mv := range mine {
				if !theirs[mv] {
					t.Fatalf("generated illegal move %s at %s", mv, g.FEN())
				}
			}
			moves := g.AllLegalMoves(g.Turn)
			if len(moves) == 0 {
				break
			}
			m := moves[rng.Intn(len(moves))]
			g.ApplyMove(m.From, m.To)
		}
	}
	t.Logf("validated %d positions against stockfish", checked)
}

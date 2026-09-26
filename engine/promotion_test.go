package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/board"
)

// A GUI's "position ... moves" with an underpromotion: the engine has to
// search the knight that is really there, not a phantom queen.
func TestUCIPositionKeepsUnderpromotion(t *testing.T) {
	g := uciPosition(strings.Fields("fen 8/4P1k1/8/8/8/8/8/K7 w - - 0 1 moves e7e8n g7f6"))
	p, ok := g.Board.PieceAt(board.Sq{File: 4, Rank: 7})
	if !ok || p != (board.Piece{Color: board.White, Type: board.Knight}) {
		t.Errorf("piece on e8 = %+v (ok=%v), want a white knight", p, ok)
	}
}

// Stockfish underpromotes now and then. The match harness has to play the
// piece it chose, or the rest of the game is played on a board Stockfish
// never saw.
func TestMatchPlaysAnEngineUnderpromotionAsChosen(t *testing.T) {
	sf, err := NewStockfish(fakeUCI(t, "bestmove a2a1n"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	g := mustFEN(t, "4k3/8/8/8/8/8/p7/4K3 b - - 0 1")
	m, ok := sf.BestMove(g, 1, 0)
	if !ok || m.Promo != board.Knight {
		t.Fatalf("BestMove = %+v (ok=%v), want a2a1 promoting to a knight", m, ok)
	}
	playFrom(g, Player{Random: true}, Player{Name: "sf", UCI: sf, UCIDepth: 1}, 1, nil)
	p, ok := g.Board.PieceAt(board.Sq{File: 0, Rank: 0})
	if !ok || p != (board.Piece{Color: board.Black, Type: board.Knight}) {
		t.Errorf("after Stockfish's a2a1n the harness board has %+v (ok=%v) on a1, want a black knight", p, ok)
	}
}

// A book built from Lichess principal variations can hold an
// underpromotion; the book move is that move, not the queen promotion.
func TestBookMoveKeepsUnderpromotion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.txt")
	fen := "4k3/8/8/8/8/8/p7/4K3 b - - 0 1"
	if err := os.WriteFile(path, []byte(fen+"|a2a1n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := b.Move(mustFEN(t, fen))
	if !ok || m.UCI() != "a2a1n" {
		t.Errorf("book move = %q (ok=%v), want a2a1n", m.UCI(), ok)
	}
}

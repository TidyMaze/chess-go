package engine

import (
	"os"
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

func TestBookEdges(t *testing.T) {
	if BookKey("only three fields") != "only three fields" {
		t.Error("a short FEN is used whole")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "book.txt")
	os.WriteFile(path, []byte("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2\n"+
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|zz99\n"+
		"no separator here\n"+
		"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1|a1a1\n"), 0o600)
	b, err := LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.Len() != 2 {
		t.Errorf("book kept %d entries, want 2", b.Len())
	}
	var none *Book
	if none.Len() != 0 {
		t.Error("nil book has entries")
	}
	if _, ok := b.Move(game.New()); ok {
		t.Error("a book move that is not a UCI move was played")
	}
	after := mustFEN(t, "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1")
	if _, ok := b.Move(after); ok {
		t.Error("a book move that is not legal here was played")
	}
	if _, err := LoadBook(filepath.Join(dir, "absent")); err == nil {
		t.Error("missing book loaded")
	}
}

func TestChampionOptionalResourcesAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	os.WriteFile(path, []byte(`{"label":"x","depth":0}`), 0o600)
	if c := ReadChampion(path); c.Depth != 5 {
		t.Errorf("depth 0 in the file must default to 5, got %d", c.Depth)
	}
	book := filepath.Join(dir, "book.txt")
	os.WriteFile(book, []byte("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2e4\n"), 0o600)
	c := Champion{Depth: 2, Book: book}
	if p, err := c.PlayerOrError(); err != nil || p.Book == nil {
		t.Errorf("book not carried: %v", err)
	}
	c.Book = filepath.Join(dir, "absent.txt")
	if _, err := c.PlayerOrError(); err == nil {
		t.Error("a missing book was not an error")
	}
	c = Champion{Depth: 2, Syzygy: filepath.Join(dir, "absent.bin")}
	if _, err := c.PlayerOrError(); err == nil {
		t.Error("missing tablebases were not an error")
	}
	if _, err := os.Stat("../tablebases3.bin"); err == nil {
		c.Syzygy = "../tablebases3.bin"
		if p, err := c.PlayerOrError(); err != nil || p.Tablebases == nil {
			t.Errorf("tablebases not carried: %v", err)
		}
	}
}

func TestPlayGameEndings(t *testing.T) {
	w := Weights(&[6]float64{1, 3, 3, 5, 9, 0})
	// Mate in one for the side to move: found at depth 1.
	r := PlayGame(w, w, 1, 4, nil)
	_ = r
	if res := playFromFEN(t, "6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1", w); res.Reason != "checkmate" || !res.Decisive || res.Winner != board.White {
		t.Errorf("Ra8#: %+v", res)
	}
	if res := playFromFEN(t, "7k/5Q2/6K1/8/8/8/8/8 b - - 0 1", w); res.Reason != "stalemate" || res.Decisive {
		t.Errorf("stalemate: %+v", res)
	}
	// Bare kings can only shuffle: a draw by repetition or the fifty-move rule.
	if res := playFromFEN(t, "8/8/8/8/8/8/8/K6k w - - 0 1", w); res.Decisive || (res.Reason != "threefold repetition" && res.Reason != "fifty-move rule" && res.Reason != "move limit") {
		t.Errorf("bare kings: %+v", res)
	}
	if SaveChampion(w, 1, filepath.Join(t.TempDir(), "no", "dir", "c.json")) == nil {
		t.Error("saved into a missing directory")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("{"), 0o600)
	if _, _, err := LoadChampion(bad); err == nil {
		t.Error("malformed champion loaded")
	}
}

// playFromFEN runs PlayGame's loop from a given position by handing it a
// game already set up: PlayGame starts from the initial position, so the
// endings are reached through the game-level helper it shares.
func playFromFEN(t *testing.T, fen string, w Weights) GameResult {
	t.Helper()
	g := mustFEN(t, fen)
	g.TrackRepetition = true
	return playGameFrom(g, w, w, 1, 200, nil)
}

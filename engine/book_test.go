package engine

import (
	"os"
	"path/filepath"
	"testing"

	"chess/game"
)

func writeBook(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBookFindsAMoveForTheStartingPosition(t *testing.T) {
	b, err := LoadBook(writeBook(t,
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2e4\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := b.Move(game.New())
	if !ok {
		t.Fatal("no book move for the starting position")
	}
	if m.UCI() != "e2e4" {
		t.Errorf("book played %s, want e2e4", m.UCI())
	}
}

// The same position reached by a different move order must hit the same
// entry, which is why the key drops the move counters.
func TestBookIgnoresMoveCounters(t *testing.T) {
	b, _ := LoadBook(writeBook(t,
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 9 42|d2d4\n"))
	if _, ok := b.Move(game.New()); !ok {
		t.Error("the starting position missed an entry differing only in its counters")
	}
}

// The book is an external file. An entry naming a move that is not legal
// in that position must be declined, not played: playing it would corrupt
// the game in a way that looks like an engine bug.
func TestBookDeclinesAnIllegalMove(t *testing.T) {
	b, _ := LoadBook(writeBook(t,
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2e5\n"))
	if m, ok := b.Move(game.New()); ok {
		t.Errorf("book played the illegal move %s", m.UCI())
	}
}

func TestBookSkipsMalformedLinesRatherThanFailing(t *testing.T) {
	b, err := LoadBook(writeBook(t,
		"# a comment\n\nnot a book line\nrnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|g1f3\n"))
	if err != nil {
		t.Fatalf("a malformed line failed the whole book: %v", err)
	}
	if b.Len() != 1 {
		t.Errorf("book holds %d entries, want 1", b.Len())
	}
	if m, ok := b.Move(game.New()); !ok || m.UCI() != "g1f3" {
		t.Errorf("the one good line was not usable")
	}
}

func TestBookMissIsNotAnError(t *testing.T) {
	b, _ := LoadBook(writeBook(t, "8/8/8/8/8/8/8/K6k w - - 0 1|a1a2\n"))
	if _, ok := b.Move(game.New()); ok {
		t.Error("a position not in the book returned a move")
	}
	var nilBook *Book
	if _, ok := nilBook.Move(game.New()); ok {
		t.Error("a nil book returned a move")
	}
}

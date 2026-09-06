package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/game"
)

func TestStartingPositionUsesTheBook(t *testing.T) {
	const fen = "r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3"
	g := startingPosition([]string{fen}, rand.New(rand.NewSource(1)))
	if got := g.FEN(); !strings.HasPrefix(got, "r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R") {
		t.Errorf("started from %q, want the book position", got)
	}
}

// One malformed line in a book of a hundred thousand must not cost a
// generation of self-play, so a bad entry falls back rather than failing.
func TestStartingPositionFallsBackOnABadBookEntry(t *testing.T) {
	g := startingPosition([]string{"this is not a fen"}, rand.New(rand.NewSource(1)))
	if g == nil {
		t.Fatal("a bad book entry produced no game")
	}
	if len(g.AllLegalMoves(g.Turn)) == 0 {
		t.Error("the fallback position has no legal moves")
	}
}

func TestStartingPositionWithoutABookIsRandom(t *testing.T) {
	a := startingPosition(nil, rand.New(rand.NewSource(1)))
	b := startingPosition(nil, rand.New(rand.NewSource(2)))
	if a.FEN() == b.FEN() {
		t.Error("two different seeds gave the same opening, so it is not random")
	}
}

func TestExtractOpeningsKeepsOnlyEarlyStandardPositions(t *testing.T) {
	input := strings.Join([]string{
		// 32 pieces, an opening: kept.
		`{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"depth":40,"pvs":[{"cp":20}]}]}`,
		// 3 pieces, an endgame: dropped.
		`{"fen":"4k3/8/8/8/8/8/8/4K2R w K -","evals":[{"depth":40,"pvs":[{"cp":300}]}]}`,
		// A Horde position: dropped as a variant.
		`{"fen":"7K/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/q6k b - -","evals":[{"depth":40,"pvs":[{"cp":10}]}]}`,
		// The opening again: dropped as a duplicate.
		`{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"depth":40,"pvs":[{"cp":20}]}]}`,
	}, "\n")

	path := filepath.Join(t.TempDir(), "book.txt")
	if err := ExtractOpenings(strings.NewReader(input), path, 0, 28); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOpenings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		data, _ := os.ReadFile(path)
		t.Fatalf("book has %d entries, want 1:\n%s", len(got), data)
	}
	// Written padded, so it parses where it is used.
	if _, err := game.ParseFEN(got[0]); err != nil {
		t.Errorf("book entry %q does not parse: %v", got[0], err)
	}
}

package engine

import (
	"math"
	"testing"

	"chess/board"
	"chess/game"
)

// Every match game can be handed to a sink as a record: where it started,
// every move, the mover's reported score per ply, who played which colour
// and how it ended. This is what a loss audit reads; without it a gauntlet
// only ever says how many games were lost, never how.
func TestPlayFromHandsTheFinishedGameToTheSink(t *testing.T) {
	var got []GameRecord
	GameSink = func(r GameRecord) { got = append(got, r) }
	defer func() { GameSink = nil }()

	white := Player{Name: "w", Depth: 1, Iterative: true}
	black := Player{Name: "b", Random: true}
	g := game.New()
	startFEN := g.FEN()
	winner, decisive := playFrom(g, white, black, 6, nil)

	if len(got) != 1 {
		t.Fatalf("sink received %d records, want 1", len(got))
	}
	r := got[0]
	if r.StartFEN != startFEN {
		t.Errorf("start FEN %q, want %q", r.StartFEN, startFEN)
	}
	if len(r.Moves) == 0 || len(r.Moves) > 6 {
		t.Errorf("%d moves recorded, want 1..6", len(r.Moves))
	}
	if len(r.Scores) != len(r.Moves) {
		t.Errorf("%d scores for %d moves", len(r.Scores), len(r.Moves))
	}
	// The random player reports no score, the searching one does.
	if !math.IsNaN(r.Scores[1]) {
		t.Errorf("black is random and should report NaN, got %v", r.Scores[1])
	}
	if math.IsNaN(r.Scores[0]) {
		t.Error("white searched and should report a score")
	}
	if r.White != "w" || r.Black != "b" {
		t.Errorf("names %q/%q, want w/b", r.White, r.Black)
	}
	if r.Winner != winner || r.Decisive != decisive {
		t.Errorf("result %v/%v, want %v/%v", r.Winner, r.Decisive, winner, decisive)
	}
	if r.Winner != board.White && r.Winner != board.Black {
		t.Errorf("winner %v is not a colour", r.Winner)
	}
}

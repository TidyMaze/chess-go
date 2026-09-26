package main

import (
	"math"
	"testing"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// The worst drop is measured across the challenger's own moves only, from
// the evaluation before the move to the one after it; the opponent's moves
// raising or lowering the score are not the challenger's mistakes.
func TestWorstDropIsTheChallengersLargestFall(t *testing.T) {
	evals := []plyEval{
		{ply: 1, ours: true, eval: 0.2},
		{ply: 2, ours: false, eval: -0.1}, // opponent improved: not ours
		{ply: 3, ours: true, eval: -0.3},  // we lost 0.2
		{ply: 4, ours: false, eval: -0.4},
		{ply: 5, ours: true, eval: -2.9}, // we lost 2.5
		{ply: 6, ours: false, eval: -3.0},
	}
	w := findWorstDrop(evals)
	if !w.found || w.ply != 5 || math.Abs(w.drop-2.5) > 1e-9 {
		t.Fatalf("worst drop %+v, want ply 5, drop 2.5", w)
	}
}

func TestMateScoresDoNotCountAsDrops(t *testing.T) {
	evals := []plyEval{
		{ply: 1, ours: true, eval: 0.5},
		{ply: 2, ours: false, eval: 0.4},
		{ply: 3, ours: true, mate: true},
		{ply: 4, ours: false, eval: 0.3},
		{ply: 5, ours: true, eval: 0.1},
	}
	if w := findWorstDrop(evals); w.ply != 5 || math.Abs(w.drop-0.2) > 1e-9 {
		t.Fatalf("got %+v, want the 0.2 fall at ply 5 measured from 0.3", w)
	}
}

// The judge answers for the side to move; replay must flip it to the
// challenger's view so a fall is a fall whichever colour we hold.
func TestReplayFlipsTheJudgeToTheChallengersView(t *testing.T) {
	g := game.New()
	r := engine.GameRecord{StartFEN: g.FEN(), Moves: []string{"e2e4", "e7e5"}, Scores: []float64{0.3, math.NaN()},
		White: "reference", Black: "challenger: x"}
	// A judge that always says +1 for the side to move.
	judge := func(*game.Game) (float64, bool, bool) { return 1, false, true }
	evals, err := replay(r, judge, 1)
	if err != nil {
		t.Fatal(err)
	}
	// After e2e4 Black (challenger) is to move: +1 for the challenger.
	// After e7e5 White is to move: +1 for White is -1 for the challenger.
	if evals[0].eval != 1 || evals[1].eval != -1 {
		t.Fatalf("evals %v %v, want +1 then -1", evals[0].eval, evals[1].eval)
	}
	if evals[0].ours || !evals[1].ours {
		t.Fatal("ownership of moves is wrong")
	}
	if evals[1].material != 14 {
		t.Errorf("material %d, want 14 non-pawn men", evals[1].material)
	}
}

// A recorded underpromotion replays as the piece that was chosen, or every
// position the judge sees after it is one that never happened.
func TestReplayKeepsAnUnderpromotion(t *testing.T) {
	r := engine.GameRecord{StartFEN: "4k3/8/8/8/8/8/p7/4K3 b - - 0 1", Moves: []string{"a2a1n"},
		Scores: []float64{math.NaN()}, White: "reference", Black: "challenger: x"}
	var onA1 board.Piece
	judge := func(g *game.Game) (float64, bool, bool) {
		onA1, _ = g.Board.PieceAt(board.Sq{File: 0, Rank: 0})
		return 0, false, true
	}
	if _, err := replay(r, judge, 1); err != nil {
		t.Fatal(err)
	}
	if onA1 != (board.Piece{Color: board.Black, Type: board.Knight}) {
		t.Errorf("after a2a1n the judge saw %+v on a1, want a black knight", onA1)
	}
}

func TestOutcomeFromTheChallengersSide(t *testing.T) {
	r := engine.GameRecord{White: "challenger: x", Black: "reference", Winner: board.Black, Decisive: true}
	if outcome(r) != 0 {
		t.Error("a black win with the challenger white is a loss")
	}
	r.Decisive = false
	if outcome(r) != 0.5 {
		t.Error("undecided is a draw")
	}
}

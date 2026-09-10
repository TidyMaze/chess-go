package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Adjudication must never contradict a finished game, and must not shift
// the aggregate score.
//
// A game where both engines have agreed for several plies that one side
// is six pawns up is decided; playing it out costs a third of the match's
// wall clock. The safety condition is agreement: one engine can be wrong
// about its own position, but two differently configured engines both
// reporting a six-pawn gap, on alternating plies, are as reliable as the
// result would have been.
//
// What is deliberately *not* asserted is per-game equality with playing
// it out, because the two legitimately differ: king and rook against king
// is a forced win that a shallow engine cannot convert inside the ply
// cap, so playing it out scores a draw and adjudication scores the win.
// Adjudication is the more accurate of the two there. What must hold is
// that a real mate is never contradicted, and that the aggregate a match
// measures does not move.
func TestAdjudicationNeverContradictsAFinishedGame(t *testing.T) {
	oldWin, oldWinPlies := AdjudicateWinPawns, AdjudicateWinPlies
	oldDraw, oldDrawPlies, oldAfter := AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly
	defer func() {
		AdjudicateWinPawns, AdjudicateWinPlies = oldWin, oldWinPlies
		AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly = oldDraw, oldDrawPlies, oldAfter
	}()

	openings := []string{
		"4k3/8/8/8/8/8/8/QQQ1K3 w - - 0 1",
		"qqq1k3/8/8/8/8/8/8/4K3 w - - 0 1",
		"4k3/8/8/8/8/8/8/RRR1K3 b - - 0 1",
		"6k1/5ppp/8/8/8/8/5PPP/6K1 w - - 0 1",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	}
	a, b := Strong(3), Strong(3)
	a.Name, b.Name = "a", "b"

	type outcome struct {
		winner   board.Color
		decisive bool
		mated    bool
		plies    int
	}
	run := func(adjudicate bool) []outcome {
		if adjudicate {
			AdjudicateWinPawns, AdjudicateWinPlies = 6, 6
			AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly = 0.35, 10, 60
		} else {
			AdjudicateWinPawns, AdjudicateDrawPawns = 0, 0
		}
		var out []outcome
		for i, fen := range openings {
			// Seeded, so the random tie-break among equal moves plays the
			// same game in both arms and the comparison is paired.
			SeedRandom(int64(i) + 1)
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			plies := 0
			w, d := playFrom(g, a, b, 200, func(*game.Game, int, board.Sq, board.Sq) { plies++ })
			out = append(out, outcome{w, d, g.IsCheckmate(g.Turn) || g.KingCaptured, plies})
		}
		return out
	}
	full, adj := run(false), run(true)

	scoreOf := func(o outcome) float64 {
		if !o.decisive {
			return 0.5
		}
		if o.winner == board.White {
			return 1
		}
		return 0
	}
	fullScore, adjScore, savedPlies, totalPlies := 0.0, 0.0, 0, 0
	for i := range full {
		// A game that actually ended in mate is not negotiable.
		if full[i].mated && (full[i].winner != adj[i].winner || !adj[i].decisive) {
			t.Errorf("%s ended in mate for %v; adjudication said %v/%v",
				openings[i], full[i].winner, adj[i].winner, adj[i].decisive)
		}
		fullScore += scoreOf(full[i])
		adjScore += scoreOf(adj[i])
		totalPlies += full[i].plies
		savedPlies += full[i].plies - adj[i].plies
	}
	n := float64(len(full))
	t.Logf("score per game: %.3f played out, %.3f adjudicated; plies: %d -> %d (%.0f%% saved)",
		fullScore/n, adjScore/n, totalPlies, totalPlies-savedPlies,
		100*float64(savedPlies)/float64(totalPlies))
	// The aggregate is what a match measures. A shift of more than a
	// quarter of a point per game would bias every result.
	if diff := adjScore/n - fullScore/n; diff < -0.25 || diff > 0.25 {
		t.Errorf("adjudication moved the score per game by %+.3f", diff)
	}
	// The draw rule has to actually fire. On a 0.10 band with a sixteen-ply
	// streak it never did: only 45% of late plies in level games sit that
	// close to zero, so every drawn game ran to the ply cap and the "saved"
	// figure above came entirely from the win rule.
	drawsShortened := 0
	for i := range full {
		if !full[i].decisive && !adj[i].decisive && adj[i].plies < full[i].plies {
			drawsShortened++
		}
	}
	if drawsShortened == 0 {
		t.Error("no drawn game was adjudicated early, so the draw rule is dead")
	}
	if savedPlies <= 0 {
		t.Errorf("adjudication saved no plies (%d of %d)", savedPlies, totalPlies)
	}
}

// The rules themselves, as a pure decision.
func TestAdjudicationRules(t *testing.T) {
	oldWin, oldWinPlies := AdjudicateWinPawns, AdjudicateWinPlies
	oldDraw, oldDrawPlies, oldAfter := AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly
	defer func() {
		AdjudicateWinPawns, AdjudicateWinPlies = oldWin, oldWinPlies
		AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly = oldDraw, oldDrawPlies, oldAfter
	}()
	AdjudicateWinPawns, AdjudicateWinPlies = 6, 6
	AdjudicateDrawPawns, AdjudicateDrawPlies, AdjudicateDrawAfterPly = 0.35, 10, 60

	var a adjudicator
	// One side clearly winning, but not yet for long enough.
	for i := 0; i < 5; i++ {
		if w, decided := a.observe(board.White, 7.5, i); decided {
			t.Fatalf("decided after %d plies as %v", i+1, w)
		}
	}
	w, decided := a.observe(board.White, 7.5, 5)
	if !decided || w != board.White {
		t.Errorf("six plies at seven pawns: decided=%v winner=%v", decided, w)
	}
	// A gap that flickers below the threshold resets the count.
	var f adjudicator
	for i := 0; i < 5; i++ {
		f.observe(board.White, 7.5, i)
	}
	f.observe(board.White, 2.0, 5)
	if _, decided := f.observe(board.White, 7.5, 6); decided {
		t.Error("a reset count still decided")
	}
	// Disagreement about who is winning never decides.
	var dis adjudicator
	for i := 0; i < 20; i++ {
		side := board.White
		if i%2 == 1 {
			side = board.Black
		}
		if _, decided := dis.observe(side, 7.5, i); decided {
			t.Fatal("decided while the engines disagreed on who is winning")
		}
	}
	// A dead-equal position, but only after the opening.
	var dr adjudicator
	for i := 0; i < 40; i++ {
		if _, _, drawn := dr.observeDraw(0.02, i); drawn && i < AdjudicateDrawAfterPly {
			t.Fatalf("drew at ply %d, before the opening is over", i)
		}
	}
	var late adjudicator
	drawn := false
	for i := 60; i < 60+AdjudicateDrawPlies; i++ {
		_, _, drawn = late.observeDraw(0.02, i)
	}
	if !drawn {
		t.Errorf("%d quiet plies after ply 60 did not draw", AdjudicateDrawPlies)
	}
	// A score outside the band resets it.
	var swing adjudicator
	for i := 60; i < 70; i++ {
		swing.observeDraw(0.02, i)
	}
	swing.observeDraw(1.5, 70)
	if _, _, drawn := swing.observeDraw(0.02, 71); drawn {
		t.Error("a reset draw count still drew")
	}
	// Switched off, nothing is ever decided.
	AdjudicateWinPawns, AdjudicateDrawPawns = 0, 0
	var off adjudicator
	for i := 0; i < 100; i++ {
		if _, decided := off.observe(board.White, 50, i); decided {
			t.Fatal("decided with adjudication off")
		}
		if _, _, drawn := off.observeDraw(0, i); drawn {
			t.Fatal("drew with adjudication off")
		}
	}
}

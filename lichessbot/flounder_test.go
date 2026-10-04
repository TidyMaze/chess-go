package lichessbot

import (
	"testing"
	"time"

	"chess/engine"
)

func TestFlounderGameQ7KEaiiY(t *testing.T) {
	p, err := engine.ReadChampion("../champion_bot.json").PlayerOrError()
	if err != nil {
		t.Fatalf("read champion: %v", err)
	}
	p.MCTS = true // bot runs with -mcts flag in start_bullet_blitz_bot.sh

	moves := "d2d4 g8f6 b1c3 d7d5 c1f4 e7e6 e2e3 f8d7 g1f3 f6h5 f4g5"
	var full gameFull
	full.InitialFen = "startpos"
	full.Speed = "bullet"
	full.White.ID = "flounderbot"
	full.Black.ID = "tidymazebot"
	st := gameState{
		Moves:       moves,
		WhiteTimeMS: 48000,
		BlackTimeMS: 64000,
		WhiteIncMS:  2000,
		BlackIncMS:  2000,
	}

	g, err := applyMovesString("startpos", moves)
	if err != nil {
		t.Fatalf("applyMovesString: %v", err)
	}

	color, err := ourColor(full, "tidymazebot")
	if err != nil {
		t.Fatalf("ourColor: %v", err)
	}
	if !isOurTurn(g, color) {
		t.Fatalf("expected our turn, got %v (color=%s)", g.Turn, color)
	}

	sess := &gameSession{overhead: newOverheadEstimate()}
	_, inBook := p.Book.Move(g)
	outOfBook := sess.movesOutOfBook(11, inBook)
	player := effectivePlayer(p, color, st, full.Speed, sess.overhead, outOfBook)

	t.Logf("TimeBudget: %v, HardBudget: %v, Threads: %d, MCTS: %v, MCTSv4: %v",
		player.TimeBudget, player.HardBudget, player.Threads, player.MCTS, player.MCTSv4)

	start := time.Now()
	m, score, ok := engine.PlayerPickScored(player, g)
	elapsed := time.Since(start)
	t.Logf("Search result: move=%s, score=%.3f, ok=%v in %v", g.MoveUCI(m), score, ok, elapsed)
}

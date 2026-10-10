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

func TestDrawGameLvXOnz47(t *testing.T) {
	p, err := engine.ReadChampion("../champion_bot.json").PlayerOrError()
	if err != nil {
		t.Fatalf("read champion: %v", err)
	}
	p.MCTS = true
	p.TimeBudget = 50 * time.Millisecond

	moves := "e2e4 e7e6 d2d4 d7d5 b1c3 f8b4 e4e5 c7c5 a2a3 b4c3 b2c3 b8c6 g1f3 g8e7 d4c5 e8g8 a1b1 a7a6 f1d3 c8d7 f3g5 e7g6 d1h5 h7h6 g5f7 f8f7 h5g6 g8f8 g6g3 d8c7 f2f4 a8c8 e1g1 c6d8 g3h3 d7a4 f4f5 a4e8 f5e6 f7f1 d3f1 e8g6 f1d3 g6d3 c2d3 b7b5 c5b6 c7e5 c1d2 e5f6 b1f1 f8e7 f1f6 g7f6 c3c4 d8e6 d2b4 c8c5 b4c5 e7d7 c4d5 f6f5 d5e6 d7c6 b6b7 c6c7 b6b8q c7b8 c5d6 b8a8 h3f3 a8a7 d6c5 a7b8 c5d6 b8a7 f3e3 a7b7"
	g, err := applyMovesString("startpos", moves)
	if err != nil {
		t.Fatalf("applyMovesString: %v", err)
	}
	t.Logf("Turn: %d, FEN: %s", g.Turn, g.FEN())
	if g.Turn != 0 {
		t.Fatalf("expected White turn, got %v", g.Turn)
	}

	for _, mv := range g.AllLegalMoves(g.Turn) {
		cnt := g.CountIfPlayed(mv.From, mv.To)
		if g.MoveUCI(mv) == "e3f3" {
			t.Logf("Move %s count = %d", g.MoveUCI(mv), cnt)
		}
	}

	m, score, ok := engine.PlayerPickScored(p, g)
	if !ok {
		t.Fatal("no move found")
	}
	t.Logf("Move picked: %s (score %.2f)", g.MoveUCI(m), score)
	if g.MoveUCI(m) == "e3f3" {
		t.Fatalf("MCTS repeated position with e3f3 drawing winning game!")
	}
}

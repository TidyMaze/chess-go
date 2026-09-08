package engine

import (
	"errors"
	"os"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

func TestRandomAndGreedyPlayersPickLegalMoves(t *testing.T) {
	g := game.New()
	m, ok := PlayerPick(Player{Random: true}, g)
	if !ok || !containsMove(g.AllLegalMoves(g.Turn), m) {
		t.Errorf("random player: %v %v", m, ok)
	}
	// A capture is available: greedy takes one.
	cap := mustFEN(t, "4k3/8/8/3p4/4P3/8/8/4K3 w - - 0 1")
	m, ok = PlayerPick(Player{Greedy: true}, cap)
	if _, isCapture := cap.Board.PieceAt(m.To); !ok || !isCapture {
		t.Errorf("greedy player with a capture available played %v", m)
	}
	// None available: any legal move.
	m, ok = PlayerPick(Player{Greedy: true}, g)
	if !ok || !containsMove(g.AllLegalMoves(g.Turn), m) {
		t.Errorf("greedy player without captures: %v %v", m, ok)
	}
	// Mated: no move.
	mated := mustFEN(t, "6k1/5ppp/8/8/8/8/5PPP/R5K1 b - - 0 1")
	mated.ApplyMove(board.Sq{File: 0, Rank: 0}, board.Sq{File: 0, Rank: 7}) // wrong side to move, so place mate directly
	m2 := mustFEN(t, "R5k1/5ppp/8/8/8/8/5PPP/6K1 b - - 0 1")
	if _, ok := PlayerPick(Player{Random: true}, m2); ok {
		t.Error("a mated random player found a move")
	}
	_ = m
}

func containsMove(ms []game.Move, m game.Move) bool {
	for _, x := range ms {
		if x == m {
			return true
		}
	}
	return false
}

func TestMatchResultArithmetic(t *testing.T) {
	r := MatchResult{Wins: 6, Draws: 2, Losses: 2}
	if r.Games() != 10 || r.Score() != 0.7 {
		t.Errorf("games %d score %.2f", r.Games(), r.Score())
	}
	if r.Elo() <= 0 || r.EloMargin() <= 0 {
		t.Errorf("elo %d margin %d for a 70%% score", r.Elo(), r.EloMargin())
	}
	if (MatchResult{Wins: 3}).EloMargin() != 0 || (MatchResult{Losses: 3}).EloMargin() != 0 {
		t.Error("a perfect score has no finite margin and must report 0")
	}
	if a := AnchorPlayer(); a.Depth != 1 {
		t.Errorf("anchor depth %d", a.Depth)
	}
}

func TestTinyMatchesRunEveryHarness(t *testing.T) {
	a, b := Player{Random: true, Name: "a"}, Player{Random: true, Name: "b"}
	if r := PlayMatch(a, b, 4, 20); r.Games() != 4 {
		t.Errorf("PlayMatch played %d games", r.Games())
	}
	calls := 0
	hook := func(g *game.Game, ply int, from, to board.Sq) { calls++ }
	if r := PlayMatchLive(a, b, 2, 20, hook); r.Games() != 2 || calls == 0 {
		t.Errorf("PlayMatchLive: %d games, hook called %d times", r.Games(), calls)
	}
	if r := PlayMatchSerial(a, b, 2, 20); r.Games() != 2 {
		t.Errorf("PlayMatchSerial played %d games", r.Games())
	}
	// Progress reporting, through the callback and through the default print.
	oldEvery, oldCB := MatchProgressEvery, MatchProgress
	defer func() { MatchProgressEvery, MatchProgress = oldEvery, oldCB }()
	MatchProgressEvery = time.Millisecond
	fired := 0
	MatchProgress = func(done, total int, elapsed, eta time.Duration) { fired++ }
	PlayMatch(Strong(2), Strong(2), 8, 30)
	if fired == 0 {
		t.Error("the progress callback never fired on a 1ms ticker")
	}
	MatchProgress = nil
	PlayMatch(Strong(2), Strong(2), 4, 30) // prints progress lines
}

func TestMatchOpeningsFromTheListWithOffset(t *testing.T) {
	oldList, oldOff := MatchOpenings, MatchOpeningOffset
	defer func() { MatchOpenings, MatchOpeningOffset = oldList, oldOff }()
	a := "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1"
	b := "rnbqkbnr/pppppppp/8/8/3P4/8/PPP1PPPP/RNBQKBNR b KQkq d3 0 1"
	MatchOpenings = []string{a, b, "not a fen"}
	MatchOpeningOffset = 1
	if got := BookKey(matchOpening(0).FEN()); got != BookKey(mustFEN(t, b).FEN()) {
		t.Errorf("offset 1 from pair 0 gave %s", got)
	}
	// An unparsable entry falls back to a random opening, which still is a game.
	if g := matchOpening(1); g == nil || len(g.AllLegalMoves(g.Turn)) == 0 {
		t.Error("fallback opening is not playable")
	}
	MatchOpenings = nil
	if g := matchOpening(3); g == nil {
		t.Error("no list: random opening expected")
	}
}

func TestPlayerPickWithReusesATable(t *testing.T) {
	g := game.New()
	table := NewTranspositionTable(12)
	if _, ok := PlayerPickWith(Strong(2), g, table); !ok {
		t.Error("no move")
	}
}

func TestPlayerScoreVariants(t *testing.T) {
	g := game.New()
	p := Strong(2)
	if _, ok := PlayerScore(p, g); !ok {
		t.Error("PlayerScore failed")
	}
	_ = PlayerStaticEval(p, &g.Board)
	_ = QuiescenceScore(p, g)
}

const testStockfish = "/opt/homebrew/bin/stockfish"

func TestPlayMatchAgainstUCI(t *testing.T) {
	if _, err := os.Stat(testStockfish); err != nil {
		t.Skip("no Stockfish at", testStockfish)
	}
	me := Player{Random: true}
	newOpp := func() (Player, func(), error) {
		sf, err := NewStockfish(testStockfish, 0, 1320)
		if err != nil {
			return Player{}, func() {}, err
		}
		return Player{Name: "sf", UCI: sf, UCIDepth: 1}, func() { sf.Close() }, nil
	}
	r, err := PlayMatchAgainstUCI(me, newOpp, 2, 20, 5) // more workers than games: clamped
	if err != nil || r.Games() != 2 {
		t.Errorf("result %+v err %v", r, err)
	}
	broken := func() (Player, func(), error) { return Player{}, func() {}, errors.New("no engine") }
	if _, err := PlayMatchAgainstUCI(me, broken, 1, 10, 0); err == nil {
		t.Error("an opponent that fails to start must be an error")
	}
}

package engine

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

// scriptedEngine is a fake engine for the match clock: it plays the first
// legal move, says the move took whatever wall time its player's name is
// scripted for, and records the budget each move was handed.
type scriptedEngine struct {
	mu      sync.Mutex
	spent   map[string]time.Duration
	budgets []time.Duration
	uciMS   []int
}

func (s *scriptedEngine) pick(p Player, g *game.Game, _ *TranspositionTable) (game.Move, float64, bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budgets = append(s.budgets, p.TimeBudget)
	s.uciMS = append(s.uciMS, p.UCIMoveTimeMS)
	moves := g.AllLegalMoves(g.Turn)
	if len(moves) == 0 {
		return game.Move{}, math.NaN(), false, 0
	}
	return moves[0], math.NaN(), true, s.spent[p.Name]
}

// recordingRule is a budget rule that keeps every clock it was shown.
func recordingRule(views *[]ClockView, budget time.Duration) BudgetRule {
	return func(v ClockView) time.Duration {
		*views = append(*views, v)
		return budget
	}
}

// Each move costs its own side the wall time it took, then earns the
// increment; the other side's clock does not move. The rule reads both
// clocks as they stand before the move, and counts the mover's own moves
// since the match opening.
func TestTheMatchClockDebitsEachMoveAndCreditsTheIncrement(t *testing.T) {
	var views []ClockView
	rule := recordingRule(&views, time.Second)
	fake := &scriptedEngine{spent: map[string]time.Duration{"w": 3 * time.Second, "b": 500 * time.Millisecond}}
	c := &MatchClock{Initial: 10 * time.Second, Increment: time.Second, pick: fake.pick}
	out := playGame(game.New(), Player{Name: "w", ClockRule: rule}, Player{Name: "b", ClockRule: rule}, 4, nil, c)
	// White 10 -> 7 -> 8, black 10 -> 9.5 -> 10.5, white 8 -> 5 -> 6.
	want := []ClockView{
		{OurMS: 10000, OppMS: 10000, IncMS: 1000, OppIncMS: 1000, MovesOutOfBook: 0},
		{OurMS: 10000, OppMS: 8000, IncMS: 1000, OppIncMS: 1000, MovesOutOfBook: 0},
		{OurMS: 8000, OppMS: 10500, IncMS: 1000, OppIncMS: 1000, MovesOutOfBook: 1},
		{OurMS: 10500, OppMS: 6000, IncMS: 1000, OppIncMS: 1000, MovesOutOfBook: 1},
	}
	if len(views) != len(want) {
		t.Fatalf("the rule was asked %d times over 4 plies, want 4: %+v", len(views), views)
	}
	for i := range want {
		if views[i] != want[i] {
			t.Errorf("ply %d: the rule saw %+v, want %+v", i+1, views[i], want[i])
		}
	}
	if out.flag {
		t.Error("a game where nobody ran out of time was scored as a flag loss")
	}
}

// A side whose clock reaches zero on a move loses, and the game says so.
// Zero counts, as on lichess: a clock at 0.0 has fallen. The zero case
// stops on the ply the clock reaches zero, so a rule that let it play on
// could not flag it one move later instead.
func TestASideWhoseClockRunsOutLosesOnTime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		initial  time.Duration
		maxMoves int
	}{
		{"below zero", 5 * time.Second, 100}, // 5 -> 3 -> 1 -> -1
		{"exactly zero", 4 * time.Second, 4}, // 4 -> 2 -> 0 on ply 4
	} {
		fake := &scriptedEngine{spent: map[string]time.Duration{"slow": 2 * time.Second, "fast": time.Millisecond}}
		c := &MatchClock{Initial: tc.initial, pick: fake.pick}
		out := playGame(game.New(), Player{Name: "fast"}, Player{Name: "slow"}, tc.maxMoves, nil, c)
		if !out.flag || !out.decisive || out.winner != board.White {
			t.Errorf("%s: black spent 2s a move on a %s clock and got %+v, want a white win on time",
				tc.name, tc.initial, out)
		}
	}
}

// The match tallies flag losses per side, from the first player's point of
// view, and scores them as losses.
func TestAMatchOnTheClockCountsFlagLossesPerSide(t *testing.T) {
	fake := &scriptedEngine{spent: map[string]time.Duration{"slow": 2 * time.Second, "fast": time.Millisecond}}
	c := &MatchClock{Initial: 3 * time.Second, pick: fake.pick}
	slow, fast := Player{Name: "slow"}, Player{Name: "fast"}
	if got, want := PlayMatchOnClock(slow, fast, 4, 100, c), (MatchResult{Losses: 4, FlagLosses: 4}); got != want {
		t.Errorf("slow against fast: %+v, want %+v", got, want)
	}
	if got, want := PlayMatchOnClock(fast, slow, 4, 100, c), (MatchResult{Wins: 4, OppFlagLosses: 4}); got != want {
		t.Errorf("fast against slow: %+v, want %+v", got, want)
	}
}

// At scale S the rule sees both clocks and the increment at S times their
// real value, so a 12+0.1 second game is budgeted as the 2+1 it stands
// for, and the budget it returns is divided by S before the engine sees
// it. The clock itself is debited in real time.
func TestTheClockScaleDividesTheBudget(t *testing.T) {
	var views []ClockView
	fake := &scriptedEngine{spent: map[string]time.Duration{"w": 300 * time.Millisecond}}
	c := &MatchClock{Initial: 12 * time.Second, Increment: 100 * time.Millisecond, Scale: 10, pick: fake.pick}
	playGame(game.New(), Player{Name: "w", ClockRule: recordingRule(&views, 5*time.Second)}, Player{Name: "b"}, 3, nil, c)
	if len(views) != 2 {
		t.Fatalf("the rule was asked %d times, want 2", len(views))
	}
	if want := (ClockView{OurMS: 120000, OppMS: 120000, IncMS: 1000, OppIncMS: 1000}); views[0] != want {
		t.Errorf("first move at scale 10: the rule saw %+v, want %+v", views[0], want)
	}
	// 12000 - 300 + 100 real milliseconds, times 10.
	if views[1].OurMS != 118000 {
		t.Errorf("after a 300 ms move at scale 10 the rule saw %d ms, want 118000", views[1].OurMS)
	}
	if fake.budgets[0] != 500*time.Millisecond || fake.uciMS[0] != 500 {
		t.Errorf("a 5 s budget at scale 10 reached the engine as %s (UCI movetime %d), want 500ms and 500",
			fake.budgets[0], fake.uciMS[0])
	}
}

// A player with no rule keeps the budget it was configured with, and an
// external engine is never asked for a movetime of zero.
func TestAClockRuleIsTheOnlyThingThatChangesTheBudget(t *testing.T) {
	var views []ClockView
	fake := &scriptedEngine{spent: map[string]time.Duration{}}
	c := &MatchClock{Initial: 12 * time.Second, Scale: 10, pick: fake.pick}
	white := Player{Name: "w", ClockRule: recordingRule(&views, 4*time.Millisecond)}
	black := Player{Name: "b", TimeBudget: 42 * time.Millisecond, UCIMoveTimeMS: 42}
	playGame(game.New(), white, black, 2, nil, c)
	if fake.budgets[0] != 400*time.Microsecond || fake.uciMS[0] != 1 {
		t.Errorf("4 ms at scale 10 reached the engine as %s (UCI movetime %d), want 400µs and 1", fake.budgets[0], fake.uciMS[0])
	}
	if fake.budgets[1] != 42*time.Millisecond || fake.uciMS[1] != 42 {
		t.Errorf("a player without a rule got %s (UCI movetime %d), want its own 42ms", fake.budgets[1], fake.uciMS[1])
	}
}

// An external engine gets its budget as "go movetime", and the real clock
// times it: no fake in the way.
func TestAnExternalEngineOnTheClockIsHandedItsBudgetAsMovetime(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "cmds")
	if err := os.WriteFile(seen, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := NewStockfish(fakeUCIEngine(t, seen), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var views []ClockView
	white := Player{Name: "uci", UCI: e, UCIDepth: 3, ClockRule: recordingRule(&views, 5*time.Second)}
	c := &MatchClock{Initial: 12 * time.Second, Increment: 100 * time.Millisecond, Scale: 10}
	out := playGame(game.New(), white, Player{Random: true}, 1, nil, c)
	if out.flag {
		t.Error("the external engine flagged on a 12 s clock")
	}
	cmds, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cmds), "go movetime 500\n") {
		t.Errorf("a 5 s budget at scale 10 did not reach the engine as go movetime 500:\n%s", cmds)
	}
}

// Real engines on a real clock: nobody flags a 20 s game of 10 plies.
func TestRealPlayersPlayAMatchOnTheClock(t *testing.T) {
	a, b := Player{Random: true, Name: "a"}, Player{Random: true, Name: "b"}
	res := PlayMatchOnClock(a, b, 2, 10, &MatchClock{Initial: 20 * time.Second})
	if res.Games() != 2 || res.FlagLosses+res.OppFlagLosses != 0 {
		t.Errorf("two random movers on a 20 s clock: %+v", res)
	}
}

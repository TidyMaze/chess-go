package main

import (
	"strings"
	"testing"
	"time"

	"chess/lichessbot"
)

func TestBaseAndIncrementReadsAPGNTimeControl(t *testing.T) {
	cases := []struct {
		in        string
		base, inc int64
		ok        bool
	}{
		{"300+3", 300000, 3000, true},
		{"60+0", 60000, 0, true},
		{"1200+10", 1200000, 10000, true},
		// A correspondence or unclocked game says "-", and a wrong guess
		// here would silently corrupt every spend in the game.
		{"-", 0, 0, false},
		{"", 0, 0, false},
		{"300", 0, 0, false},
		{"abc+3", 0, 0, false},
		{"0+3", 0, 0, false},
	}
	for _, c := range cases {
		base, inc, ok := baseAndIncrement(c.in)
		if ok != c.ok || base != c.base || inc != c.inc {
			t.Errorf("baseAndIncrement(%q) = %d, %d, %v; want %d, %d, %v",
				c.in, base, inc, ok, c.base, c.inc, c.ok)
		}
	}
}

func TestParseClocksReadsEveryReadingInOrder(t *testing.T) {
	movetext := `1. b3 { [%clk 0:05:00] } 1... e5 { [%clk 0:04:58] } 2. Bb2 { [%clk 0:05:01] } 2... Nc6 { [%clk 1:00:02] }`
	got := parseClocks(movetext)
	want := []int64{300000, 298000, 301000, 3602000}
	if len(got) != len(want) {
		t.Fatalf("got %d clocks, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clock %d = %d, want %d", i, got[i], want[i])
		}
	}
}

const twoGames = `[Event "rated blitz game"]
[Site "https://lichess.org/abcd1234"]
[White "Opponent"]
[Black "TidyMazeBot"]
[Result "0-1"]
[TimeControl "300+3"]
[Termination "Normal"]

1. e4 { [%clk 0:05:00] } 1... e5 { [%clk 0:05:00] } 2. Nf3 { [%clk 0:04:58] } 2... Nc6 { [%clk 0:04:50] } 3. Bb5 { [%clk 0:04:55] } 3... a6 { [%clk 0:04:40] } 0-1

[Event "rated bullet game"]
[Site "https://lichess.org/wxyz5678"]
[White "TidyMazeBot"]
[Black "Opponent"]
[Result "0-1"]
[TimeControl "60+0"]
[Termination "Time forfeit"]

1. e4 { [%clk 0:00:30] } 1... e5 { [%clk 0:01:00] } 2. Nf3 { [%clk 0:00:10] } 2... Nc6 { [%clk 0:00:58] } 0-1
`

func TestParsePGNSplitsGamesAndFindsOurSide(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	if len(games) != 2 {
		t.Fatalf("parsed %d games, want 2", len(games))
	}
	if games[0].id != "abcd1234" || games[1].id != "wxyz5678" {
		t.Errorf("ids %q and %q", games[0].id, games[1].id)
	}
	if games[0].weAreWhite {
		t.Error("first game: we are Black, not White")
	}
	if !games[1].weAreWhite {
		t.Error("second game: we are White")
	}
	if games[0].timeControl != "300+3" {
		t.Errorf("time control %q", games[0].timeControl)
	}
}

// The cost of a move is the drop in our own clock plus the increment handed
// back for playing it, counted on our readings only. Getting the side wrong
// audits the opponent instead, which is a silent and very convincing way to
// measure nothing.
func TestAuditGameMeasuresOurOwnSpending(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	a, ok := auditGame(games[0])
	if !ok {
		t.Fatal("first game was not auditable")
	}
	// We are Black: 5:00 then 4:50 then 4:40, so two moves, each costing
	// ten seconds of clock plus the three second increment.
	if a.moves != 2 {
		t.Fatalf("audited %d moves, want 2", a.moves)
	}
	if a.meanSpentMS != 13000 {
		t.Errorf("mean spend %dms, want 13000 (10s of clock plus a 3s increment)", a.meanSpentMS)
	}
	if a.lowestMS != 280000 {
		t.Errorf("lowest %dms, want 280000", a.lowestMS)
	}
}

func TestAuditGameReportsATimeForfeit(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	a, ok := auditGame(games[1])
	if !ok {
		t.Fatal("second game was not auditable")
	}
	if !a.flagged {
		t.Error("a game whose Termination is a time forfeit was not reported as one")
	}
	// We are White here: 0:30 then 0:10, so the clock is ours, not the
	// opponent's healthy 1:00.
	if a.lowestMS != 10000 {
		t.Errorf("lowest %dms, want 10000; a higher number means the opponent's clock was audited", a.lowestMS)
	}
}

func TestAuditGameSkipsAGameWithoutClocks(t *testing.T) {
	noClocks := `[Event "x"]
[Site "https://lichess.org/nope0000"]
[White "TidyMazeBot"]
[TimeControl "300+3"]

1. e4 e5 2. Nf3 Nc6 0-1
`
	games := parsePGN(strings.NewReader(noClocks), "tidymazebot")
	if len(games) != 1 {
		t.Fatalf("parsed %d games, want 1", len(games))
	}
	if _, ok := auditGame(games[0]); ok {
		t.Error("a game with no clocks was audited; it has nothing to measure")
	}
}

func TestAuditGameSkipsAnUnclockedTimeControl(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	g := games[0]
	g.timeControl = "-"
	if _, ok := auditGame(g); ok {
		t.Error("a game with no time control was audited")
	}
}

// The median gap between what a move really cost and what the rule allowed
// is the number that matters for fast games: it is the part of a move that
// no budget can shrink. The mean hides it, since one long think that
// returned early offsets many moves that each paid a fixed cost too much.
func TestAuditGameReportsTheMedianOverheadNotJustTheMean(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	a, ok := auditGame(games[0])
	if !ok {
		t.Fatal("game was not auditable")
	}
	// Two moves, each costing 13s. The rule's own budget at those clocks
	// decides the gap, so assert it is consistent with the reported means
	// rather than restating the formula here, which is the copy this tool
	// exists to avoid.
	if a.medianOverMS < a.meanSpentMS-a.meanBudgetMS-1000 ||
		a.medianOverMS > a.meanSpentMS-a.meanBudgetMS+1000 {
		t.Errorf("median overhead %dms is nowhere near mean spent minus mean allowed (%dms)",
			a.medianOverMS, a.meanSpentMS-a.meanBudgetMS)
	}
}

func TestSecsFormatsMilliseconds(t *testing.T) {
	for _, c := range []struct {
		ms   int64
		want string
	}{{0, "0.0s"}, {1500, "1.5s"}, {60000, "60.0s"}, {-500, "-0.5s"}} {
		if got := secs(c.ms); got != c.want {
			t.Errorf("secs(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestHeaderValueHandlesAMalformedHeader(t *testing.T) {
	if got := headerValue(`[Site "https://lichess.org/abcd1234"]`); got != "https://lichess.org/abcd1234" {
		t.Errorf("got %q", got)
	}
	if got := headerValue("[Site no quotes]"); got != "" {
		t.Errorf("a header with no quotes gave %q, want empty", got)
	}
}

func TestReportCountsLowAndFlaggedGames(t *testing.T) {
	games := parsePGN(strings.NewReader(twoGames), "tidymazebot")
	var buf strings.Builder
	// A floor above the blitz game's lowest clock so both games count as
	// low, and the bullet game is the one lost on time.
	audited, low, flagged := report(&buf, games, 300000)
	if audited != 2 {
		t.Errorf("audited %d games, want 2", audited)
	}
	if low != 2 {
		t.Errorf("counted %d low games, want 2", low)
	}
	if flagged != 1 {
		t.Errorf("counted %d time forfeits, want 1", flagged)
	}
	out := buf.String()
	if !strings.Contains(out, "FLAGGED") || !strings.Contains(out, "LOW") {
		t.Errorf("table did not mark the games:\n%s", out)
	}
	if !strings.Contains(out, "abcd1234") || !strings.Contains(out, "wxyz5678") {
		t.Errorf("table is missing a game:\n%s", out)
	}
}

// clockaudit links the budget rule it measures against, so a binary built
// before a change to that rule scores every game against the old one and
// reports the difference as overhead. A 1800+5 game read "budget 15.0s,
// spent 31.2s, overhead 17.0s" from a two hour old build, and 30.7s /
// 31.2s / 1.5s once rebuilt. The fingerprint exists so that is visible in
// the first line of output rather than discovered in the numbers.
func TestTheRuleFingerprintTracksTheRealRule(t *testing.T) {
	// A slow control and a fast one, because the two halves of the rule
	// move independently: the ceiling scales with the clock above 750s and
	// is flat below it.
	slow := lichessbot.MoveTimeBudget(1800000, 5000)
	fast := lichessbot.MoveTimeBudget(120000, 1000)
	if slow <= fast {
		t.Errorf("a 1800s clock budgets %v and a 120s clock %v; the slow one must be larger or the fingerprint cannot show a rule change", slow, fast)
	}
	// The slow control must be past the old flat 15s ceiling, which is the
	// specific staleness this catches.
	if slow <= 15*time.Second {
		t.Errorf("1800s+5s budgets %v, still at or below the old flat cap; either the rule regressed or this test is measuring nothing", slow)
	}
}

package lichessbot

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"chess/engine"
)

// A move counts as out of book from the first one the book did not supply,
// and every move of ours after that counts too, a later book hit included.
// It is keyed on the ply rather than on how many times it was asked, so a
// position searched again (a reconnect, a released claim) gets the same k.
func TestTheSessionCountsOurMovesSinceTheBookRanOut(t *testing.T) {
	sess := &gameSession{}
	for _, step := range []struct {
		plies  int
		inBook bool
		want   int
	}{
		{0, true, 0},
		{2, true, 0},
		{4, false, 0},
		{4, false, 0},
		{6, false, 1},
		{8, true, 2},
		{24, false, 10},
	} {
		if got := sess.movesOutOfBook(step.plies, step.inBook); got != step.want {
			t.Errorf("at ply %d (book hit %v): %d moves out of book, want %d", step.plies, step.inBook, got, step.want)
		}
	}
}

// The clock handed to the rule is ours and theirs, from the colour we play.
func TestClockForReadsBothSides(t *testing.T) {
	st := gameState{WhiteTimeMS: 60000, BlackTimeMS: 15000, WhiteIncMS: 1000, BlackIncMS: 2000}
	if got, want := clockFor("white", st, 3), (Clock{OurMS: 60000, OppMS: 15000, IncMS: 1000, OppIncMS: 2000, MovesOutOfBook: 3}); got != want {
		t.Errorf("white: %+v, want %+v", got, want)
	}
	if got, want := clockFor("black", st, 7), (Clock{OurMS: 15000, OppMS: 60000, IncMS: 2000, OppIncMS: 1000, MovesOutOfBook: 7}); got != want {
		t.Errorf("black: %+v, want %+v", got, want)
	}
}

// The bot plays by the new rule, fed the opponent's clock and the count.
func TestEffectivePlayerUsesTheOpponentClockAndTheBookCount(t *testing.T) {
	base := engine.Player{TimeBudget: time.Second, Depth: 3}
	overhead := newOverheadEstimate()
	theyAreShort := gameState{WhiteTimeMS: 60000, BlackTimeMS: 15000, WhiteIncMS: 1000, BlackIncMS: 1000}
	weAreShort := gameState{WhiteTimeMS: 60000, BlackTimeMS: 240000, WhiteIncMS: 1000, BlackIncMS: 1000}

	short := effectivePlayer(base, "white", theyAreShort, "blitz", overhead, 10).TimeBudget
	long := effectivePlayer(base, "white", weAreShort, "blitz", overhead, 10).TimeBudget
	if short <= long {
		t.Errorf("opponent short of time got %v, us short of time %v; the first must think longer", short, long)
	}
	if want := moveBudget(clockFor("white", theyAreShort, 10), overhead); short != want {
		t.Errorf("effective budget %v, want the new rule's %v", short, want)
	}
	fresh := effectivePlayer(base, "white", theyAreShort, "blitz", overhead, 0).TimeBudget
	if fresh <= short {
		t.Errorf("first move out of book got %v, tenth %v; the first must think longer", fresh, short)
	}
}

// End to end through the game stream: a book hit, then three moves out of
// book, each against a different opponent clock. The budgets logged must
// be the rule's for that clock and for k = 0, 0, 1, 2: the book hit does
// not start the count, the first search does.
func TestTheBotBudgetsEachMoveWithTheOpponentClockAndTheBookCount(t *testing.T) {
	bookPath := t.TempDir() + "/book.txt"
	if err := os.WriteFile(bookPath, []byte("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2e4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	book, err := engine.LoadBook(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	player := engine.Strong(1)
	player.Book = book

	type event struct {
		moves        string
		ourMS, oppMS int64
		k            int
	}
	events := []event{
		{"", 22000, 22000, 0},
		{"e2e4 e7e5", 22000, 5500, 0},
		{"e2e4 e7e5 g1f3 b8c6", 22000, 88000, 1},
		{"e2e4 e7e5 g1f3 b8c6 f1c4 g8f6", 22000, 22000, 2},
	}
	state := func(e event, status string) string {
		return fmt.Sprintf(`{"type":"gameState","moves":%q,"status":%q,"wtime":%d,"btime":%d,"winc":0,"binc":0}`, e.moves, status, e.ourMS, e.oppMS)
	}
	lines := []string{`{"type":"gameFull","id":"gb","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","speed":"blitz","state":` + state(events[0], "started") + `}`}
	for _, e := range events[1:] {
		lines = append(lines, state(e, "started"))
	}
	lines = append(lines, state(event{moves: events[3].moves + " d2d3", ourMS: 22000, oppMS: 22000}, "resign"))

	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"gb"}}` + "\n"
	f.streams["/api/bot/game/stream/gb"] = strings.Join(lines, "\n") + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: player, Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "game gb: finished") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	got := regexp.MustCompile(`\(budget [^)]*\)`).FindAllString(buf.String(), -1)
	var want []string
	for _, e := range events {
		budget := moveBudget(Clock{OurMS: e.ourMS, OppMS: e.oppMS, MovesOutOfBook: e.k}, newOverheadEstimate())
		want = append(want, fmt.Sprintf("(budget %s, opponent %s, %d out of book)",
			budget.Round(time.Millisecond), time.Duration(e.oppMS)*time.Millisecond, e.k))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("logged budgets:\n%s\nwant:\n%s\nlog:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"), buf.String())
	}
}

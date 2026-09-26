package lichessbot

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"chess/engine"
)

// liveGameAPI serves one game's stream the way lichess does: every connection
// starts with body and then stays open with nothing more to say until its
// context ends. The event stream and the record of posts are fakeAPI's.
type liveGameAPI struct {
	*fakeAPI
	path  string
	body  string
	mu    sync.Mutex
	opens int
}

func (l *liveGameAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	if path != l.path {
		return l.fakeAPI.streamNDJSON(ctx, path)
	}
	l.mu.Lock()
	l.opens++
	l.mu.Unlock()
	return io.NopCloser(io.MultiReader(strings.NewReader(l.body), blockingReader{ctx: ctx})), nil
}

func (l *liveGameAPI) openCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opens
}

func (l *liveGameAPI) movePosts() []string {
	var out []string
	for _, p := range l.postedPaths() {
		if strings.Contains(p, "/move/") {
			out = append(out, p)
		}
	}
	return out
}

// gameFullLine is the first event of a game stream, with the bot playing
// botColor, the given move list, and clockMS on both clocks (0 for none).
func gameFullLine(id, botColor, moves string, clockMS int64) string {
	white, black := "tidymazebot", "opponent"
	if botColor == "black" {
		white, black = black, white
	}
	return fmt.Sprintf(`{"type":"gameFull","id":%q,"white":{"id":%q},"black":{"id":%q},"initialFen":"startpos","speed":"blitz","state":{"type":"gameState","moves":%q,"status":"started","wtime":%d,"btime":%d}}`,
		id, white, black, moves, clockMS, clockMS) + "\n"
}

// Lichess announces every ongoing game again as gameStart each time the event
// stream connects. A reconnect while a game is being played must not start a
// second loop for it: two loops search every move twice on the same cores,
// both post, and the one game counts twice against MaxGames.
func TestAnEventStreamReconnectDoesNotStartASecondLoopForAGameInPlay(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"s1"}}` + "\n"
	l := &liveGameAPI{fakeAPI: f, path: "/api/bot/game/stream/s1", body: gameFullLine("s1", "white", "", 0)}
	b := &Bot{API: l, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Two connections to the event stream, each announcing the game, while
	// the game's own stream stays open.
	for i := 0; i < 2; i++ {
		if err := b.runOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 2*time.Second, "the first move", func() bool { return len(l.movePosts()) >= 1 })
	time.Sleep(300 * time.Millisecond)
	if n := b.gamesInPlay.Load(); n != 1 {
		t.Errorf("games in play %d, want 1 for one game", n)
	}
	if got := l.openCount(); got != 1 {
		t.Errorf("game stream opened %d times, want 1", got)
	}
	if got := l.movePosts(); len(got) != 1 {
		t.Errorf("move posts %v, want exactly 1 for one turn", got)
	}
}

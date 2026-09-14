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

// resumableGameAPI hands out a different body each time the same game stream
// is opened, which is what a mid-game drop and a reconnect look like from the
// client side. Anything it runs out of bodies for blocks until the context
// ends, the way a real stream sits open waiting for the opponent.
type resumableGameAPI struct {
	*fakeAPI
	path   string
	mu     sync.Mutex
	bodies []io.Reader
	opens  int
}

func (r *resumableGameAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	if path != r.path {
		return r.fakeAPI.streamNDJSON(ctx, path)
	}
	r.mu.Lock()
	r.opens++
	var body io.Reader
	if len(r.bodies) > 0 {
		body, r.bodies = r.bodies[0], r.bodies[1:]
	}
	r.mu.Unlock()
	if body == nil {
		return io.NopCloser(blockingReader{ctx: ctx}), nil
	}
	return io.NopCloser(body), nil
}

func (r *resumableGameAPI) openCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.opens
}

// blockingReader models an open stream with nothing to say yet: it does not
// return EOF, because a real one does not either. A fake that returns EOF
// instead would let a broken client pass.
type blockingReader struct{ ctx context.Context }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

// failingThenEOF yields its bytes and then fails, the way a dropped socket
// does: the bytes already read are good, the stream is simply gone.
type failingReader struct{ r io.Reader }

func (f failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, fmt.Errorf("read tcp: errno 65535")
	}
	return n, err
}

// A game stream that drops mid-game must be reconnected to, not treated as
// the game ending. Observed on lichess 2026-09-14: game rUZ1clR4's stream
// died after 2m24s with errno 65535, the bot logged "finished" and stopped
// reading, and the game was then lost on time with the bot still on the
// board. The opponent's clock was never the problem: nobody was listening.
func TestBotReconnectsToAGameWhoseStreamDrops(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g90"}}` + "\n"
	// First connection: the game is under way and it is the opponent to
	// move, so the bot posts nothing, then the socket dies.
	first := failingReader{r: strings.NewReader(
		`{"type":"gameFull","id":"g90","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"state":{"type":"gameState","moves":"","status":"started"}}` + "\n")}
	// Second connection: lichess replays the game, now with our move due.
	second := strings.NewReader(
		`{"type":"gameFull","id":"g90","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"state":{"type":"gameState","moves":"e2e4","status":"started"}}` + "\n")

	r := &resumableGameAPI{fakeAPI: f, path: "/api/bot/game/stream/g90", bodies: []io.Reader{first, second}}
	b := &Bot{API: r, Player: engine.Strong(1), Username: "tidymazebot",
		Log: silentLogger(), ReconnectDelay: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.runOnce(ctx); err != nil {
		t.Fatal(err)
	}

	waitForPosts(t, f, 1)
	if got := r.openCount(); got < 2 {
		t.Errorf("game stream opened %d time(s), want at least 2: a dropped stream must be reconnected to", got)
	}
	for _, p := range f.postedPaths() {
		if strings.HasPrefix(p, "/api/bot/game/g90/move/") {
			return
		}
	}
	t.Errorf("no move posted after the reconnect: %v", f.postedPaths())
}

// The other half: once the game really is over, the bot must stop opening
// the stream. Without this a reconnect loop turns every finished game into
// an endless one.
func TestBotStopsReconnectingOnceTheGameIsOver(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g91"}}` + "\n"
	body := strings.NewReader(
		`{"type":"gameFull","id":"g91","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"state":{"type":"gameState","moves":"e2e4","status":"started"}}` + "\n" +
			`{"type":"gameState","moves":"e2e4 e7e5","status":"resign","winner":"black"}` + "\n")
	r := &resumableGameAPI{fakeAPI: f, path: "/api/bot/game/stream/g91", bodies: []io.Reader{body}}
	b := &Bot{API: r, Player: engine.Strong(1), Username: "tidymazebot",
		Log: silentLogger(), ReconnectDelay: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
	time.Sleep(50 * time.Millisecond)
	if got := r.openCount(); got != 1 {
		t.Errorf("game stream opened %d times, want 1: a finished game must not be reopened", got)
	}
}

// A stream that never delivers anything must not be retried for ever. This
// is what stops a game id lichess has forgotten from spinning in the
// background until the process dies.
func TestBotGivesUpOnAGameStreamThatNeverDelivers(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g92"}}` + "\n"
	w := &erroringStreamAPI{fakeAPI: f, errorPath: "/api/bot/game/stream/g92"}
	var buf syncBuf
	b := &Bot{API: w, Player: engine.Strong(1), Username: "tidymazebot",
		Log: newBufLogger(&buf), ReconnectDelay: time.Millisecond}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.runOnce(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runOnce never returned: the game reconnect loop does not give up")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), "giving up") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "giving up") {
		t.Errorf("log never said it gave up: %q", buf.String())
	}
}

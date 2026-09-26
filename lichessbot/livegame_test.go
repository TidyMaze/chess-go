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
// starts with body and then stays open, delivering whatever is pushed on
// lines, until its context ends. The event stream and the record of posts
// are fakeAPI's; onPost, when set, decides the answer to a post fakeAPI let
// through.
type liveGameAPI struct {
	*fakeAPI
	path   string
	body   string
	lines  chan string
	onPost func(path string) error
	mu     sync.Mutex
	opens  int
}

func (l *liveGameAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	if path != l.path {
		return l.fakeAPI.streamNDJSON(ctx, path)
	}
	l.mu.Lock()
	l.opens++
	l.mu.Unlock()
	return io.NopCloser(io.MultiReader(strings.NewReader(l.body), &lineReader{ctx: ctx, lines: l.lines})), nil
}

func (l *liveGameAPI) postForm(path string, form string) error {
	if err := l.fakeAPI.postForm(path, form); err != nil {
		return err
	}
	if l.onPost != nil {
		return l.onPost(path)
	}
	return nil
}

// lineReader is blockingReader that also hands over each line pushed on
// lines. A nil channel makes it exactly blockingReader.
type lineReader struct {
	ctx   context.Context
	lines <-chan string
	buf   []byte
}

func (r *lineReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case line := <-r.lines:
			r.buf = []byte(line + "\n")
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func gameStateLine(moves string) string {
	return fmt.Sprintf(`{"type":"gameState","moves":%q,"status":"started"}`, moves)
}

// referee plays lichess's side of one game on a liveGameAPI with lines: it
// keeps the real move list, refuses a move that is out of turn or illegal as
// lichess does ("Not your turn, or game already over"), and after each move
// it accepts pushes the new state, then the opponent's next reply if any.
type referee struct {
	mu       sync.Mutex
	moves    string
	accepted int
	rejected int
}

func newReferee(l *liveGameAPI, botColor string, replies ...string) *referee {
	r := &referee{}
	l.onPost = func(path string) error {
		if !strings.Contains(path, "/move/") {
			return nil
		}
		uci := path[strings.LastIndex(path, "/")+1:]
		r.mu.Lock()
		defer r.mu.Unlock()
		g, err := applyMovesString("startpos", r.moves)
		legal := false
		if err == nil && isOurTurn(g, botColor) {
			for _, m := range g.AllLegalMoves(g.Turn) {
				legal = legal || moveUCIForLichess(g, m) == uci
			}
		}
		if !legal {
			r.rejected++
			return fmt.Errorf("POST %s: 400 Bad Request: Not your turn, or game already over", path)
		}
		r.accepted++
		r.moves = strings.TrimSpace(r.moves + " " + uci)
		l.lines <- gameStateLine(r.moves)
		if len(replies) > 0 {
			r.moves += " " + replies[0]
			replies = replies[1:]
			l.lines <- gameStateLine(r.moves)
		}
		return nil
	}
	return r
}

func (r *referee) counts() (accepted, rejected int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accepted, r.rejected
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

// An opponent who offers a draw (or proposes a takeback) while the bot thinks
// makes lichess queue a gameState with the move list the bot is answering.
// Read after the move went out, it must not start a second search: that
// search blocks the stream while the real reply waits on the bot's clock,
// and its move is then refused or, worse, accepted as the answer to a
// position the bot never looked at.
func TestAQueuedDrawOfferDoesNotMakeTheBotAnswerTheSameMovesTwice(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"d1"}}` + "\n"
	l := &liveGameAPI{fakeAPI: f, path: "/api/bot/game/stream/d1", lines: make(chan string, 16),
		body: gameFullLine("d1", "white", "", 0) + `{"type":"gameState","moves":"","status":"started","bdraw":true}` + "\n"}
	r := newReferee(l, "white", "g8f6")
	b := &Bot{API: l, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "two accepted moves", func() bool { a, _ := r.counts(); return a >= 2 })
	time.Sleep(300 * time.Millisecond)
	if a, rej := r.counts(); a != 2 || rej != 0 {
		t.Errorf("move posts %v: %d accepted, %d refused; want 2 accepted and none refused", l.movePosts(), a, rej)
	}
}

package lichessbot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"chess/engine"
)

// fakeAPI drives Bot end to end without a network: the event stream and
// each game's stream are pre-seeded lines, and every POST is recorded so
// a test can assert exactly what the bot tried to do.
type fakeAPI struct {
	mu            sync.Mutex
	streams       map[string]string // path -> NDJSON body
	streamErr     map[string]error  // path -> error streamNDJSON returns instead
	posts         []string          // path, in call order
	postErr       map[string]error
	postErrPrefix map[string]error
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{streams: map[string]string{}, streamErr: map[string]error{}, postErr: map[string]error{}, postErrPrefix: map[string]error{}}
}

func (f *fakeAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	body, ok := f.streams[path]
	err, hasErr := f.streamErr[path]
	f.mu.Unlock()
	if hasErr {
		return nil, err
	}
	if !ok {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (f *fakeAPI) postForm(path string, form string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, path)
	if err, ok := f.postErr[path]; ok {
		return err
	}
	for prefix, err := range f.postErrPrefix {
		if strings.HasPrefix(path, prefix) {
			return err
		}
	}
	return nil
}

func (f *fakeAPI) postedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posts...)
}

// A full round trip: the account stream offers a challenge, the bot
// accepts it, then gets a gameStart, then the game stream hands it a
// gameFull with us to move, and the bot must post exactly one move.
func TestBotAcceptsAChallengeAndPlaysItsMove(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = strings.Join([]string{
		`{"type":"challenge","challenge":{"id":"c1","rated":true,"speed":"blitz","variant":{"key":"standard"},"challenger":{"title":""}}}`,
		`{"type":"gameStart","game":{"id":"g1"}}`,
	}, "\n") + "\n"
	f.streams["/api/bot/game/stream/g1"] = `{"type":"gameFull","id":"g1","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 2)

	posts := f.postedPaths()
	if len(posts) != 2 {
		t.Fatalf("posts: %v, want 2 (accept, then one move)", posts)
	}
	if posts[0] != "/api/challenge/c1/accept" {
		t.Errorf("first post %q, want the accept endpoint", posts[0])
	}
	if !strings.HasPrefix(posts[1], "/api/bot/game/g1/move/") {
		t.Errorf("second post %q, want a move on game g1", posts[1])
	}
}

// A challenge outside the accept policy (a variant here) must be
// declined, not accepted, and the game must never be joined.
func TestBotDeclinesAVariantChallenge(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"c2","rated":true,"speed":"blitz","variant":{"key":"chess960"},"challenger":{"title":""}}}` + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts := f.postedPaths()
	if len(posts) != 1 || posts[0] != "/api/challenge/c2/decline" {
		t.Errorf("posts: %v, want exactly one decline", posts)
	}
}

// When it is not our turn, no move is posted; only a gameState update
// that hands the turn back to us should trigger one.
func TestBotWaitsForItsOwnTurn(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g3"}}` + "\n"
	// We are black; after one white move it is our turn, and a second
	// gameState with a black reply should trigger no further move here
	// since maybeMove only reacts to the last posted state, one at a time.
	f.streams["/api/bot/game/stream/g3"] = strings.Join([]string{
		`{"type":"gameFull","id":"g3","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}`,
		`{"type":"gameState","moves":"e2e4","status":"started"}`,
	}, "\n") + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
	posts := f.postedPaths()
	if len(posts) != 1 {
		t.Fatalf("posts: %v, want exactly one move (the gameFull event has white to move, not us)", posts)
	}
}

// A game already finished by the time its stream is read must not be
// played into: no move should ever be posted for it.
func TestBotDoesNotMoveInAFinishedGame(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g4"}}` + "\n"
	f.streams["/api/bot/game/stream/g4"] = `{"type":"gameFull","id":"g4","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"mate"}}` + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none for a game already over", posts)
	}
}

// If our username matches neither side of the game, the bot must refuse
// to move rather than guess a color and play into the wrong seat.
func TestBotRefusesToPlayWhenUsernameMatchesNeitherSide(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g5"}}` + "\n"
	f.streams["/api/bot/game/stream/g5"] = `{"type":"gameFull","id":"g5","white":{"id":"alice"},"black":{"id":"bob"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none when we are not a player in this game", posts)
	}
}

// A failed accept call must be visible in the log, not swallowed.
func TestBotLogsAFailedAccept(t *testing.T) {
	f := newFakeAPI()
	f.postErr["/api/challenge/c6/accept"] = fmt.Errorf("simulated failure")
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"c6","rated":true,"speed":"blitz","variant":{"key":"standard"},"challenger":{"title":""}}}` + "\n"

	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "simulated failure") {
		t.Errorf("log did not mention the accept failure: %q", buf.String())
	}
}

func waitForPosts(t *testing.T, f *fakeAPI, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(f.postedPaths()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d posts, got %v", n, f.postedPaths())
}

func silentLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// syncBuf lets a test read what a background goroutine logged without
// racing the write: bytes.Buffer alone is not safe for that, and Bot has
// no shutdown, so a game goroutine can still be logging after Run
// returns.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func newBufLogger(buf *syncBuf) *log.Logger { return log.New(buf, "", 0) }

// An unparseable line on the account stream must be logged and skipped,
// not treated as a fatal error that tears down the whole connection.
func TestBotSkipsAnUnreadableAccountEvent(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = "not json at all\n" +
		`{"type":"gameStart","game":{"id":"g7"}}` + "\n"
	f.streams["/api/bot/game/stream/g7"] = `{"type":"gameFull","id":"g7","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"

	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
	if !strings.Contains(buf.String(), "unreadable event") {
		t.Errorf("the garbage line was not logged: %q", buf.String())
	}
}

// A challenge event whose JSON does not even parse as an object must be
// logged and dropped, not accepted or declined blind.
func TestBotSkipsAnUnreadableChallenge(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":123}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.postedPaths()) != 0 {
		t.Errorf("posts: %v, want none for an unreadable challenge", f.postedPaths())
	}
	if !strings.Contains(buf.String(), "unreadable challenge") {
		t.Errorf("log: %q, want it to mention the unreadable challenge", buf.String())
	}
}

// A decline call that fails must be logged, same as a failed accept.
func TestBotLogsAFailedDecline(t *testing.T) {
	f := newFakeAPI()
	f.postErr["/api/challenge/c8/decline"] = fmt.Errorf("decline boom")
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"c8","rated":true,"speed":"blitz","variant":{"key":"atomic"},"challenger":{"title":""}}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "decline boom") {
		t.Errorf("log did not mention the decline failure: %q", buf.String())
	}
}

// A gameState arriving before its game's gameFull must be ignored: there
// is no initial FEN or side assignment to play from yet.
func TestBotIgnoresAGameStateBeforeGameFull(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g9"}}` + "\n"
	f.streams["/api/bot/game/stream/g9"] = `{"type":"gameState","moves":"e2e4","status":"started"}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none before a gameFull has been seen", posts)
	}
}

// A move a game stream fails to open must be logged and must not panic
// the goroutine it runs in.
func TestBotLogsWhenAGameStreamFailsToOpen(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"gmissing"}}` + "\n"
	// No stream registered for gmissing: streamNDJSON returns an empty
	// body, which ends the game loop with nothing played, not an error.
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none for a game with no data", posts)
	}
}

// A failed move post must be logged with the move and the game id, since
// a silently dropped move looks like the bot going quiet mid-game.
func TestBotLogsAFailedMovePost(t *testing.T) {
	f := newFakeAPI()
	f.postErrPrefix["/api/bot/game/g10/move/"] = fmt.Errorf("move rejected")
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g10"}}` + "\n"
	f.streams["/api/bot/game/stream/g10"] = `{"type":"gameFull","id":"g10","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(buf.String(), "move rejected") {
		t.Errorf("log did not mention the failed move: %q", buf.String())
	}
}

// A position with no legal move (stalemate) on our turn must be logged
// and must not crash trying to play a move that does not exist.
func TestBotLogsWhenThereIsNoLegalMove(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g11"}}` + "\n"
	// A textbook stalemate: black to move, no legal move, not in check.
	f.streams["/api/bot/game/stream/g11"] = `{"type":"gameFull","id":"g11","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"initialFen":"7k/5Q2/6K1/8/8/8/8/8 b - - 0 1","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(buf.String(), "no legal move") {
		t.Errorf("log: %q, want it to mention no legal move", buf.String())
	}
}

// logf must fall back to stdout when no Logger was given, so a bot run
// from a quick script still shows what it did.
func TestLogfFallsBackToStdoutWhenLogIsNil(t *testing.T) {
	b := &Bot{}
	b.logf("fallback %d", 1)
}

// A failure to open the account event stream must be returned, not
// swallowed: the caller (the command's main loop) needs to know the bot
// stopped running rather than silently doing nothing.
func TestRunReturnsAnErrorWhenTheAccountStreamFailsToOpen(t *testing.T) {
	f := newFakeAPI()
	f.streamErr["/api/stream/event"] = fmt.Errorf("connection refused")
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err == nil {
		t.Error("expected an error when the account stream fails to open")
	}
}

// A gameStart event with unreadable JSON must be logged and skipped, the
// same as an unreadable challenge, rather than crashing the whole loop.
func TestBotSkipsAnUnreadableGameStart(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":123}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "unreadable gameStart") {
		t.Errorf("log: %q, want it to mention the unreadable gameStart", buf.String())
	}
}

// A game whose stream fails to open must be logged, not silently dropped.
func TestBotLogsWhenAGameStreamErrors(t *testing.T) {
	f := newFakeAPI()
	f.streamErr["/api/bot/game/stream/g12"] = fmt.Errorf("stream boom")
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g12"}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), "stream boom") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "stream boom") {
		t.Errorf("log did not mention the game stream failure: %q", buf.String())
	}
}

// A gameFull whose initial FEN is malformed must be logged, not crash the
// game goroutine or leave it stuck forever.
func TestBotLogsAMalformedInitialFEN(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g13"}}` + "\n"
	f.streams["/api/bot/game/stream/g13"] = `{"type":"gameFull","id":"g13","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"not a fen","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && buf.String() == "" {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "fen") && !strings.Contains(buf.String(), "FEN") {
		t.Errorf("log did not mention the bad FEN: %q", buf.String())
	}
}

// errReader always fails, standing in for a connection that drops
// mid-stream so eachLine's forwarded scanner error is exercised.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("read boom") }

func TestBotLogsWhenTheGameStreamDropsMidRead(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g14"}}` + "\n"
	// Swap in a reader that errors instead of a string body, by routing
	// through a small wrapper api that returns errReader for this one path.
	w := &erroringStreamAPI{fakeAPI: f, errorPath: "/api/bot/game/stream/g14"}
	var buf syncBuf
	b := &Bot{API: w, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf)}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), "stream ended") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "stream ended") {
		t.Errorf("log did not mention the dropped stream: %q", buf.String())
	}
}

type erroringStreamAPI struct {
	*fakeAPI
	errorPath string
}

func (w *erroringStreamAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == w.errorPath {
		return io.NopCloser(errReader{}), nil
	}
	return w.fakeAPI.streamNDJSON(ctx, path)
}

// A line inside a game stream that is not even valid JSON must be
// skipped, not crash the game's goroutine.
func TestBotSkipsAnUnreadableLineInAGameStream(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g15"}}` + "\n"
	f.streams["/api/bot/game/stream/g15"] = "garbage\n" +
		`{"type":"gameFull","id":"g15","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"state":{"type":"gameState","moves":"","status":"started"}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
}

// A gameFull event whose JSON does not match the expected shape (id is a
// number instead of a string here) must be skipped rather than crash.
func TestBotSkipsAMalformedGameFull(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g16"}}` + "\n"
	f.streams["/api/bot/game/stream/g16"] = `{"type":"gameFull","id":123}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none for a malformed gameFull", posts)
	}
}

// A gameState event whose JSON does not match the expected shape must be
// skipped the same way, after a valid gameFull already arrived.
func TestBotSkipsAMalformedGameState(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g17"}}` + "\n"
	f.streams["/api/bot/game/stream/g17"] = strings.Join([]string{
		`{"type":"gameFull","id":"g17","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"mate"}}`,
		`{"type":"gameState","status":123}`,
	}, "\n") + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none (the first state is already mate, the second is malformed)", posts)
	}
}

// initialFen is omitted by lichess for a game starting from the normal
// position; an empty string there must fall back to the start position,
// not be handed to applyMovesString as if it were a FEN.
func TestBotTreatsAnEmptyInitialFenAsStartpos(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"gameStart","game":{"id":"g18"}}` + "\n"
	f.streams["/api/bot/game/stream/g18"] = `{"type":"gameFull","id":"g18","white":{"id":"tidymazebot"},"black":{"id":"opponent"},"state":{"type":"gameState","moves":"","status":"started"}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPosts(t, f, 1)
}

// Lichess echoes our own outgoing challenges on the same event stream it
// uses for incoming ones. Neither accept nor decline exists for a
// challenge we sent ourselves, so the bot must ignore it rather than post
// a call that 404s. Observed against the real API 2026-09-13: TidyMazeBot
// logged "accept 9PZvBKIy failed: 404 Not Found" for a challenge it had
// just sent to maia5 itself.
func TestBotIgnoresItsOwnOutgoingChallenge(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"c19","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"tidymazebot","title":"BOT"}}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none for our own outgoing challenge", posts)
	}
}

// The champion's own clock in champion.json is a fixed budget for
// measurement races; a real game must use the clock lichess actually
// sends for that move instead.
func TestEffectivePlayerUsesTheLiveClockOverTheChampionsFixedBudget(t *testing.T) {
	base := engine.Player{TimeBudget: 999 * time.Hour, Depth: 3}
	st := gameState{WhiteTimeMS: 30000, WhiteIncMS: 0}
	got := effectivePlayer(base, "white", st, "blitz", newOverheadEstimate())
	if got.TimeBudget == base.TimeBudget {
		t.Error("the live clock did not override the champion's fixed time budget")
	}
	if got.TimeBudget <= 0 || got.TimeBudget > 30*time.Second {
		t.Errorf("effective budget %v is not derived from a 30s clock", got.TimeBudget)
	}
	if got.Depth != base.Depth {
		t.Error("depth changed; only the time budget should be overridden")
	}
}

// A game with no clock at all (correspondence) must keep the champion's
// own configured budget rather than being handed zero thinking time.
func TestEffectivePlayerKeepsTheChampionsBudgetWithNoClock(t *testing.T) {
	base := engine.Player{TimeBudget: 5 * time.Second}
	got := effectivePlayer(base, "white", gameState{}, "blitz", newOverheadEstimate())
	if got.TimeBudget != base.TimeBudget {
		t.Errorf("got %v, want the champion's own %v kept with no clock data", got.TimeBudget, base.TimeBudget)
	}
}

// Every game runs its own search, and champion.json asks for 8 threads.
// With seven games in flight that is 56 search threads on a machine with
// 4 performance cores, so every game searches far shallower than the
// champion was ever calibrated at. Observed live: seven concurrent games.
// Past the cap a challenge must be declined rather than accepted and
// played badly.
func TestBotDeclinesChallengesWhenAtItsGameLimit(t *testing.T) {
	f := newFakeAPI()
	// One game starts and stays open (its stream never closes), then a
	// challenge arrives while it is still running.
	f.streams["/api/stream/event"] = strings.Join([]string{
		`{"type":"gameStart","game":{"id":"gbusy"}}`,
		`{"type":"challenge","challenge":{"id":"clate","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"someoneelse"}}}`,
	}, "\n") + "\n"
	f.streams["/api/bot/game/stream/gbusy"] = `{"type":"gameFull","id":"gbusy","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"initialFen":"startpos","state":{"type":"gameState","moves":"","status":"started"}}` + "\n"

	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger(), MaxGames: 1}
	b.gamesInPlay.Store(1) // a game already occupying the only slot
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts := f.postedPaths()
	for _, p := range posts {
		if strings.Contains(p, "/accept") {
			t.Errorf("accepted a challenge while at the game limit: %v", posts)
		}
	}
	found := false
	for _, p := range posts {
		if p == "/api/challenge/clate/decline" {
			found = true
		}
	}
	if !found {
		t.Errorf("posts: %v, want a decline for the challenge that arrived at the limit", posts)
	}
}

// With room to spare, a challenge is accepted exactly as before.
func TestBotAcceptsWhenBelowItsGameLimit(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"cfree","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"someoneelse"}}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger(), MaxGames: 4}
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts := f.postedPaths()
	if len(posts) != 1 || posts[0] != "/api/challenge/cfree/accept" {
		t.Errorf("posts: %v, want a single accept", posts)
	}
}

// MaxGames zero means no limit, the behaviour everything else in this
// package was written against.
func TestBotWithNoGameLimitAcceptsAsBefore(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"cnl","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"someoneelse"}}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	b.gamesInPlay.Store(99)
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	posts := f.postedPaths()
	if len(posts) != 1 || posts[0] != "/api/challenge/cnl/accept" {
		t.Errorf("posts: %v, want a single accept with no limit set", posts)
	}
}

// A decline that fails while at the game limit must be logged, the same
// as any other failed decline.
func TestBotLogsAFailedDeclineAtTheGameLimit(t *testing.T) {
	f := newFakeAPI()
	f.postErr["/api/challenge/cbusy/decline"] = fmt.Errorf("limit decline boom")
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"cbusy","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"someoneelse"}}}` + "\n"
	var buf syncBuf
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&buf), MaxGames: 1}
	b.gamesInPlay.Store(1)
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "limit decline boom") {
		t.Errorf("log did not mention the failed decline: %q", buf.String())
	}
}

// Our own outgoing challenge must be ignored even when we are at the
// game limit. The limit check was added ahead of the outgoing check and
// reintroduced exactly the bug the outgoing check exists to prevent:
// lichess has no decline action for a challenge you sent yourself, so
// every one of ours 404'd. Observed live minutes after deploying the cap.
func TestBotIgnoresItsOwnOutgoingChallengeEvenAtTheGameLimit(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/stream/event"] = `{"type":"challenge","challenge":{"id":"cmine","rated":true,"speed":"rapid","variant":{"key":"standard"},"challenger":{"id":"tidymazebot"}}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger(), MaxGames: 1}
	b.gamesInPlay.Store(5)
	if err := b.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := f.postedPaths(); len(posts) != 0 {
		t.Errorf("posts: %v, want none for our own outgoing challenge at the limit", posts)
	}
}

// A correspondence game has no clock at all, so the champion's own race
// budget is the wrong one: on one second a move it played lichess's
// Stockfish at its top level and lost, game N1ok1iNf. It gets a real
// budget instead, and only correspondence does, because inferring "no
// clock" from a missing field would hand a bullet game a fifteen second
// think and flag it.
func TestCorrespondenceGetsARealBudget(t *testing.T) {
	base := engine.Player{TimeBudget: time.Second}
	got := effectivePlayer(base, "white", gameState{}, "correspondence", newOverheadEstimate())
	if got.TimeBudget <= time.Second {
		t.Errorf("correspondence got %v, no better than the race budget", got.TimeBudget)
	}
}

func TestAClockedGameWithNoClockFieldKeepsTheChampionBudget(t *testing.T) {
	base := engine.Player{TimeBudget: time.Second}
	got := effectivePlayer(base, "white", gameState{}, "bullet", newOverheadEstimate())
	if got.TimeBudget != time.Second {
		t.Errorf("bullet with no clock field got %v; it must keep the champion's budget, not an unlimited one", got.TimeBudget)
	}
}

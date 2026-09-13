package lichessbot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"chess/engine"
)

// stubbornStream stays open and says nothing until its context ends. Both
// halves matter. Lichess really does leave an idle event stream silent:
// measured on 2026-09-13, zero bytes in 75 seconds on an HTTP/2 connection
// that was open and healthy the whole time. And a Close from another
// goroutine does not end a read already parked on an HTTP/2 stream's pipe,
// which the live bot's goroutine dump showed the hard way.
type stubbornStream struct {
	ctx    context.Context
	closed chan struct{}
	once   sync.Once
}

func (s *stubbornStream) Read(p []byte) (int, error) {
	<-s.ctx.Done()
	return 0, s.ctx.Err()
}

func (s *stubbornStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// countingAPI hands out a fresh stream on every connection and counts them,
// so a test can prove the bot reconnects, or prove it does not.
type countingAPI struct {
	mu      sync.Mutex
	opens   int
	streams []*stubbornStream
	// newStream builds the body for each open. Nil means a stream that only
	// its context can end.
	newStream func(n int) io.ReadCloser
}

func (c *countingAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opens++
	if c.newStream != nil {
		return c.newStream(c.opens), nil
	}
	s := &stubbornStream{ctx: ctx, closed: make(chan struct{})}
	c.streams = append(c.streams, s)
	return s, nil
}

func (c *countingAPI) postForm(path string, form string) error { return nil }

func (c *countingAPI) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opens
}

// waitFor polls until cond holds or the deadline passes, so a test proves a
// thing happened rather than sleeping for a fixed guess.
func waitFor(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", within, what)
}

// runInBackground starts the bot and hands back a stop func that cancels it
// and waits for Run to return, so no test leaks a running bot.
func runInBackground(t *testing.T, b *Bot) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil after cancel", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not return within 2s of cancelling its context")
		}
	}
}

// An idle event stream is normal, not broken. Lichess sends nothing at all
// while no challenge and no game is happening, so a bot that treats silence
// as death reconnects forever and achieves nothing. Deciding a connection is
// dead belongs to the transport's ping health check, not to a timer up here.
func TestASilentButLiveConnectionIsLeftAlone(t *testing.T) {
	c := &countingAPI{}
	b := &Bot{
		API:            c,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            silentLogger(),
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, time.Second, "the first connection", func() bool { return c.count() >= 1 })
	time.Sleep(300 * time.Millisecond)
	if n := c.count(); n != 1 {
		t.Errorf("opened %d connections to a silent but healthy stream, want 1: silence is not a dropped connection", n)
	}
}

// A stream that ends, which is what a dead connection looks like once the
// transport has torn it down, must bring the bot straight back.
func TestRunReconnectsWhenTheStreamEnds(t *testing.T) {
	c := &countingAPI{newStream: func(int) io.ReadCloser { return io.NopCloser(emptyReader{}) }}
	b := &Bot{
		API:            c,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            silentLogger(),
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, 2*time.Second, "the bot to reconnect after a stream ended", func() bool {
		return c.count() >= 3
	})
}

// Silence in the log is what hid this bug for twelve minutes, so a
// connection that ends has to leave a trace even when nothing errored.
func TestAnEndedConnectionIsLogged(t *testing.T) {
	c := &countingAPI{newStream: func(int) io.ReadCloser { return io.NopCloser(emptyReader{}) }}
	var buf syncBuf
	b := &Bot{
		API:            c,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            newBufLogger(&buf),
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, 2*time.Second, "the end of a connection to reach the log", func() bool {
		return strings.Contains(buf.String(), "reconnecting")
	})
}

// A connection that fails to open must not end the run either: lichess
// returns 429 and 5xx often enough that giving up on one is giving up for
// the day.
func TestRunKeepsTryingWhenAConnectionCannotBeOpened(t *testing.T) {
	failing := &failOpenAPI{}
	b := &Bot{
		API:            failing,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            silentLogger(),
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, 2*time.Second, "the bot to retry a refused connection", func() bool {
		return failing.attempts() >= 3
	})
}

// A bot handed a context that is already done must not open anything at
// all, so a shutdown that races the start does not leave a connection
// behind.
func TestRunOpensNothingWhenTheContextIsAlreadyCancelled(t *testing.T) {
	c := &countingAPI{}
	b := &Bot{API: c, Player: engine.Strong(1), Username: "tidymazebot", Log: silentLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Run(ctx); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if c.count() != 0 {
		t.Errorf("opened %d connections with a cancelled context, want 0", c.count())
	}
}

// The backoff schedule, checked without waiting on it.
func TestReconnectBackoffDoublesResetsAndIsCapped(t *testing.T) {
	b := &Bot{
		ReconnectDelay:    10 * time.Millisecond,
		MaxReconnectDelay: 40 * time.Millisecond,
		HealthyConnection: 100 * time.Millisecond,
	}
	cases := []struct {
		name    string
		current time.Duration
		lasted  time.Duration
		want    time.Duration
	}{
		{"doubles after a short connection", 10 * time.Millisecond, time.Millisecond, 20 * time.Millisecond},
		{"stops doubling at the cap", 30 * time.Millisecond, time.Millisecond, 40 * time.Millisecond},
		{"stays at the cap", 40 * time.Millisecond, time.Millisecond, 40 * time.Millisecond},
		{"starts over after a healthy connection", 40 * time.Millisecond, time.Second, 10 * time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := b.nextReconnectDelay(c.current, c.lasted); got != c.want {
				t.Errorf("nextReconnectDelay(%v, %v) = %v, want %v", c.current, c.lasted, got, c.want)
			}
		})
	}
}

// Unset knobs must fall back to values that work against the real lichess,
// not to zero, which would mean a busy reconnect loop and a backoff that
// never resets.
func TestUnsetTimingKnobsFallBackToTheDefaults(t *testing.T) {
	b := &Bot{}
	if got := b.reconnectDelay(); got != defaultReconnectDelay {
		t.Errorf("reconnectDelay() = %v, want %v", got, defaultReconnectDelay)
	}
	if got := b.maxReconnectDelay(); got != maxReconnectDelay {
		t.Errorf("maxReconnectDelay() = %v, want %v", got, maxReconnectDelay)
	}
	if got := b.healthyConnection(); got != defaultHealthyConnection {
		t.Errorf("healthyConnection() = %v, want %v", got, defaultHealthyConnection)
	}
}

// The whole reason silence is safe to ignore is that the transport asks the
// connection whether it is alive. Without this the bot is back to blocking
// forever on a socket that died, which is the bug that started all of this.
func TestTheRealClientPingsAnIdleConnection(t *testing.T) {
	a := newHTTPAPI("tok")
	tr, ok := a.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", a.http.Transport)
	}
	if tr.HTTP2 == nil || tr.HTTP2.SendPingTimeout <= 0 {
		t.Fatal("no HTTP/2 ping health check: a dead connection would never be noticed")
	}
	if a.http.Timeout != 0 {
		t.Errorf("Client.Timeout is %v, want 0: it caps the body read and would cut every stream off", a.http.Timeout)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Error("no ResponseHeaderTimeout: a request that is never answered would hang the bot")
	}
}

type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }

type failOpenAPI struct {
	mu    sync.Mutex
	tries int
}

func (f *failOpenAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	f.tries++
	f.mu.Unlock()
	return nil, io.ErrUnexpectedEOF
}

func (f *failOpenAPI) postForm(path string, form string) error { return nil }

func (f *failOpenAPI) attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tries
}

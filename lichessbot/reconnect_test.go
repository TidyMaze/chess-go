package lichessbot

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"chess/engine"
)

// blockingStream never returns data and never ends, which is how a socket
// that died somewhere in the network behaves: the Read blocks forever and
// nothing on the connection ever says so. Observed on the live bot on
// 2026-09-13, where the event stream's socket sat in CLOSED while the
// process stayed alive at 0% CPU for twelve minutes, accepting nothing and
// logging nothing.
type blockingStream struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingStream() *blockingStream {
	return &blockingStream{closed: make(chan struct{})}
}

func (s *blockingStream) Read(p []byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *blockingStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *blockingStream) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// stubbornStream is a stream whose Close does NOT unblock a Read already in
// flight. That is how lichess's streams really behave: they are served over
// HTTP/2, where the read parks on the http2 pipe's condition variable and a
// Close from another goroutine leaves it parked there. Only cancelling the
// request's context ends it.
//
// Taken from the live bot's own goroutine dump on 2026-09-13, which showed
// the read sitting in http2.(*pipe).Read -> sync.(*Cond).Wait long after
// the idle watchdog had fired and closed the body.
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

func (s *stubbornStream) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// countingAPI hands out a fresh stream on every connection and counts them,
// so a test can prove the bot reconnects instead of giving up.
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

// The bug: a connection that goes silent must be abandoned. Without an idle
// timeout the bot blocks in Read forever on a dead socket and never opens a
// second connection, which is exactly what stopped it playing.
func TestASilentConnectionIsDroppedAndRetried(t *testing.T) {
	c := &countingAPI{}
	b := &Bot{
		API:            c,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            silentLogger(),
		IdleTimeout:    50 * time.Millisecond,
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, 2*time.Second, "the bot to reconnect past a silent stream", func() bool {
		return c.count() >= 3
	})

	c.mu.Lock()
	first := c.streams[0]
	c.mu.Unlock()
	if !first.isClosed() {
		t.Error("the silent stream was left open; a dropped connection must be closed, not leaked")
	}
}

// Silence is what hid this bug for twelve minutes, so a dropped connection
// has to leave a trace even when nothing errored.
func TestADroppedConnectionIsLogged(t *testing.T) {
	c := &countingAPI{}
	var buf syncBuf
	b := &Bot{
		API:            c,
		Player:         engine.Strong(1),
		Username:       "tidymazebot",
		Log:            newBufLogger(&buf),
		IdleTimeout:    50 * time.Millisecond,
		ReconnectDelay: time.Millisecond,
	}
	stop := runInBackground(t, b)
	defer stop()

	waitFor(t, 2*time.Second, "the drop to reach the log", func() bool {
		return strings.Contains(buf.String(), "reconnecting")
	})
}

// A stream that ends cleanly, which lichess does on its own schedule, must
// also bring the bot back rather than end its run.
func TestRunReconnectsWhenTheStreamEndsCleanly(t *testing.T) {
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

	waitFor(t, 2*time.Second, "the bot to reconnect after a clean end", func() bool {
		return c.count() >= 3
	})
}

// A connection that fails to open must not end the run either: lichess
// returns 429 and 5xx often enough that giving up on one is giving up for
// the day.
func TestRunKeepsTryingWhenAConnectionCannotBeOpened(t *testing.T) {
	c := &countingAPI{newStream: func(int) io.ReadCloser { return nil }}
	failing := &failOpenAPI{inner: c}
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
		IdleTimeout:       100 * time.Millisecond,
		ReconnectDelay:    10 * time.Millisecond,
		MaxReconnectDelay: 40 * time.Millisecond,
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
// not to zero, which would mean no timeout and a busy reconnect loop.
func TestUnsetTimingKnobsFallBackToTheDefaults(t *testing.T) {
	b := &Bot{}
	if got := b.idleTimeout(); got != defaultIdleTimeout {
		t.Errorf("idleTimeout() = %v, want %v", got, defaultIdleTimeout)
	}
	if got := b.reconnectDelay(); got != defaultReconnectDelay {
		t.Errorf("reconnectDelay() = %v, want %v", got, defaultReconnectDelay)
	}
	if got := b.maxReconnectDelay(); got != maxReconnectDelay {
		t.Errorf("maxReconnectDelay() = %v, want %v", got, maxReconnectDelay)
	}
}

type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }

type failOpenAPI struct {
	mu    sync.Mutex
	tries int
	inner *countingAPI
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

// The idle watchdog must not fire on a stream that is talking, including
// one that only sends lichess's blank keepalive lines.
func TestAStreamThatKeepsTalkingIsNotDropped(t *testing.T) {
	pr, pw := io.Pipe()
	r := newIdleReader(pr, 200*time.Millisecond, func() { pr.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			time.Sleep(40 * time.Millisecond)
			if _, err := pw.Write([]byte("\n")); err != nil {
				return
			}
		}
		pw.Close()
	}()

	n := 0
	err := eachLine(r, func(line []byte) error { n++; return nil })
	<-done
	if err != nil {
		t.Fatalf("a stream sending keepalives every 40ms under a 200ms idle timeout ended with %v", err)
	}
}

// And it must fire on one that stops talking mid-stream, not only on one
// that never spoke at all.
func TestAStreamThatGoesQuietMidwayIsDropped(t *testing.T) {
	s := newBlockingStream()
	r := newIdleReader(s, 50*time.Millisecond, func() { s.Close() })
	start := time.Now()
	_, err := io.ReadAll(r)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the idle timeout took %v to fire, want about 50ms", took)
	}
	if err != nil && err != io.EOF {
		t.Fatalf("reading a dropped stream gave %v", err)
	}
	if !s.isClosed() {
		t.Error("the idle timeout fired without closing the underlying stream")
	}
}

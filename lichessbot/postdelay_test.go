package lichessbot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"chess/engine"
	"chess/game"
)

// Sending a move must not be delayed by the search that chose it.
//
// In play, posting a move took 530 ms to 680 ms whenever a real search had
// just run, and 16 ms to 26 ms when the move came from the opening book and
// no search happened at all. 16 ms is also what a full cold request to
// lichess costs, DNS and TLS included, so the network was never the problem
// and neither was the move endpoint: the delay is ours, and it only appears
// behind a multi threaded search.
//
// This runs against a local server answering instantly, so anything it
// measures is this process, not lichess.
func TestPostingIsNotDelayedByTheSearchBeforeIt(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real search")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	a := &httpAPI{token: "tok", http: srv.Client()}

	post := func() time.Duration {
		start := time.Now()
		if err := a.postFormAt(srv.URL, ""); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}

	// Warm the connection so the first handshake is not counted.
	post()
	var idle time.Duration
	for i := 0; i < 5; i++ {
		if d := post(); d > idle {
			idle = d
		}
	}

	// The champion plays with several search threads, which is the case
	// that matters: one thread cannot starve anything.
	// Iterative matters: without it TimeBudget is ignored and Depth is a
	// fixed target, so a depth 64 search never returns.
	p := engine.Player{Name: "t", Depth: 6, Threads: 4, Quiescence: true, TTBits: 20,
		NullMove: true, Iterative: true, TimeBudget: 400 * time.Millisecond}
	g := game.New()
	if _, ok := engine.PlayerPick(p, g); !ok {
		t.Fatal("the search found no move from the start position")
	}
	afterSearch := post()

	// Generous: the search may leave the machine briefly busy. What this
	// rejects is the half second seen in play, which is most of a bullet
	// move and larger than the whole budget the rule allows one.
	t.Logf("idle post %v, post right after a search %v, difference %v",
		idle.Round(time.Millisecond), afterSearch.Round(time.Millisecond),
		(afterSearch - idle).Round(time.Millisecond))
	limit := idle + 150*time.Millisecond
	if afterSearch > limit {
		t.Errorf("posting took %v right after a search but %v when idle; the search is delaying the move by %v, which the clock pays for on every move",
			afterSearch.Round(time.Millisecond), idle.Round(time.Millisecond),
			(afterSearch - idle).Round(time.Millisecond))
	}
}

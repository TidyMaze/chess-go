package engine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chess/game"
)

// One UCIEngine is handed to every match worker, so with a single process
// behind it and a mutex in front, ten workers played one move at a time:
// a 200-game race at 1 s per move that should take half an hour was on
// course for five. A shared engine has to run callers in parallel, one
// process per concurrent caller, spawned on demand.
func TestSharedUCIEngineServesCallersInParallel(t *testing.T) {
	script := filepath.Join(t.TempDir(), "slow-uci.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
while read -r line; do
  case "$line" in
    uci*) echo uciok;;
    isready) echo readyok;;
    go*) sleep 1; echo "bestmove e2e4";;
    quit) exit 0;;
  esac
done
`), 0o700); err != nil {
		t.Fatal(err)
	}
	e, err := NewStockfish(script, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 4
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := e.BestMove(game.New(), 1, 0); !ok {
				t.Error("bestmove e2e4 was refused")
			}
		}()
	}
	wg.Wait()
	if el := time.Since(start); el > 2500*time.Millisecond {
		t.Errorf("%d callers took %v: they were served one at a time", callers, el.Round(time.Millisecond))
	}
	// A process that cannot be spawned any more fails the call, not the
	// program.
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	var wg2 sync.WaitGroup
	refused := 0
	var mu sync.Mutex
	for i := 0; i < callers+2; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			if _, ok := e.BestMove(game.New(), 1, 0); !ok {
				mu.Lock()
				refused++
				mu.Unlock()
			}
		}()
	}
	wg2.Wait()
	if refused == 0 {
		t.Error("with the binary gone, every caller beyond the pooled processes should have been refused")
	}
}

// Every command has to fail cleanly when the process behind it dies
// mid-answer or when the engine has been closed, and a bestmove with no
// score line before it is "no evaluation", not a zero.
func TestPooledClientFailsCleanlyWhenAProcessDiesOrIsClosed(t *testing.T) {
	writeFake := func(name, onGo string) string {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile read -r line; do\n  case \"$line\" in\n    uci*) echo uciok;;\n    isready) echo readyok;;\n    go*|eval) "+onGo+";;\n    quit) exit 0;;\n  esac\ndone\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	g := game.New()

	dies, err := NewStockfish(writeFake("dies.sh", "exit 0"), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dies.LegalMoves(g); err == nil {
		t.Error("LegalMoves survived a process that died mid-answer")
	}
	if _, _, ok := dies.Evaluate(g, 1); ok {
		t.Error("Evaluate survived a process that died mid-answer")
	}
	if _, ok := dies.StaticEval(g); ok {
		t.Error("StaticEval survived a process that died mid-answer")
	}

	mute, err := NewStockfish(writeFake("mute.sh", `echo "bestmove e2e4"`), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := mute.Evaluate(g, 1); ok {
		t.Error("a bestmove with no score line was taken as an evaluation")
	}
	mute.Close()
	if _, ok := mute.BestMove(g, 1, 0); ok {
		t.Error("BestMove on a closed engine returned a move")
	}
	if looksLikeUCIMove("e7e8x") {
		t.Error("x is not a promotion piece")
	}
	if looksLikeUCIMove("e9e8") {
		t.Error("rank 9 is off the board")
	}

	garbled, err := NewStockfish(writeFake("garbled.sh", `echo "bestmove zz99"`), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := garbled.BestMove(g, 1, 0); ok {
		t.Error("an unparsable bestmove was accepted")
	}
}

package main

import (
	"bufio"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"

	"chess/board"
	"chess/engine"
)

// Many workers finish games at once; every record must land as one whole
// line, and NaN scores (book moves, the external engine) must survive the
// round trip since the audit relies on them to tell "no opinion" apart
// from "level".
func TestJSONLSinkWritesWholeLinesUnderConcurrency(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex
	sink := jsonlSink(lockedWriter{&buf, &mu})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sink(engine.GameRecord{StartFEN: "fen", Moves: []string{"e2e4", "e7e5"}, Scores: []float64{0.3, math.NaN()},
				White: "challenger", Black: "reference", Winner: board.Color(i % 2), Decisive: true})
		}(i)
	}
	wg.Wait()
	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	n := 0
	for sc.Scan() {
		var r engine.GameRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\n%s", n, err, sc.Text())
		}
		if len(r.Moves) != 2 || !math.IsNaN(r.Scores[1]) {
			t.Fatalf("line %d lost data: %+v", n, r)
		}
		n++
	}
	if n != 50 {
		t.Errorf("%d lines, want 50", n)
	}
}

type lockedWriter struct {
	b  *strings.Builder
	mu *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

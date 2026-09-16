package main

import (
	"encoding/json"
	"io"
	"sync"

	"chess/engine"
)

// jsonlSink writes one JSON line per finished game. Match workers call it
// concurrently, so it holds a lock around each write; a torn line would
// make the whole file unreadable to the audit that consumes it.
func jsonlSink(w io.Writer) func(engine.GameRecord) {
	var mu sync.Mutex
	enc := json.NewEncoder(w)
	return func(r engine.GameRecord) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(r)
	}
}

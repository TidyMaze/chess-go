package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// A book move is played without a search, so the engine reports its score
// as NaN. encoding/json refuses NaN and fails the whole document, not the
// one field, so the reply went out as 200 with an empty body and the board
// froze on the first move the book knew. It must serialise as null, which
// the browser already reads as "no score".
func TestAMoveWithNoScoreSerialises(t *testing.T) {
	body, err := json.Marshal(moveResponse{
		OK: true, FEN: "startpos", Score: jsonFloat(math.NaN()),
	})
	if err != nil {
		t.Fatalf("a response carrying no score must still encode, got %v", err)
	}
	if !strings.Contains(string(body), `"score":null`) {
		t.Errorf("score should be null, got %s", body)
	}
}

// The same applies to knps, which divides by an elapsed time that is zero
// whenever the move came from the book.
func TestAnInfiniteRateSerialises(t *testing.T) {
	body, err := json.Marshal(moveResponse{KNPS: jsonFloat(math.Inf(1))})
	if err != nil {
		t.Fatalf("an infinite rate must not fail the document, got %v", err)
	}
	if !strings.Contains(string(body), `"knps":null`) {
		t.Errorf("knps should be null, got %s", body)
	}
}

// An ordinary score must still come out as a number.
func TestARealScoreIsStillANumber(t *testing.T) {
	body, err := json.Marshal(moveResponse{Score: jsonFloat(-1.25)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"score":-1.25`) {
		t.Errorf("score should be -1.25, got %s", body)
	}
}

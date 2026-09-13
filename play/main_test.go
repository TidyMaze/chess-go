package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"chess/engine"
)

func initTestChampion() {
	if champions == nil {
		champions = engine.NewChampionWatcher("../champion.json")
	}
}

func TestHandleNewIncludesEval(t *testing.T) {
	initTestChampion()
	req := httptest.NewRequest("POST", "/api/new", nil)
	w := httptest.NewRecorder()
	handleNew(w, req)

	var res moveResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.FEN == "" {
		t.Fatalf("expected valid new game response, got %+v", res)
	}
}

func TestHandleMoveReturnsSearchStats(t *testing.T) {
	initTestChampion()
	body, _ := json.Marshal(moveRequest{
		FEN:  "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		From: "e2",
		To:   "e4",
	})
	req := httptest.NewRequest("POST", "/api/move", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleMove(w, req)

	var res moveResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.EngineMove == "" {
		t.Fatalf("expected engine reply move, got %+v", res)
	}
	if res.Depth <= 0 {
		t.Errorf("expected positive depth, got %d", res.Depth)
	}
}

func TestHandleEvalReturnsMetrics(t *testing.T) {
	initTestChampion()
	body, _ := json.Marshal(moveRequest{
		FEN: "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
	})
	req := httptest.NewRequest("POST", "/api/eval", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleEval(w, req)

	var res moveResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("expected ok eval, got %+v", res)
	}
}

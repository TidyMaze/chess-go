package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"chess/board"
	"chess/engine"
	"chess/game"
)

func TestWriteAndReadSample(t *testing.T) {
	fen := "r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	own := engine.AppendHalfKPFeatures(nil, &g.Board, board.White)
	opp := engine.AppendHalfKPFeatures(nil, &g.Board, board.Black)
	score := 0.35 // pawns from White perspective

	var buf bytes.Buffer
	rec := sampleRecord{
		game:   1,
		target: float32(score),
		static: float32(score),
		own:    own,
		opp:    opp,
	}

	if err := writeBinaryRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if len(data) != 14+2*(len(own)+len(opp)) {
		t.Fatalf("unexpected record size: got %d, want %d", len(data), 14+2*(len(own)+len(opp)))
	}

	// Verify header decoding matching pytorch/train.py: head = struct.Struct("<iffBB")
	gameID := int32(binary.LittleEndian.Uint32(data[0:4]))
	target := math.Float32frombits(binary.LittleEndian.Uint32(data[4:8]))
	static := math.Float32frombits(binary.LittleEndian.Uint32(data[8:12]))
	nOwn := int(data[12])
	nOpp := int(data[13])

	if gameID != 1 {
		t.Errorf("gameID = %d, want 1", gameID)
	}
	if math.Abs(float64(target)-score) > 1e-5 {
		t.Errorf("target = %f, want %f", target, score)
	}
	if math.Abs(float64(static)-score) > 1e-5 {
		t.Errorf("static = %f, want %f", static, score)
	}
	if nOwn != len(own) {
		t.Errorf("nOwn = %d, want %d", nOwn, len(own))
	}
	if nOpp != len(opp) {
		t.Errorf("nOpp = %d, want %d", nOpp, len(opp))
	}

	// Verify feature values
	pos := 14
	for i, expected := range own {
		v := int32(binary.LittleEndian.Uint16(data[pos+2*i:]))
		if v != expected {
			t.Fatalf("own[%d] = %d, want %d", i, v, expected)
		}
	}
}

func TestStockfishScoring(t *testing.T) {
	if _, err := os.Stat("/opt/homebrew/bin/stockfish"); err != nil {
		t.Skip("stockfish binary not present")
	}

	sf, err := engine.NewStockfish("/opt/homebrew/bin/stockfish", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	g, err := game.ParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	score, mate, ok := sf.Evaluate(g, 6)
	if !ok {
		t.Fatal("evaluate failed")
	}
	if mate {
		t.Error("starting position evaluated as mate")
	}
	t.Logf("starting position SF depth 6 score: %+.2f", score)

	staticScore, staticOk := sf.StaticEval(g)
	if !staticOk {
		t.Fatal("static eval failed")
	}
	t.Logf("starting position SF static score: %+.2f", staticScore)
}

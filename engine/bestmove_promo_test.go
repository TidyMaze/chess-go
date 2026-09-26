package engine

import (
	"strings"
	"testing"
)

// "bestmove e7e8" is not a legal UCI move: python-chess raises "illegal
// uci" and a GUI forfeits the game the first time the engine promotes.
func TestUCIBestmoveNamesThePromotionPiece(t *testing.T) {
	var out strings.Builder
	ServeUCI(strings.NewReader("position fen 8/4P1k1/8/8/8/8/8/K7 w - - 0 1\ngo depth 4\nquit\n"), &out, Strong(2))
	if !strings.Contains(out.String(), "bestmove e7e8q\n") {
		t.Errorf("want bestmove e7e8q, got:\n%s", out.String())
	}
}

// A game record is replayed later (loss audit, python-chess), so its moves
// have to be legal UCI too.
func TestGameRecordNamesThePromotionPiece(t *testing.T) {
	var got []GameRecord
	GameSink = func(r GameRecord) { got = append(got, r) }
	defer func() { GameSink = nil }()
	sf, err := NewStockfish(fakeUCI(t, "bestmove e7e8q"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	playFrom(mustFEN(t, "8/4P1k1/8/8/8/8/6K1/8 w - - 0 1"), Player{Name: "sf", UCI: sf, UCIDepth: 1}, Player{Random: true}, 1, nil)
	if len(got) != 1 || len(got[0].Moves) != 1 || got[0].Moves[0] != "e7e8q" {
		t.Errorf("recorded moves %v, want [e7e8q]", got)
	}
}

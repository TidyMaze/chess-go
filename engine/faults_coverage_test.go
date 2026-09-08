package engine

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"chess/board"
	"chess/game"
)

// A writer that fails after a byte budget, to reach every error return of
// a serializer that a real file only produces on a full disk.
type failAfter struct{ left int }

func (w *failAfter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		n := w.left
		w.left = 0
		return n, errors.New("disk full")
	}
	w.left -= len(p)
	return len(p), nil
}

var krkOnce struct {
	sync.Once
	set *TablebaseSet
}

// krk is the king-and-rook table, generated once per test binary: the
// generation takes twenty-odd seconds.
func krk() *TablebaseSet {
	krkOnce.Do(func() {
		tb := GenerateTablebase([]board.ColoredPiece{
			{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Rook}, {Color: board.Black, Type: board.King}}, nil)
		// Keyed the way Probe looks positions up: by the material signature.
		g, _ := game.ParseFEN("4k3/8/8/8/8/8/8/4K2R w - - 0 1")
		key, _ := materialKey(nil, &g.Board)
		krkOnce.set = &TablebaseSet{byKey: map[string]*Tablebase{string(key): tb}}
	})
	return krkOnce.set
}

func TestTablebaseSerializationFaults(t *testing.T) {
	set := krk()
	if set.Len() == 0 {
		t.Fatal("empty set")
	}
	var none *TablebaseSet
	if none.Len() != 0 {
		t.Error("nil set has entries")
	}
	// Every write in the format fails at some budget.
	for budget := 0; budget < 64; budget++ {
		if err := set.writeSet(&failAfter{left: budget}); err == nil {
			t.Errorf("budget %d bytes: no error", budget)
		}
	}
	// Unwritable path.
	if set.Save(filepath.Join(t.TempDir(), "no", "dir", "tb.bin")) == nil {
		t.Error("saved into a missing directory")
	}
	// Truncated files fail at every stage of the loader.
	path := filepath.Join(t.TempDir(), "tb.bin")
	if err := set.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, cut := range []int{0, 2, 5, 9, 12, 14, 18, 21, len(data) - 3} {
		if cut < 0 || cut >= len(data) {
			continue
		}
		short := filepath.Join(t.TempDir(), "short.bin")
		os.WriteFile(short, data[:cut], 0o600)
		if _, err := LoadTablebases(short); err == nil {
			t.Errorf("a file cut at %d bytes loaded", cut)
		}
	}
	if _, err := LoadTablebases(filepath.Join(t.TempDir(), "absent.bin")); err == nil {
		t.Error("missing file loaded")
	}
}

func TestTablebaseProbeMisses(t *testing.T) {
	set := krk()
	// Material not in the set.
	g := mustFEN(t, "4k3/8/8/8/8/8/8/4KQ2 w - - 0 1")
	if _, ok := set.Probe(&g.Board, board.White); ok {
		t.Error("probed a material the set does not hold")
	}
	if _, ok := set.probeDTM(&g.Board, board.White); ok {
		t.Error("probeDTM on unknown material")
	}
	// Too many pieces of one colour for the key buffer.
	many := mustFEN(t, "4k3/8/8/8/8/8/PPPPPPPP/4K3 w - - 0 1")
	if _, ok := set.Probe(&many.Board, board.White); ok {
		t.Error("probed a nine-piece position")
	}
	var none *TablebaseSet
	if _, ok := none.Probe(&g.Board, board.White); ok {
		t.Error("nil set answered")
	}
	if _, ok := none.probeDTM(&g.Board, board.White); ok {
		t.Error("nil set answered a DTM")
	}
	// encodePosition: order and pieces disagree.
	kr := mustFEN(t, "4k3/8/8/8/8/8/8/4K2R w - - 0 1")
	if _, ok := encodePosition(&kr.Board, board.White, []board.ColoredPiece{{Color: board.White, Type: board.King}}); ok {
		t.Error("encoded against an order of the wrong length")
	}
	if _, ok := encodePosition(&kr.Board, board.White, []board.ColoredPiece{
		{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Queen}, {Color: board.Black, Type: board.King}}); ok {
		t.Error("encoded against an order naming a missing piece")
	}
	if _, ok := encodePosition(&many.Board, board.White, make([]board.ColoredPiece, 10)); ok {
		t.Error("encoded ten pieces")
	}
}

// A fake UCI engine: a shell script that answers the handshake and then
// says whatever the test wants, so the wrapper's parsing branches can be
// reached without depending on Stockfish's output.
func fakeUCI(t *testing.T, replies string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-uci.sh")
	script := "#!/bin/sh\nwhile read -r line; do\n case \"$line\" in\n  uci) echo uciok;;\n  isready) echo readyok;;\n  quit) exit 0;;\n  go*|eval) printf '%s\\n' " + shellQuote(replies) + ";;\n esac\ndone\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += `'\''`
		} else {
			out += string(r)
		}
	}
	return out + "'"
}

func TestUCIWrapperFaultBranches(t *testing.T) {
	if _, err := NewStockfish("/etc/hosts", 0, 0); err == nil {
		t.Error("a non-executable file started")
	}
	g := game.New()
	// An engine that answers a move which is not legal here, and one that
	// has none.
	illegal, _ := NewStockfish(fakeUCI(t, "bestmove a1a8"), 0, 0)
	if _, ok := illegal.BestMove(g, 1, 0); ok {
		t.Error("an illegal engine move was accepted")
	}
	none, _ := NewStockfish(fakeUCI(t, "bestmove (none)"), 0, 0)
	if _, ok := none.BestMove(g, 1, 0); ok {
		t.Error("(none) was accepted as a move")
	}
	if looksLikeUCIMove("e7e8x") {
		t.Error("a five-letter token with no promotion piece is not a move")
	}
	// Evaluate: a score line too short to parse, then a mate.
	odd, _ := NewStockfish(fakeUCI(t, "info depth 1 score\ninfo depth 1 score cp\ninfo depth 1 score cp x\ninfo depth 2 score mate 3\nbestmove e2e4"), 0, 0)
	if _, mate, ok := odd.Evaluate(g, 1); !ok || !mate {
		t.Errorf("mate line not read: mate=%v ok=%v", mate, ok)
	}
	// Static evaluation with a garbled line, then a good one.
	se, _ := NewStockfish(fakeUCI(t, "Final evaluation\nFinal evaluation abc (white side)\nFinal evaluation +0.42 (white side)"), 0, 0)
	if v, ok := se.StaticEval(g); !ok || v != 0.42 {
		t.Errorf("static eval %v %v", v, ok)
	}
	// An engine that says nothing useful: the parse ends on readyok.
	quiet, _ := NewStockfish(fakeUCI(t, "info string nothing"), 0, 0)
	if _, ok := quiet.StaticEval(g); ok {
		t.Error("a silent engine produced a static eval")
	}
	// An engine that dies: reads hit end of file.
	dying, _ := NewStockfish(fakeUCI(t, "bestmove e2e4"), 0, 0)
	dying.Close()
	if _, err := dying.LegalMoves(g); err == nil {
		t.Error("LegalMoves on a dead engine returned no error")
	}
	if _, _, ok := dying.Evaluate(g, 1); ok {
		t.Error("Evaluate on a dead engine succeeded")
	}
	if _, ok := dying.StaticEval(g); ok {
		t.Error("StaticEval on a dead engine succeeded")
	}
	if dying.waitFor("readyok") != "" {
		t.Error("waitFor on a dead engine returned a line")
	}
}

func TestLegacyNetFaults(t *testing.T) {
	bad := &Net{W1: make([]float32, 10), B1: make([]float32, 4)}
	if bad.Evaluate(&game.New().Board) != 0 {
		t.Error("a mis-sized net evaluated")
	}
	if bad.Save(filepath.Join(t.TempDir(), "no", "dir", "n.json")) == nil {
		t.Error("saved into a missing directory")
	}
	p := filepath.Join(t.TempDir(), "g.json")
	os.WriteFile(p, []byte("{"), 0o600)
	if _, err := LoadNet(p); err == nil {
		t.Error("garbage loaded")
	}
}

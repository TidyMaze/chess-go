package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

func TestResultAccountingBranchesAndOpenings(t *testing.T) {
	oldList, oldPlies := MatchOpenings, OpeningPlies
	defer func() { MatchOpenings, OpeningPlies = oldList, oldPlies }()
	// Every game from bare kings is a draw; a random player against a
	// searching one loses.
	MatchOpenings = []string{"8/8/8/8/8/8/8/K6k w - - 0 1"}
	if r := PlayMatch(Player{Random: true}, Strong(1), 2, 40); r.Draws != 2 {
		t.Errorf("bare kings: %+v", r)
	}
	MatchOpenings = nil
	if r := PlayMatch(Player{Random: true}, Strong(2), 4, 120); r.Losses == 0 {
		t.Errorf("random against depth 2 never lost: %+v", r)
	}
	// Random openings that run into the end of a game.
	OpeningPlies = 600
	if g := matchOpening(7); g == nil {
		t.Error("no opening")
	}
	// A player that cannot pick ends the game early.
	none, _ := NewStockfish(fakeUCI(t, "bestmove (none)"), 0, 0)
	defer none.Close()
	winner, decisive := playFrom(game.New(), Player{UCI: none}, Player{Random: true}, 10, nil)
	if decisive {
		t.Errorf("a game nobody could continue was decisive for %v", winner)
	}
	if _, err := os.Stat(testStockfish); err == nil {
		// Against a real engine, so wins, draws and losses are all reachable.
		if _, err := PlayMatchAgainstUCI(Strong(2), func() (Player, func(), error) {
			sf, err := NewStockfish(testStockfish, 0, 1320)
			return Player{UCI: sf, UCIDepth: 4}, func() { sf.Close() }, err
		}, 4, 120, 2); err != nil {
			t.Errorf("match against Stockfish: %v", err)
		}
	}
}

func TestScorePathsAndTunedPlayer(t *testing.T) {
	g := game.New()
	tuned := Strong(2)
	tuned.Tuned = true
	if _, ok := PlayerScoreWith(tuned, g, nil); !ok {
		t.Error("tuned player: no score")
	}
	tuned.Weights = &[6]float64{1, 3, 3, 5, 9, 0}
	if _, ok := PlayerScoreWith(tuned, g, nil); !ok {
		t.Error("tuned player with explicit weights: no score")
	}
	mated := mustFEN(t, "R5k1/5ppp/8/8/8/8/5PPP/6K1 b - - 0 1")
	if _, ok := PlayerScoreWith(Player{Depth: 0}, mated, nil); ok {
		t.Error("scored a position with no moves")
	}
	if _, ok := PlayerPick(Player{Depth: 2, UsePST: true}, mated); ok {
		t.Error("the fixed-depth root found a move with none legal")
	}
	if _, ok := ChooseMoveIterativeTimed(mated, board.Black, 2, &Eval{}, false, 0); ok {
		t.Error("the iterative root found a move with none legal")
	}
	_ = TunedMobility()
}

func TestEvaluationTermEdges(t *testing.T) {
	// A king with more attackers than the danger scale has entries.
	swarm := mustFEN(t, "3qqq2/3qkq2/3qqq2/8/8/8/8/K7 w - - 0 1")
	ev := &Eval{UsePST: true, KingSafety: 0.05}
	_ = PositionScoreEval(&swarm.Board, board.White, ev)
	_ = PositionScoreEval(&swarm.Board, board.Black, ev)
	// Rook on the seventh, doubled rooks, a backward pawn, a knight outpost.
	pos := mustFEN(t, "4k3/1R2pp2/8/3Pn3/2P5/4P3/1R6/4K3 w - - 0 1")
	for _, p := range []Player{{Depth: 1, UsePST: true, Extras: true}, {Depth: 1, UsePST: true, Shape: true}} {
		_ = PlayerStaticEval(p, &pos.Board)
		_ = PositionScoreEval(&pos.Board, board.Black, evalForPlayer(p))
	}
	outpost := mustFEN(t, "4k3/pp3ppp/8/3N4/2P1P3/8/PP3PPP/4K3 w - - 0 1")
	_ = PlayerStaticEval(Player{Depth: 1, UsePST: true, Shape: true}, &outpost.Board)
	_ = PositionScoreEval(&outpost.Board, board.Black, evalForPlayer(Player{UsePST: true, Shape: true}))
}

func TestLegacySearchTableFlagsAndNullMove(t *testing.T) {
	for _, fen := range []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 b - - 0 9",
		"4k3/8/8/8/8/8/8/QQQ1K3 w - - 0 1",
		"4k3/8/8/8/8/8/8/QQQ1K3 b - - 0 1",
	} {
		g := mustFEN(t, fen)
		legacy := Player{Depth: 4, UsePST: true, TTBits: 12, NullMove: true, Quiescence: true}
		if _, ok := PlayerPick(legacy, g); !ok {
			t.Error("no move")
		}
	}
	var none *TranspositionTable
	if _, ok := none.probe(1, 1, board.White, -1, 1); ok {
		t.Error("nil table answered")
	}
	none.store(1, 0, 1, ttExact, board.White) // no-op
}

func TestMarshalFailuresSurface(t *testing.T) {
	dir := t.TempDir()
	nan := math.NaN()
	if err := WriteChampion(filepath.Join(dir, "c.json"), Champion{Elo: nan}); err == nil {
		t.Error("a NaN Elo marshalled")
	}
	if err := SaveChampion(Weights(&[6]float64{nan, 3, 3, 5, 9, 0}), 0, filepath.Join(dir, "w.json")); err == nil {
		t.Error("NaN weights marshalled")
	}
	h := tinyHalfKP(4, 8, false)
	h.B2 = float32(nan)
	if err := h.Save(filepath.Join(dir, "h.json")); err == nil {
		t.Error("a NaN network marshalled")
	}
	n := &Net{B2: float32(nan)}
	if err := n.Save(filepath.Join(dir, "n.json")); err == nil {
		t.Error("a NaN legacy network marshalled")
	}
	// Legacy net: Scale defaults on load, clipping both ways in Evaluate.
	const hidden = 4
	m := &Net{W1: make([]float32, nnueInputs*hidden), B1: []float32{-5, 5, -5, 5}, W2: []float32{1, 1, 1, 1}, Scale: 0}
	p := filepath.Join(dir, "m.json")
	if err := m.Save(p); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadNet(p); err != nil || loaded.Scale != 1 {
		t.Errorf("scale default: %v %v", loaded, err)
	}
	_ = m.Evaluate(&game.New().Board)
}

func TestSPRTEloExtremes(t *testing.T) {
	s := NewSPRT(0, 20)
	if s.Elo() != 0 {
		t.Error("no games is 0")
	}
	s.Add(0, 0, 5)
	if s.Elo() != -800 {
		t.Errorf("all losses %v", s.Elo())
	}
	w := NewSPRT(0, 20)
	w.Add(5, 0, 0)
	if w.Elo() != 800 {
		t.Errorf("all wins %v", w.Elo())
	}
}

func TestWideNetTakesThePooledBuffer(t *testing.T) {
	wide := tinyHalfKP(maxHalfKPHidden+8, 8, false)
	if v := wide.Evaluate(&game.New().Board); math.IsNaN(v) {
		t.Error("NaN")
	}
}

func TestDeepReverseFutilityCutsInsideTheSearch(t *testing.T) {
	up := mustFEN(t, "4k3/pppppppp/8/8/8/8/PPPPPPPP/QQQQK3 w - - 0 1")
	p := Strong(7)
	p.DeepRFP = true
	if _, ok := PlayerScoreWith(p, up, nil); !ok {
		t.Error("no score")
	}
	// An aborted null-move child unwinds without storing.
	g := mustFEN(t, "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	q := Strong(6)
	ev := evalForPlayer(q)
	ev.Table = NewTranspositionTable(12)
	ctx := &searchCtx{}
	ctx.reset()
	ctx.ev, ctx.quiescence, ctx.extensions = ev, true, true
	ctx.ev.acc = &ctx.acc
	ctx.path[0] = zobristBoard(&g.Board, g.Turn)
	ctx.nodes = 1
	ctx.deadline = time.Now().Add(-time.Second)
	ctx.search(g, g.Turn, g.Turn, 6, 0, negInf, posInf)
	if !ctx.aborted {
		t.Error("did not abort")
	}
}

func TestPlayGameMoveLimitAndTrainDefaults(t *testing.T) {
	w := Weights(&[6]float64{1, 3, 3, 5, 9, 0})
	if r := PlayGame(w, w, 1, 2, nil); r.Reason != "move limit" {
		t.Errorf("two plies: %+v", r)
	}
	cfg := Config{Generations: 1, GamesPerGen: 1, PopulationSize: 1, Depth: 1, MaxMoves: 6, BenchmarkGames: 1,
		ChampionPath: filepath.Join(t.TempDir(), "c.json")}
	if _, rec := Train(cfg); len(rec) != 1 {
		t.Errorf("records %d", len(rec))
	}
	_ = os.Remove(cfg.ChampionPath)
}

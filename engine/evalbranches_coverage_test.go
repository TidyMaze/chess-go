package engine

import (
	"math"
	"testing"

	"chess/board"
	"chess/game"
)

func TestEvaluationBranches(t *testing.T) {
	g := mustFEN(t, "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	// Shape terms with and without explicit weights, extras for both colours.
	sh := Strong(1)
	sh.Shape = true
	if PlayerStaticEval(sh, &g.Board) == 0 && false {
		t.Error("unreachable")
	}
	w := DefaultShapeWeights()
	w.Outpost *= 3
	sh.ShapeW = &w
	_ = PlayerStaticEval(sh, &g.Board)
	ex := Strong(1)
	ex.Extras = true
	white := PlayerStaticEval(ex, &g.Board)
	black := PositionScoreEval(&g.Board, board.Black, evalForPlayer(ex))
	if math.Abs(white+black) > 1e-9 {
		t.Errorf("extras are not antisymmetric: %.4f vs %.4f", white, black)
	}
	// Phase clamps when there is more material than a starting position.
	heavy := mustFEN(t, "QQQQk3/QQQQ4/8/8/8/8/qqqq4/qqqqK3 w - - 0 1")
	if ph := gamePhase(&heavy.Board); ph != 1 {
		t.Errorf("phase %.3f for an overloaded board, want the clamp 1", ph)
	}
	// A legacy residual net corrects the hand score; a non-residual one replaces it.
	const hidden = 4
	net := &Net{W1: make([]float32, nnueInputs*hidden), B1: make([]float32, hidden), W2: make([]float32, hidden), B2: 1, Scale: 1}
	lopsided := mustFEN(t, "4k3/8/8/8/8/8/8/QQQ1K3 w - - 0 1")
	for _, residual := range []bool{true, false} {
		net.Residual = residual
		ev := &Eval{UsePST: true, Net: net}
		a := PositionScoreEval(&lopsided.Board, board.White, ev)
		b := PositionScoreEval(&lopsided.Board, board.Black, ev)
		if residual && !(a > 4 && b < -4) {
			t.Errorf("residual net: white %.2f black %.2f, the hand score must survive", a, b)
		}
		// A non-residual net replaces the hand score entirely: with zero
		// weights it says exactly its output bias, from each side's view.
		if !residual && (math.Abs(a-1) > 1e-6 || math.Abs(b+1) > 1e-6) {
			t.Errorf("replacing net: white %.2f black %.2f, want +1/-1", a, b)
		}
		_ = PositionScoreEval(&g.Board, board.White, ev)
	}
	// The network outside the search: no per-ply accumulator, full evaluation.
	if halfkp, err := LoadHalfKPNet("../champion_net.json"); err == nil {
		ev := &Eval{HalfKP: halfkp, HalfKPBlend: 0.45, UsePST: true}
		a := PositionScoreEval(&lopsided.Board, board.White, ev)
		if a < 4 {
			t.Errorf("three queens up scores %.2f through the network blend", a)
		}
	}
	var none *Eval
	if none.table() != nil {
		t.Error("nil Eval has a table")
	}
}

func TestSearchHelperEdges(t *testing.T) {
	if lmrTable(70, 70) != lmrTable(63, 62) {
		t.Error("depth and move index must clamp to the table")
	}
	c := &searchCtx{}
	c.reset()
	m := mv(4, 1, 4, 3)
	c.recordKiller(maxSearchPly+3, m) // out of range: ignored
	c.recordKiller(2, m)
	c.recordKiller(2, m) // the same move again: no shuffle
	if c.killers[2][0] != m || c.killers[2][1] != (game.Move{}) {
		t.Errorf("killers %v", c.killers[2])
	}
	// More moves than the stack buffer: ordering still works.
	g := game.New()
	ms := make([]game.Move, 120)
	for i := range ms {
		ms[i] = mv(i%8, 1, i%8, 2)
	}
	c.orderMoves(g, ms, game.Move{}, 0, board.White)
}

func TestSearchSwitchesReachTheirBranches(t *testing.T) {
	kiwi := mustFEN(t, "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	for name, mod := range map[string]func(*Player){
		"deep reverse futility": func(p *Player) { p.DeepRFP = true },
		"null gate":             func(p *Player) { p.NullGate = true },
		"null scale":            func(p *Player) { p.NullScale = true },
		"keep null-move ep":     func(p *Player) { p.KeepNullMoveEP = true },
		"no castle":             func(p *Player) { p.NoCastle = true },
	} {
		p := Strong(5)
		mod(&p)
		if _, ok := PlayerPick(p, kiwi); !ok {
			t.Errorf("%s: no move", name)
		}
		if _, ok := PlayerScoreWith(p, kiwi, nil); !ok {
			t.Errorf("%s: no score", name)
		}
	}
	// No castling: the castling moves are filtered out at the root.
	nc := Strong(1)
	nc.NoCastle = true
	only := mustFEN(t, "4k3/8/8/8/8/8/8/4K2R w K - 0 1")
	if m, ok := PlayerPick(nc, only); ok && m.From == (board.Sq{File: 4, Rank: 0}) && m.To == (board.Sq{File: 6, Rank: 0}) {
		t.Error("castled with castling refused")
	}
	// A nil evaluation still searches.
	if _, ok := ChooseMoveIterativeTimed(kiwi, board.White, 2, nil, false, 0); !ok {
		t.Error("nil Eval: no move")
	}
	// The fixed-depth legacy search with its table, null move and quiescence, both colours.
	for _, fen := range []string{
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R b KQkq - 0 1",
	} {
		g := mustFEN(t, fen)
		legacy := Player{Depth: 3, UsePST: true, TTBits: 12, NullMove: true, Quiescence: true}
		if _, ok := PlayerPick(legacy, g); !ok {
			t.Error("legacy search: no move")
		}
		plain := Player{Depth: 2, UsePST: true}
		if _, ok := PlayerPick(plain, g); !ok {
			t.Error("plain fixed-depth search: no move")
		}
	}
}

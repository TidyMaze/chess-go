package engine

import (
	"math/rand"
	"sort"
	"testing"

	"chess/board"
	"chess/game"
)

// Picking the best remaining move one at a time must hand out the moves in
// exactly the order of a stable descending sort on the score, ties kept in
// generation order, or the search tree changes. The search also writes its
// legal moves over the front of the list while it picks, which must not
// disturb the moves still to come.
func TestPickMoveMatchesStableSort(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 2000; trial++ {
		n := rng.Intn(129)
		ms := make([]game.Move, n)
		scores := make([]int, n)
		sees := make([]int16, n)
		keys := make([]int64, n)
		for i := range ms {
			ms[i] = game.Move{From: board.Sq{File: int8(i % 8), Rank: int8(i / 8 % 8)}, To: board.Sq{File: int8(i / 64), Rank: 0}}
			// A narrow range, so ties are common, around the bands the
			// real scores use, including negative ones, and history sums
			// past an int32 either way.
			scores[i] = []int{1 << 30, 1 << 20, 1<<20 - 100, 1 << 19, -1 << 20, 0, -5, 7, 1<<32 - 4, -1 << 32}[rng.Intn(10)] + rng.Intn(3)
			sees[i] = int16(rng.Intn(41) - 20)
			keys[i] = orderKey(scores[i], i, sees[i])
		}
		want := make([]int, n)
		for i := range want {
			want[i] = i
		}
		sort.SliceStable(want, func(a, b int) bool { return scores[want[a]] > scores[want[b]] })

		legal := ms[:0]
		for j := 0; j < n; j++ {
			pickMove(nil, ms, keys, j)
			w := want[j]
			if ms[j] != (game.Move{From: board.Sq{File: int8(w % 8), Rank: int8(w / 8 % 8)}, To: board.Sq{File: int8(w / 64), Rank: 0}}) {
				t.Fatalf("trial %d, n=%d: pick %d gave %v, stable sort gives move %d", trial, n, j, ms[j], w)
			}
			if got := keySEE(keys[j]); got != sees[w] {
				t.Fatalf("trial %d: pick %d carries SEE %d, want %d", trial, j, got, sees[w])
			}
			if rng.Intn(4) != 0 {
				legal = append(legal, ms[j])
			}
		}
	}
}

// A capture of a cheaper piece needs an exchange evaluation to be placed,
// and most nodes cut off before it would come up. The evaluation waits
// until the capture could be the best move left, and the order that comes
// out is still the one the evaluated scores give: winning captures by what
// they win, losing ones after every quiet move.
func TestPickMoveEvaluatesExchangesOnDemand(t *testing.T) {
	for _, fen := range []string{
		// exf5 wins a knight, exd5 trades pawns, Qxd5 loses the queen to Rd7.
		"4k3/3r4/8/3p1n2/4P3/8/8/3QK3 w - - 0 1",
		// Rooks and queen hitting defended pawns, a free bishop.
		"r3k2r/1pp2ppp/p1n1b3/3pp3/1b1PP1q1/2N1BN2/PPPQ1PPP/R3KB1R w KQkq - 0 1",
		"2r3k1/5ppp/p2q4/1p1p4/3Pn3/1P2Q1P1/P3NP1P/2R3K1 b - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		color := g.Turn
		c := &searchCtx{ev: &Eval{MainSEE: true}}
		ms := g.AllLegalMoves(color)

		type ranked struct {
			m     game.Move
			score int
			sv    int16
		}
		want := make([]ranked, len(ms))
		for i, m := range ms {
			sc, sv := c.scoreMove(g, m, game.Move{}, 1, color)
			attacker, _ := g.Board.PieceAt(m.From)
			if victim, ok := g.Board.PieceAt(m.To); ok && mvvLvaPiece[victim.Type] < mvvLvaPiece[attacker.Type] {
				x := see(&g.Board, m)
				sc, sv = 1<<20+x*100+mvvLvaPiece[victim.Type], int16(x)
				if x < 0 {
					sc = -1<<20 + x*100
				}
			}
			want[i] = ranked{m, sc, sv}
		}
		sort.SliceStable(want, func(a, b int) bool { return want[a].score > want[b].score })

		keys := make([]int64, len(ms))
		c.scoreMoves(g, ms, keys, game.Move{}, 1, color)
		pending := 0
		for _, k := range keys {
			if keySEE(k) == seeUnknown {
				pending++
			}
		}
		if pending == 0 {
			t.Fatalf("%s: every exchange was evaluated before the first pick", fen)
		}
		for j := range ms {
			pickMove(&g.Board, ms, keys, j)
			if ms[j] != want[j].m || keySEE(keys[j]) != want[j].sv {
				t.Fatalf("%s: pick %d gave %v (SEE %d), want %v (SEE %d)", fen, j, ms[j], keySEE(keys[j]), want[j].m, want[j].sv)
			}
		}
	}

	// The first pick of the first position is the knight, and the losing
	// queen capture is still unevaluated behind it.
	g, _ := game.ParseFEN("4k3/3r4/8/3p1n2/4P3/8/8/3QK3 w - - 0 1")
	c := &searchCtx{ev: &Eval{MainSEE: true}}
	ms := g.AllLegalMoves(board.White)
	keys := make([]int64, len(ms))
	c.scoreMoves(g, ms, keys, game.Move{}, 1, board.White)
	pickMove(&g.Board, ms, keys, 0)
	exf5 := game.Move{From: board.Sq{File: 4, Rank: 3}, To: board.Sq{File: 5, Rank: 4}}
	qxd5 := game.Move{From: board.Sq{File: 3, Rank: 0}, To: board.Sq{File: 3, Rank: 4}}
	if ms[0] != exf5 {
		t.Fatalf("first pick %v, want exf5 %v", ms[0], exf5)
	}
	for k := 1; k < len(ms); k++ {
		if ms[k] == qxd5 && keySEE(keys[k]) != seeUnknown {
			t.Fatalf("Qxd5 was evaluated (SEE %d) before it could be picked", keySEE(keys[k]))
		}
	}
}

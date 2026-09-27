package engine

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"chess/board"
	"chess/game"
)

// bestKeyScan is the selection scan bestKey replaced, kept as its
// reference: the first index of the largest of keys[j:].
func bestKeyScan(keys []int64, j int) int {
	best := j
	top := keys[j]
	for i := j + 1; i < len(keys); i++ {
		if v := keys[i]; v > top {
			best, top = i, v
		}
	}
	return best
}

// bestKey must pick exactly what the scan picked, on the keys the search
// builds (unique) and on any others: duplicates, where the first of the
// equal largest wins, and the extremes of int64.
func TestBestKeyMatchesScan(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for trial := 0; trial < 50000; trial++ {
		n := 1 + rng.Intn(140)
		keys := make([]int64, n)
		for i := range keys {
			switch trial % 4 {
			case 0:
				keys[i] = orderKey([]int{1 << 30, 1 << 20, 1<<20 - 100, 1 << 19, -1 << 20, 0, -5, 7}[rng.Intn(8)]+rng.Intn(3), i, int16(rng.Intn(256)-128))
			case 1:
				keys[i] = int64(rng.Intn(4))
			case 2:
				keys[i] = rng.Int63() - rng.Int63()
			default:
				keys[i] = []int64{math.MinInt64, math.MaxInt64, 0, -1}[rng.Intn(4)]
			}
		}
		j := rng.Intn(n)
		if got, want := bestKey(keys, j), bestKeyScan(keys, j); got != want {
			t.Fatalf("trial %d: bestKey(%v, %d) = %d, scan gives %d", trial, keys, j, got, want)
		}
	}
}

// Two moves have the same moveWord exactly when they are equal, for every
// square on or next to the board and every piece type as promotion.
func TestMoveWordEqualIffMovesEqual(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	sq := func() board.Sq { return board.Sq{File: int8(rng.Intn(12) - 2), Rank: int8(rng.Intn(12) - 2)} }
	move := func() game.Move { return game.Move{From: sq(), To: sq(), Promo: board.PieceType(rng.Intn(6))} }
	for trial := 0; trial < 200000; trial++ {
		a := move()
		b := a
		switch rng.Intn(6) {
		case 0:
			b = move()
		case 1:
			b.From.File = int8(rng.Intn(12) - 2)
		case 2:
			b.From.Rank = int8(rng.Intn(12) - 2)
		case 3:
			b.To.File = int8(rng.Intn(12) - 2)
		case 4:
			b.To.Rank = int8(rng.Intn(12) - 2)
		default:
			b.Promo = board.PieceType(rng.Intn(6))
		}
		if (moveWord(a) == moveWord(b)) != (a == b) {
			t.Fatalf("%v and %v: moveWord %#x and %#x", a, b, moveWord(a), moveWord(b))
		}
	}
	if moveWord(game.Move{}) != 0 {
		t.Fatalf("the zero move has word %#x, want 0", moveWord(game.Move{}))
	}
}

// sortByKey must put ms and keys in the order repeated selection of the
// largest key gives, each move still beside its own key.
func TestSortByKeyMatchesSelection(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for trial := 0; trial < 20000; trial++ {
		n := rng.Intn(129)
		ms := make([]game.Move, n)
		keys := make([]int64, n)
		for i := range ms {
			ms[i] = game.Move{From: board.Sq{File: int8(i % 8), Rank: int8(i / 8 % 8)}, To: board.Sq{File: int8(i / 64), Rank: 0}}
			keys[i] = orderKey([]int{1<<20 - 100, 1 << 19, 1<<19 - 1, 1 << 18, -1 << 20, 0, -5, 7, 1<<32 - 4, -1 << 32}[rng.Intn(10)]+rng.Intn(3), i, int16(rng.Intn(41)-20))
		}
		wantMs := append([]game.Move(nil), ms...)
		wantKeys := append([]int64(nil), keys...)
		for j := range wantKeys {
			b := bestKeyScan(wantKeys, j)
			wantMs[j], wantMs[b] = wantMs[b], wantMs[j]
			wantKeys[j], wantKeys[b] = wantKeys[b], wantKeys[j]
		}
		sortByKey(ms, keys)
		for j := range ms {
			if ms[j] != wantMs[j] || keys[j] != wantKeys[j] {
				t.Fatalf("trial %d, n=%d: position %d holds %v key %d, selection gives %v key %d", trial, n, j, ms[j], keys[j], wantMs[j], wantKeys[j])
			}
		}
	}
}

// pickAll hands out the moves the way the search's move loop does: one
// pick per move until pickMove reports the rest sorted, then none. It also
// writes over the front of ms as the loop compacts its legal moves there.
func pickAll(b *board.Board, ms []game.Move, keys []int64, keep func() bool) (order []game.Move, sees []int16, sortedAt int) {
	sortedAt = -1
	legal := ms[:0]
	for j := range ms {
		if sortedAt < 0 && pickMove(b, ms, keys, j) {
			sortedAt = j
		}
		order = append(order, ms[j])
		sees = append(sees, keySEE(keys[j]))
		if keep() {
			legal = append(legal, ms[j])
		}
	}
	return order, sees, sortedAt
}

// Once the best move left is below the killers and counter move, pickMove
// sorts the rest in one go, and a loop that stops picking there must see
// the moves, and their exchange values, in the order of a stable
// descending sort on the score, as with a pick per move.
func TestPickMoveSortsTheRestOnce(t *testing.T) {
	// The smallest key a pending exchange can have: a pawn victim, the
	// last index of a list. Nothing pending may be left when sorting.
	if lowest := orderKey(1<<20+101, 0xFFF, seeUnknown); sortBelow >= lowest {
		t.Fatalf("sortBelow %d is not below the lowest pending key %d", sortBelow, lowest)
	}
	rng := rand.New(rand.NewSource(17))
	sorted := 0
	for trial := 0; trial < 5000; trial++ {
		n := rng.Intn(129)
		ms := make([]game.Move, n)
		scores := make([]int, n)
		sees := make([]int16, n)
		keys := make([]int64, n)
		for i := range ms {
			ms[i] = game.Move{From: board.Sq{File: int8(i % 8), Rank: int8(i / 8 % 8)}, To: board.Sq{File: int8(i / 64), Rank: 0}}
			scores[i] = []int{1 << 30, 1<<20 + 300, 1 << 20, 1<<20 - 1, 1<<20 - 100, 1 << 19, 1 << 18, 1<<18 - 50, 1<<18 - 52, -1 << 20, 0, -5, 7, 1<<32 - 4, -1 << 32}[rng.Intn(15)] + rng.Intn(3)
			sees[i] = int16(rng.Intn(41) - 20)
			keys[i] = orderKey(scores[i], i, sees[i])
		}
		want := make([]int, n)
		for i := range want {
			want[i] = i
		}
		sort.SliceStable(want, func(a, b int) bool { return scores[want[a]] > scores[want[b]] })
		byIndex := append([]game.Move(nil), ms...)

		order, gotSees, at := pickAll(nil, ms, keys, func() bool { return rng.Intn(4) != 0 })
		if at >= 0 {
			sorted++
			if keys[at] >= sortBelow {
				t.Fatalf("trial %d: sorted the rest at pick %d, whose key %d is not below sortBelow", trial, at, keys[at])
			}
		}
		for j, w := range want {
			if order[j] != byIndex[w] || gotSees[j] != sees[w] {
				t.Fatalf("trial %d, n=%d, sorted at %d: pick %d gave %v (SEE %d), stable sort gives %v (SEE %d)", trial, n, at, j, order[j], gotSees[j], byIndex[w], sees[w])
			}
		}
	}
	if sorted < 1000 {
		t.Fatalf("only %d of 5000 lists were sorted in one go", sorted)
	}

	// On real positions, pending exchanges included: the same order as a
	// pick per move, and nothing still pending once the rest is sorted.
	for _, fen := range []string{
		"4k3/3r4/8/3p1n2/4P3/8/8/3QK3 w - - 0 1",
		"r3k2r/1pp2ppp/p1n1b3/3pp3/1b1PP1q1/2N1BN2/PPPQ1PPP/R3KB1R w KQkq - 0 1",
		"2r3k1/5ppp/p2q4/1p1p4/3Pn3/1P2Q1P1/P3NP1P/2R3K1 b - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		c := &searchCtx{ev: &Eval{MainSEE: true}}
		ms := g.AllLegalMoves(g.Turn)
		ref := append([]game.Move(nil), ms...)
		refKeys := make([]int64, len(ms))
		c.scoreMoves(g, ref, refKeys, game.Move{}, 1, g.Turn)
		for j := range ref {
			for keySEE(refKeys[bestKeyScan(refKeys, j)]) == seeUnknown {
				k := bestKeyScan(refKeys, j)
				sc, sv := exchangeScore(&g.Board, ref[k])
				refKeys[k] = orderKey(sc, keyIndex(refKeys[k]), sv)
			}
			k := bestKeyScan(refKeys, j)
			ref[j], ref[k] = ref[k], ref[j]
			refKeys[j], refKeys[k] = refKeys[k], refKeys[j]
		}

		keys := make([]int64, len(ms))
		c.scoreMoves(g, ms, keys, game.Move{}, 1, g.Turn)
		order, gotSees, at := pickAll(&g.Board, ms, keys, func() bool { return true })
		if at < 0 {
			t.Fatalf("%s: the rest was never sorted", fen)
		}
		for j := range ref {
			if order[j] != ref[j] || gotSees[j] != keySEE(refKeys[j]) {
				t.Fatalf("%s: pick %d gave %v (SEE %d), picking one by one gives %v (SEE %d)", fen, j, order[j], gotSees[j], ref[j], keySEE(refKeys[j]))
			}
			if gotSees[j] == seeUnknown {
				t.Fatalf("%s: pick %d, %v, still has its exchange pending", fen, j, order[j])
			}
		}
	}
}

// scoreMove is the per-move ranking scoreMoves used to call, kept as its
// reference: transposition-table move first, then captures by MVV-LVA,
// then killers, then history.
func (c *searchCtx) scoreMove(g *game.Game, m game.Move, ttMove game.Move, ply int, color board.Color) (int, int16) {
	if m == ttMove {
		return 1 << 30, 0
	}
	victim, isDirectCapture := g.Board.CellPiece(m.To)
	attacker, hasAttacker := g.Board.CellPiece(m.From)
	isEP := !isDirectCapture && hasAttacker && attacker.Type == board.Pawn && m.From.File != m.To.File
	if isEP {
		if ep, has := g.Board.EPSquare(); has && ep == m.To {
			victim = board.Piece{Type: board.Pawn}
			isDirectCapture = true
		}
	}
	if isDirectCapture {
		if c.ev != nil && c.ev.MainSEE {
			if mvvLvaPiece[victim.Type] >= mvvLvaPiece[attacker.Type] {
				return 1<<20 + (mvvLvaPiece[victim.Type]-mvvLvaPiece[attacker.Type])*100 + mvvLvaPiece[victim.Type], 0
			}
			return 1<<20 + mvvLvaPiece[victim.Type]*101, seeUnknown
		}
		return 1<<20 + mvvLvaPiece[victim.Type]*100 - mvvLvaPiece[attacker.Type], 0
	}
	if hasAttacker && attacker.Type == board.Pawn && (m.To.Rank == 7 || m.To.Rank == 0) {
		return 1<<20 - 100, 0
	}
	if ply < maxSearchPly {
		if c.killers[ply][0] == m {
			return 1 << 19, 0
		}
		if c.killers[ply][1] == m {
			return 1<<19 - 1, 0
		}
	}
	if c.ev != nil && c.ev.Countermoves && m == c.counterFor(color, c.prevMove) {
		return 1 << 18, 0
	}
	if hasAttacker && attacker.Type == board.Pawn && c.ev != nil && c.ev.PawnPush && advancedPawnPush(&g.Board, m, color) {
		return 1<<18 - 50, 0
	}
	score := int(c.history[color][sqIndex(m.From)][sqIndex(m.To)])
	if hasAttacker && c.ev != nil && c.ev.ContHist && c.prevMove != (game.Move{}) {
		score += int(c.cont[color][sqIndex(c.prevMove.To)][attacker.Type][sqIndex(m.To)])
	}
	return score, 0
}

// scoreMovesOneByOne keys ms the way scoreMoves did before it was fused:
// one scoreMove call per move. It is the reference scoreMoves must match.
func scoreMovesOneByOne(c *searchCtx, g *game.Game, ms []game.Move, keys []int64, ttMove game.Move, ply int, color board.Color) {
	for i, m := range ms {
		sc, sv := c.scoreMove(g, m, ttMove, ply, color)
		keys[i] = orderKey(sc, i, sv)
	}
}

// scoreMoves must give every move the key scoreMove gives it, whatever the
// features, killers, counter move, table move, histories and ply, on
// pseudo-legal lists with captures, en passant and promotions in them.
func TestScoreMovesMatchesScoreMove(t *testing.T) {
	fens := append([]string{
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
		"8/P5kP/8/8/8/8/p5Kp/1N4N1 b - - 0 1",
		"1n2k3/P1P5/8/2pP4/8/8/5p1p/4K1N1 w - c6 0 1",
		"4k3/3r4/8/3p1n2/4P3/8/8/3QK3 w - - 0 1",
	}, championBenchFENs...)
	rng := rand.New(rand.NewSource(5))
	c := &searchCtx{}
	randomMove := func(list []game.Move) game.Move {
		switch rng.Intn(4) {
		case 0:
			return game.Move{}
		case 1:
			return game.Move{From: board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}, To: board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}}
		default:
			return list[rng.Intn(len(list))]
		}
	}
	for trial := 0; trial < 4000; trial++ {
		g, err := game.ParseFEN(fens[trial%len(fens)])
		if err != nil {
			t.Fatal(err)
		}
		for n := rng.Intn(30); n > 0; n-- {
			legal := g.AllLegalMoves(g.Turn)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			g.ApplyMove(m.From, m.To)
		}
		color := g.Turn
		list, _ := g.AppendPseudoLegalMoves(nil, color, false)
		if len(list) == 0 {
			continue
		}

		c.ev = nil
		if rng.Intn(5) != 0 {
			c.ev = &Eval{MainSEE: rng.Intn(2) == 0, Countermoves: rng.Intn(2) == 0, PawnPush: rng.Intn(2) == 0, ContHist: rng.Intn(2) == 0}
		}
		ply := []int{0, 1, 7, maxSearchPly - 1, maxSearchPly, maxSearchPly + 3}[rng.Intn(6)]
		if ply < maxSearchPly {
			c.killers[ply] = [2]game.Move{randomMove(list), randomMove(list)}
		}
		c.prevMove = randomMove(list)
		if c.prevMove != (game.Move{}) {
			c.counter[color][sqIndex(c.prevMove.From)][sqIndex(c.prevMove.To)] = randomMove(list)
		}
		for _, m := range list {
			c.history[color][sqIndex(m.From)][sqIndex(m.To)] = int32(rng.Int63n(1<<32) - 1<<31)
			for pt := range 6 {
				c.cont[color][sqIndex(c.prevMove.To)][pt][sqIndex(m.To)] = int32(rng.Int63n(1<<32) - 1<<31)
			}
		}
		ttMove := randomMove(list)

		want := make([]int64, len(list))
		scoreMovesOneByOne(c, g, list, want, ttMove, ply, color)
		got := make([]int64, len(list))
		c.scoreMoves(g, list, got, ttMove, ply, color)
		for i := range list {
			if got[i] != want[i] {
				t.Fatalf("trial %d, %s, ply %d, ev %+v: move %v keyed %d, scoreMove gives %d", trial, g.FEN(), ply, c.ev, list[i], got[i], want[i])
			}
		}
	}
}

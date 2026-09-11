package engine

import (
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"chess/board"
	"chess/game"
	"chess/moves"
)

// Standard search techniques from the chess-programming literature, all
// of which this engine was missing:
//
//   - Iterative deepening (Slate & Atkin, 1977): search depth 1, 2, ... N
//     rather than N directly. Sounds wasteful -- it is not, because each
//     iteration leaves the best move in the transposition table, so the
//     next iteration tries it first everywhere and alpha-beta prunes far
//     harder. The shallow searches cost a small fraction of the deep one.
//   - Principal Variation Search / NegaScout (Reinefeld, 1983): once the
//     first move has established a bound, search the rest with a
//     zero-width window, which either confirms cheaply that they are
//     worse (the common case) or triggers a re-search.
//   - Killer moves (Akl & Newborn, 1977): a quiet move that caused a
//     beta cutoff at some other node of the same ply is likely to cut
//     off here too, so try it early.
//   - History heuristic (Schaeffer, 1989): keep a per (from, to) success
//     count across the whole search and order quiet moves by it.
//   - Late Move Reductions: after the first few moves at a node, ordering
//     says the rest are probably bad, so search them shallower and only
//     re-search at full depth if one unexpectedly beats alpha.
//
// The ordering these produce is the point: alpha-beta's pruning power
// depends almost entirely on searching the best move first.

const maxSearchPly = 64

// searchCtxPool recycles search contexts.
//
// A searchCtx carries the history table, which at [2][64][64] is 32 KB
// even as int32, plus killers and the repetition path. Allocating one per
// search made the training workload 170 KB and 132 allocations per move,
// 86% of all bytes allocated, and every one of those had to be zeroed and
// later collected.
//
// Recycled rather than cleared: the history heuristic is a move-ordering
// hint, and carrying it between searches is what strong engines do
// deliberately, since a move that caused cutoffs a moment ago probably
// still will. Stale killers are harmless for the same reason.
var searchCtxPool = sync.Pool{New: func() any { return new(searchCtx) }}

// reset clears everything a previous search left behind.
//
// The pool hands back a used context and nothing was clearing it. Three
// separate corruptions followed, and the first is the serious one:
//
//   - `path` holds the Zobrist key of every position on the current line,
//     and `isRepetition` scans path[0..ply]. PlayerScoreWith never set
//     path[0], so at ply 1 the search compared against the root key of
//     some earlier, unrelated search. In self-play those positions recur
//     constantly, so a legitimate position was scored as a draw. That is
//     a wrong number, not a slow one, and PlayerScoreWith is what labels
//     the training data.
//   - `deadline` and `aborted` survived from a timed search, so a later
//     untimed call could abort at its first node and return 0.
//   - `killers` and `history` carried over, changing move ordering. That
//     alone should not change a value in sound alpha-beta, and measuring
//     whether it does is now possible because the other two are gone.
//
// Called once per top-level search, not per iteration: iterative
// deepening wants its killers and history to persist across depths, which
// is the whole point of keeping them on the context.
func (c *searchCtx) reset() {
	c.killers = [maxSearchPly][2]game.Move{}
	c.counter = [2][64][64]game.Move{}
	c.moveStack = [maxSearchPly]game.Move{}
	c.prevMove = game.Move{}
	c.history = [2][64][64]int32{}
	c.path = [maxSearchPly]uint64{}
	c.abortAtNodes = 0
	c.checkMask = 2047
	c.stop = nil
	c.played = nil
	c.nodes = 0
	c.aborted = false
	c.deadline = time.Time{}
}

// futilityMargin is how much a single move is assumed to be worth, per
// remaining ply, in pawns. A position further than this from the bound is
// treated as unreachable. Indexed by depth; only 1..3 are used.
var futilityMargin = [4]float64{0, 1.0, 2.0, 3.0}

// lmrTable is the reduction for a quiet move at a given depth and move
// number, precomputed because it is read at every node.
//
// The shape is the standard one: roughly 0.5 + ln(depth)*ln(move)/2.5,
// so the reduction grows slowly with both and stays modest at the
// shallow depths this engine searches.
var lmrReductions [64][64]int

func init() {
	for d := 1; d < 64; d++ {
		for m := 1; m < 64; m++ {
			r := 0.5 + math.Log(float64(d))*math.Log(float64(m))/2.5
			lmrReductions[d][m] = int(r)
		}
	}
}

// lmrReduction is the late move reduction for a quiet move: logarithmic
// in depth and move number, two thirds of that at a PV node, one ply
// less for a killer, none for a promotion, never below one ply once it
// applies and never into quiescence.
func lmrReduction(depth, moveIndex int, pvNode, isKiller, promoted bool) int {
	if depth < 3 || promoted {
		return 0
	}
	r := lmrTable(depth, moveIndex)
	if pvNode {
		r = r * 2 / 3
	}
	if isKiller {
		r--
	}
	if r > depth-2 {
		r = depth - 2
	}
	if r < 1 {
		if isKiller {
			return 0
		}
		r = 1
	}
	return r
}

func lmrTable(depth, moveIndex int) int {
	d, m := depth, moveIndex+1
	if d > 63 {
		d = 63
	}
	if m > 63 {
		m = 63
	}
	return lmrReductions[d][m]
}

// lastSearchNodes is the node count of the most recent search, for
// reporting nodes per second. Not safe to read from concurrent searches;
// intended for single-threaded benchmarking.
var lastSearchNodes int64

// LastSearchNodes is the node count of the most recent search.
//
// Diagnostic only: nothing in the search reads it, so the races it used to
// have could not change a game. They could still change a measurement, and
// did: a depth diagnostic read it while ten parallel games were writing it.
func LastSearchNodesValue() int { return int(atomic.LoadInt64(&lastSearchNodes)) }

type searchCtx struct {
	ev         *Eval
	killers    [maxSearchPly][2]game.Move
	history    [2][64][64]int32
	quiescence bool
	nodes      int
	extensions bool
	// path holds the Zobrist key of every position on the line currently
	// being searched, so a repetition can be recognised as a draw.
	//
	// Without this the search cannot see that shuffling a piece back and
	// forth goes nowhere, and it showed: analysis against Stockfish caught
	// the engine playing Be3-c1, Bc1-g5, Bg5-c1, matches ran 36-40% draws,
	// and an extra ply of search was worth +14 +/- 27 Elo when it should be
	// worth 50-70. A deeper search without repetition detection mostly
	// finds more elaborate ways to go round in circles.
	path [maxSearchPly]uint64
	// played holds positions that already occurred in the real game.
	// Repeating one of those is a draw by repetition, which the search
	// must be able to see coming, especially when it is winning.
	played map[uint64]int
	// deadline aborts the search when a time budget runs out, and aborted
	// records that it happened.
	//
	// Checking only between iterations is not enough: one iteration can
	// overrun without bound, and did. Three games in every five hundred
	// stopped making progress at all, because a single depth on some
	// position ran for minutes inside a 32 ms budget.
	deadline time.Time
	// abortAtNodes is a test hook: abort once this many nodes have been
	// searched, so what happens at a cut-off can be tested exactly instead
	// of by racing a clock.
	abortAtNodes int64
	// checkMask decides how often the clock is read: on every node whose
	// count has no bits in common with it. 2047 for a budget of hundreds of
	// milliseconds, far fewer for a 10 ms one, where 2048 nodes is half the
	// budget and the search overran by 113%.
	checkMask int
	// stop is shared by the threads of one parallel search: set once the
	// main thread has its move, so the helpers abandon theirs.
	stop    *int32
	aborted bool
	acc     [accSlots]halfKPAcc
	// counter[colour][from][to] is the quiet move that last refuted the
	// move from->to played by the other side; moveStack[ply] is the move
	// that led to ply+1, so a node's previous move is moveStack[ply-1].
	counter   [2][64][64]game.Move
	moveStack [maxSearchPly]game.Move
	prevMove  game.Move
}

// recordCounter remembers reply as the refutation of prev by colour.
func (c *searchCtx) recordCounter(colour board.Color, prev, reply game.Move) {
	if prev == (game.Move{}) {
		return
	}
	c.counter[colour][sqIndex(prev.From)][sqIndex(prev.To)] = reply
}

// counterFor is the remembered refutation of prev by colour, or zero.
func (c *searchCtx) counterFor(colour board.Color, prev game.Move) game.Move {
	if prev == (game.Move{}) {
		return game.Move{}
	}
	return c.counter[colour][sqIndex(prev.From)][sqIndex(prev.To)]
}

// isRepetition reports whether this position already appears on the
// current search line, or twice in the game before the search began.
func (c *searchCtx) isRepetition(key uint64, ply int) bool {
	for i := 0; i < ply && i < maxSearchPly; i++ {
		if c.path[i] == key {
			return true
		}
	}
	// Two prior occurrences plus this one is threefold. One prior
	// occurrence is not yet a draw, so it is not treated as one.
	return c.played[key] >= 2
}

func sqIndex(s board.Sq) int { return s.Rank*8 + s.File }

// isKiller reports whether m is one of this ply's killer moves.
func (c *searchCtx) isKiller(ply int, m game.Move) bool {
	return ply < maxSearchPly && (c.killers[ply][0] == m || c.killers[ply][1] == m)
}

func (c *searchCtx) recordKiller(ply int, m game.Move) {
	if ply >= maxSearchPly {
		return
	}
	if c.killers[ply][0] == m {
		return
	}
	c.killers[ply][1] = c.killers[ply][0]
	c.killers[ply][0] = m
}

func (c *searchCtx) recordHistory(color board.Color, m game.Move, depth int) {
	c.history[color][sqIndex(m.From)][sqIndex(m.To)] += int32(depth * depth)
}

// scoreMove ranks a move for ordering: transposition-table move first,
// then captures by MVV-LVA, then killers, then history.
func (c *searchCtx) scoreMove(g *game.Game, m game.Move, ttMove game.Move, ply int, color board.Color) int {
	if m == ttMove {
		return 1 << 30
	}
	if isCaptureMove(g, m) {
		victim, onSquare := g.Board.PieceAt(m.To)
		if !onSquare {
			victim = board.Piece{Type: board.Pawn} // en passant
		}
		attacker, _ := g.Board.PieceAt(m.From)
		if c.ev != nil && c.ev.MainSEE {
			// Winning captures first by what they win, losing captures
			// after every quiet move.
			if x := see(&g.Board, m); x < 0 {
				return -1<<20 + x*100
			} else {
				return 1<<20 + x*100 + mvvLvaPiece[victim.Type]
			}
		}
		return 1<<20 + mvvLvaPiece[victim.Type]*100 - mvvLvaPiece[attacker.Type]
	}
	if ply < maxSearchPly {
		if c.killers[ply][0] == m {
			return 1 << 19
		}
		if c.killers[ply][1] == m {
			return 1<<19 - 1
		}
	}
	if c.ev != nil && c.ev.Countermoves && m == c.counterFor(color, c.prevMove) {
		return 1 << 18
	}
	return int(c.history[color][sqIndex(m.From)][sqIndex(m.To)])
}

func (c *searchCtx) orderMoves(g *game.Game, ms []game.Move, ttMove game.Move, ply int, color board.Color) {
	// Insertion sort by descending score: move lists are short (tens of
	// entries), so this beats a general sort with its allocation and
	// comparator indirection.
	// On the stack for any realistic move list; a fresh slice per node
	// was 8% of all bytes allocated.
	var scoreBuf [96]int
	scores := scoreBuf[:0]
	if len(ms) > len(scoreBuf) {
		scores = make([]int, 0, len(ms))
	}
	scores = scores[:len(ms)]
	for i, m := range ms {
		scores[i] = c.scoreMove(g, m, ttMove, ply, color)
	}
	for i := 1; i < len(ms); i++ {
		m, sc := ms[i], scores[i]
		j := i - 1
		for j >= 0 && scores[j] < sc {
			ms[j+1], scores[j+1] = ms[j], scores[j]
			j--
		}
		ms[j+1], scores[j+1] = m, sc
	}
}

// search is a negamax-style alpha-beta from `color`'s point of view,
// with the score always relative to `maximizingFor`.
func (c *searchCtx) search(g *game.Game, color, maximizingFor board.Color, depth, ply int, alpha, beta float64) float64 {
	return c.searchNull(g, color, maximizingFor, depth, ply, alpha, beta, false)
}

// searchNull carries whether the parent node just played a null move.
//
// Two nulls in a row hand the move back to the side that passed, on an
// unchanged position, so the search evaluates a line neither player can
// reach and returns a bound derived from it. Every engine forbids it and
// this one did not, and the arithmetic says exactly where it starts to
// bite: a null costs three plies, so depth 5 reaches depth 2 and cannot
// null again, while depth 6 reaches depth 3 and can. Measured across that
// boundary with the same evaluation on both sides, depth 6 lost to depth 5
// by 125 +/- 36 Elo, when a ply is normally worth 50 to 100 the other way.
func (c *searchCtx) searchNull(g *game.Game, color, maximizingFor board.Color, depth, ply int, alpha, beta float64, afterNull bool) float64 {
	// The clock is read every 2048 nodes rather than every node: time.Now
	// is a syscall-ish read and this is the hottest loop in the engine.
	// 2048 nodes is well under a millisecond, so the overrun it allows is
	// far smaller than the one it prevents.
	if c.abortAtNodes > 0 && int64(c.nodes) >= c.abortAtNodes {
		c.aborted = true
		return 0
	}
	if c.aborted {
		return 0
	}
	if c.nodes&c.checkMask == 0 {
		if !c.deadline.IsZero() && time.Now().After(c.deadline) {
			c.aborted = true
			return 0
		}
		// A helper thread stops once the main thread has its move.
		if c.stop != nil && atomic.LoadInt32(c.stop) != 0 {
			c.aborted = true
			return 0
		}
	}
	c.nodes++
	tt := c.ev.table()

	var ttMove game.Move
	// Hashed with `color`, the side to move at this node, and not with
	// g.Turn.
	//
	// This search makes and unmakes moves on g.Board and never touches
	// g.Turn, so g.Turn stays whatever it was at the root for the whole
	// tree. Hashing it meant every node was keyed as though the root's
	// side were to move, and two positions that differ only in whose turn
	// it is shared a key. The table then handed one node's score to the
	// other: at depth 4 in the Kiwipete position it returned +1.107833 for
	// a node actually worth -6.333333, and the root played a move 3.6
	// pawns worse than the best one while reporting the correct score for
	// the move it did not play.
	key := zobristBoard(&g.Board, color)
	if ply > 0 && depth > 0 && !(c.ev != nil && c.ev.NoRepetition) && c.isRepetition(key, ply) {
		// A draw, scored 0 regardless of whose turn it is. This is
		// deliberately checked before the transposition table: the table
		// keys on the position, not on how the game reached it, so it
		// cannot distinguish a first visit from a repetition.
		return 0
	}
	if ply < maxSearchPly {
		c.path[ply] = key
	}
	if deadPosition(&g.Board) {
		return 0
	}
	if tt != nil && depth > 0 {
		if score, ok := tt.probe(key, depth, maximizingFor, alpha, beta); ok {
			return score
		}
		if m, ok := tt.bestMove(key); ok {
			ttMove = m
		}
	}
	if c.ev != nil && c.ev.IIR && iirReduces(depth, ttMove != (game.Move{})) {
		depth--
	}
	// The network accumulator for this node, derived from the parent's.
	// After the table probe on purpose: a node that cuts off there has no
	// children and no evaluation, so it never needs one.
	c.ev.setAccPly(&g.Board, ply)
	origAlpha, origBeta := alpha, beta

	// 96, not 64: a middlegame with the queens out has 50 to 60 legal
	// moves, and every position past the buffer's capacity reallocated
	// on the heap. The allocation profile put 48% of all bytes there.
	var moveBuf [96]game.Move
	legal, inCheck := g.AppendLegalMovesInCheck(moveBuf[:0], color)
	if len(legal) == 0 {
		return terminalScore(g, color, maximizingFor, depth)
	}
	if depth <= 0 {
		if c.quiescence {
			return quiesce(g, color, maximizingFor, alpha, beta, c.ev, 0, ply)
		}
		return evalPositionFor(g, color, maximizingFor, c.ev)
	}

	maximizing := color == maximizingFor

	// Futility pruning (Heinz, 1998). Near the leaves, a position already
	// far outside the window is very unlikely to be dragged back inside by
	// one quiet move, because a quiet move is worth much less than the
	// margin. Two uses of the same idea:
	//
	//   - reverse futility, here: the static score is so far past the
	//     cutoff bound that we return it without searching at all;
	//   - forward futility, in the move loop: the static score is so far
	//     short of the bound that individual quiet moves are skipped.
	//
	// Both are unsound in the strict sense -- a tactic can beat the margin
	// -- so they are switched off when in check, and captures and checking
	// moves are never pruned, since those are exactly the moves that move
	// the score by more than a margin.
	// A window whose bound is already a mate score means a forced mate is
	// in play, where a static margin says nothing useful. No test here
	// distinguishes the guard (mate-in-1, mate-in-2 and K+R vs K all
	// convert either way), so it is insurance rather than a proven fix,
	// kept because it costs two comparisons on a path that already
	// evaluates the position.
	const mateBound = mateScore - maxSearchPly
	staticEval, haveStatic := 0.0, false
	futile := c.ev != nil && c.ev.Futility && !inCheck && depth <= 3 &&
		alpha > negInf && beta < posInf &&
		alpha > -mateBound && beta < mateBound
	if futile {
		staticEval, haveStatic = evalPositionFor(g, color, maximizingFor, c.ev), true
		margin := futilityMargin[depth]
		if maximizing && staticEval-margin >= beta {
			return staticEval - margin
		}
		if !maximizing && staticEval+margin <= alpha {
			return staticEval + margin
		}
	}

	// Deep reverse futility, depths 4 to 7 at zero-window nodes.
	if c.ev != nil && c.ev.DeepRFP && depth >= 4 && depth <= 7 && !inCheck && zeroWindow(alpha, beta) {
		if !haveStatic {
			staticEval, haveStatic = evalPositionFor(g, color, maximizingFor, c.ev), true
		}
		if reverseFutilityCuts(depth, staticEval, alpha, beta, maximizing, inCheck) {
			return staticEval
		}
	}
	nullOK := true
	if c.ev.useNullMove() && c.ev.NullGate && depth >= 3 && !inCheck && !afterNull {
		if !haveStatic {
			staticEval, haveStatic = evalPositionFor(g, color, maximizingFor, c.ev), true
		}
		bound := beta
		if !maximizing {
			bound = alpha
		}
		nullOK = nullMoveAllowed(staticEval, bound, maximizing)
	}
	if c.ev.useNullMove() && depth >= 3 && !inCheck && !afterNull && nullOK {
		// Clear the en passant square across the null move.
		//
		// A null hands the move to the opponent without a move being
		// played, so any en passant right belonged to the side that is
		// now passing and must not survive. Leaving it set lets the
		// opponent's generator "capture en passant" onto a square nobody
		// double-pushed to, which removes a pawn that is not there and
		// whose unmake then puts a phantom pawn on the board.
		//
		// The damage was not confined to the null-move subtree: the
		// phantom survived back to the caller. After 1.e4, scoring the
		// position at depth 3 returned a board with a black pawn on e2.
		// That corrupted the game being replayed in the PGN importer,
		// where it showed up as 47% of games "failing to replay", and it
		// silently corrupted self-play generation too, where a wrong
		// position does not announce itself: the engine simply searched
		// and labelled a position that never occurred.
		r := c.ev.NullReduction
		if r <= 0 {
			r = 3
		}
		if c.ev.NullScale {
			r += depth / 6
		}
		if c.ev.NullGate {
			over := staticEval - beta
			if !maximizing {
				over = alpha - staticEval
			}
			r = nullMoveReduction(depth, over)
		}
		ep, hadEP := g.Board.EPSquare()
		if !c.ev.KeepNullMoveEP {
			g.Board.SetEPSquare(board.Sq{}, false)
		}
		if ply < maxSearchPly {
			c.moveStack[ply] = game.Move{}
		}
		score := c.searchNull(g, color.Other(), maximizingFor, depth-r, ply+1, alpha, beta, true)
		g.Board.SetEPSquare(ep, hadEP)
		if c.aborted {
			return 0
		}
		if maximizing && score >= beta {
			return score
		}
		if !maximizing && score <= alpha {
			return score
		}
	}

	if ply > 0 {
		c.prevMove = c.moveStack[ply-1]
	} else {
		c.prevMove = game.Move{}
	}
	c.orderMoves(g, legal, ttMove, ply, color)

	best := negInf
	if !maximizing {
		best = posInf
	}
	bestMove := legal[0]

	for i, m := range legal {
		isCapture := isCaptureMove(g, m)
		exchange := 0
		if isCapture && c.ev != nil && c.ev.MainSEE && depth <= 6 {
			exchange = see(&g.Board, m)
		}

		// Make/unmake rather than copying the board into a child Game:
		// this is the hot path, and the copy was the largest per-node cost
		// left. Promotion is handled here because the board layer does not
		// know the rule.
		undo, promoted := makeSearchMove(g, m)
		if ply < maxSearchPly {
			c.moveStack[ply] = m
		}
		// Recurse on the same Game: search reads the board through g and
		// takes the side to move as a parameter, so there is no need to
		// build a child object at all.

		lmp := c.ev != nil && c.ev.LMP && zeroWindow(alpha, beta)
		givesCheck := false
		if c.extensions || futile || lmp || (isCapture && c.ev != nil && c.ev.MainSEE) {
			givesCheck = moves.IsInCheck(&g.Board, color.Other())
		}

		// Losing captures at shallow depth on a zero window are not worth
		// their subtree either.
		if isCapture && c.ev != nil && c.ev.MainSEE && i > 0 && seePrunes(depth, exchange, inCheck, givesCheck, zeroWindow(alpha, beta)) {
			g.Board.UnmakeMove(undo)
			continue
		}

		// Late move pruning, at zero-window nodes only: the ordering has
		// put this quiet move far down the list at a depth where even a
		// reduced search of it is not worth the nodes.
		if lmp && i > 0 && lateMovePruned(depth, i, false, inCheck, isCapture, promoted, givesCheck, c.isKiller(ply, m)) {
			g.Board.UnmakeMove(undo)
			continue
		}

		// Forward futility: a quiet, non-checking, non-promoting move this
		// far short of the bound is not going to reach it, so skip its
		// whole subtree. The first move is always searched so that `best`
		// is backed by a real score.
		if futile && i > 0 && !isCapture && !givesCheck && !promoted {
			margin := futilityMargin[depth]
			if (maximizing && staticEval+margin <= alpha) ||
				(!maximizing && staticEval-margin >= beta) {
				g.Board.UnmakeMove(undo)
				continue
			}
		}

		// Late move reductions: the ordering above says moves after the
		// first few are unlikely to be best, so look at them shallower.
		reduction := 0
		if depth >= 3 && i >= 3 && !isCapture && !inCheck && !(c.ev != nil && c.ev.NoLMR) {
			reduction = 1
			if c.ev != nil && c.ev.ScaledLMR {
				// Reduce more the deeper the search and the later the
				// move. A flat one-ply reduction treats the fourth move
				// and the fortieth alike, and treats a depth-3 node like a
				// depth-12 one, when the move ordering says the fortieth
				// move at high depth is far less likely to be best.
				//
				// The logarithmic form is what strong engines use: it
				// grows without ever reducing so much that a good move
				// cannot come back, and the re-search on a fail-high
				// catches the cases where it was wrong.
				reduction = lmrReduction(depth, i, !zeroWindow(alpha, beta), c.isKiller(ply, m), promoted)
			}
		}

		// Check extension: a forced sequence should not be cut off
		// half-way. If the move gives check, spend an extra ply so the
		// search sees how the check resolves instead of evaluating a
		// position that is about to change sharply.
		extension := 0
		if c.extensions && ply < maxSearchPly-2 && givesCheck {
			extension = 1
			reduction = 0
		}

		var value float64
		if i == 0 {
			value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
		} else {
			// Principal variation search: try a zero-width window first.
			if maximizing {
				value = c.search(g, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, alpha, alpha+1e-6)
				if value > alpha {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			} else {
				value = c.search(g, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, beta-1e-6, beta)
				if value < beta {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			}
		}

		g.Board.UnmakeMove(undo)
		// An aborted child returned 0, not a score. Unwind without
		// storing: the table is reused for the rest of the game, and a
		// timed search that ran out of clock was leaving zeros in it for
		// the next move to believe.
		if c.aborted {
			return 0
		}

		if maximizing {
			if value > best {
				best, bestMove = value, m
			}
			if best > alpha {
				alpha = best
			}
		} else {
			if value < best {
				best, bestMove = value, m
			}
			if best < beta {
				beta = best
			}
		}
		if beta <= alpha {
			if OrderingStats {
				recordCutoff(i)
			}
			if !isCapture {
				c.recordKiller(ply, m)
				c.recordHistory(color, m, depth)
				if ply > 0 {
					c.recordCounter(color, c.moveStack[ply-1], m)
				}
			}
			break
		}
	}

	if tt != nil && depth > 0 {
		flag := ttExact
		if best <= origAlpha {
			flag = ttUpperBound
		} else if best >= origBeta {
			flag = ttLowerBound
		}
		tt.storeWithMove(key, best, depth, flag, maximizingFor, bestMove)
	}
	return best
}

// ChooseMoveIterative is the modern entry point: iterative deepening with
// killers, history, PVS and LMR, reusing one transposition table across
// iterations.
func ChooseMoveIterative(g *game.Game, color board.Color, maxDepth int, ev *Eval, useQuiescence bool) (game.Move, bool) {
	return ChooseMoveIterativeTimed(g, color, maxDepth, ev, useQuiescence, 0)
}

// ChooseMoveIterativeTimed is the same search under a time budget.
//
// When budget is positive the search keeps deepening until it predicts the
// next iteration would overrun, and plays the deepest completed result.
// The prediction uses the measured branching factor of this search rather
// than a constant, because it varies from 1.9 to 4.6 per ply depending on
// the position, and a fixed guess would either stop a ply early
// everywhere or overrun on tactical positions.
//
// It stops between iterations rather than inside one. Aborting mid-search
// would need every partial result discarded, and a search that returns a
// half-explored score is worse than one ply less of a complete one.
func ChooseMoveIterativeTimed(g *game.Game, color board.Color, maxDepth int, ev *Eval, useQuiescence bool, budget time.Duration) (game.Move, bool) {
	m, _, ok := chooseMoveIterativeScored(g, color, maxDepth, ev, useQuiescence, budget)
	return m, ok
}

// chooseMoveIterativeScored also returns the score of the move it chose,
// from the point of view of the side to move. Returned rather than stored
// in a package variable because games run in parallel: a global would be
// whichever game wrote last.
func chooseMoveIterativeScored(g *game.Game, color board.Color, maxDepth int, ev *Eval, useQuiescence bool, budget time.Duration) (game.Move, float64, bool) {
	return chooseMoveIterativeScoredThreads(g, color, maxDepth, ev, useQuiescence, budget, 1)
}

// chooseMoveIterativeScoredThreads is Lazy SMP: threads-1 helpers search
// the same position, staggered a ply apart, sharing the transposition
// table and nothing else. Their moves are discarded. What they contribute
// is the table, which the main search then finds already filled, and the
// gain is plies reached per second on a machine with cores to spare.
//
// Each helper gets its own game (the search makes and unmakes moves on
// it), its own Eval (the evaluation writes STM and its accumulator through
// that pointer at every node) and its own context. Only the table is
// shared, and it locks itself once told it is.
func chooseMoveIterativeScoredThreads(g *game.Game, color board.Color, maxDepth int, ev *Eval, useQuiescence bool, budget time.Duration, threads int) (game.Move, float64, bool) {
	if ev == nil {
		ev = &Eval{}
	}
	if ev.Table == nil {
		ev.Table = NewTranspositionTable(20)
	}
	atomic.StoreInt64(&lastSearchNodes, 0)
	if threads <= 1 {
		return searchIterative(g, color, maxDepth, ev, useQuiescence, budget, nil, 0)
	}
	ev.Table.share()
	var shared smpShared
	var wg sync.WaitGroup
	for i := 1; i < threads; i++ {
		// Copied here, before the main search starts writing to g and ev,
		// not inside the goroutine where it would race with those writes.
		hg := *g
		hg.Board = g.Board.Clone()
		hev := *ev
		wg.Add(1)
		go func(seed int64, hg game.Game, hev Eval) {
			defer wg.Done()
			searchIterative(&hg, color, maxDepth, &hev, useQuiescence, budget, &shared, seed)
		}(int64(i), hg, hev)
	}
	m, score, ok := searchIterative(g, color, maxDepth, ev, useQuiescence, budget, &shared, 0)
	atomic.StoreInt32(&shared.stop, 1)
	wg.Wait()
	return m, score, ok
}

// clockCheckMask is how many nodes may pass between two reads of the clock,
// minus one, for a given budget. Reading time.Now is not free and this is
// the hottest loop in the engine, so a long budget reads it every 2048
// nodes; a 10 ms budget cannot afford that, since 2048 nodes is three to
// five milliseconds and the search overran by 113% on the test positions.
// Sixty-four nodes is well under a millisecond and costs nothing visible.
func clockCheckMask(budget time.Duration) int {
	switch {
	case budget < 5*time.Millisecond:
		return 15
	case budget < 50*time.Millisecond:
		return 63
	case budget < 250*time.Millisecond:
		return 511
	default:
		return 2047
	}
}

// smpShared is what the threads of one parallel search share besides the
// table: the stop signal, and the main thread's completed depth so the
// helpers can stay ahead of it. Helpers that iterate in lockstep with the
// main thread search the tree it is searching and store nothing it has not
// already found: measured, eight threads searched 5.6 times the nodes and
// reached exactly the same depth.
type smpShared struct {
	stop      int32
	mainDepth int32
}

// searchIterative is one thread's iterative deepening. main marks the
// thread whose move is played and whose diagnostics are recorded.
func searchIterative(g *game.Game, color board.Color, maxDepth int, ev *Eval, useQuiescence bool, budget time.Duration, shared *smpShared, seed int64) (game.Move, float64, bool) {
	// Seed 0 is the main thread, whose move is played and whose diagnostics
	// are recorded. Helpers start a ply or two higher and shuffle their root
	// order, so they explore what the main thread has not reached yet
	// instead of racing it through the same tree.
	main := seed == 0
	startDepth := 1
	var rng *rand.Rand
	if !main {
		startDepth = 1 + int(seed%3)
		rng = rand.New(rand.NewSource(seed))
	}
	legal := g.AllLegalMoves(color)
	if ev != nil && ev.NoCastle {
		kept := legal[:0]
		for _, m := range legal {
			if p, ok := g.Board.PieceAt(m.From); ok && p.Type == board.King &&
				(m.To.File-m.From.File == 2 || m.From.File-m.To.File == 2) {
				continue
			}
			kept = append(kept, m)
		}
		legal = kept
	}
	if len(legal) == 0 {
		return game.Move{}, 0, false
	}
	if ev == nil {
		ev = &Eval{}
	}
	if ev.Table == nil {
		ev.Table = NewTranspositionTable(20)
	}
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	if shared != nil {
		ctx.stop = &shared.stop
	}
	if main {
		ctx.abortAtNodes = testAbortAtNodes
	}
	ctx.ev, ctx.quiescence, ctx.extensions = ev, useQuiescence, ev.Extensions
	ctx.ev.acc = &ctx.acc
	ctx.acc[0].valid = false
	if budget > 0 {
		ctx.checkMask = clockCheckMask(budget)
		// Five percent past the budget, hard. The between-iteration check
		// below deliberately starts iterations that may not finish, so
		// this abort is the usual way a move ends, not a backstop; an
		// aborted iteration is discarded and costs nothing now that its
		// parents no longer store half-finished scores.
		ctx.deadline = time.Now().Add(budget * 21 / 20)
	}
	ctx.played = playedKeys(g)
	ctx.path[0] = zobristHash(g)
	completed := 0
	defer func() {
		// Every thread adds its nodes; only the main thread's depth counts.
		atomic.AddInt64(&lastSearchNodes, int64(ctx.nodes))
		if main {
			atomic.StoreInt64(&lastSearchDepth, int64(completed))
		}
	}()

	best := legal[0]
	prevScore := 0.0
	start := time.Now()
	for depth := startDepth; depth <= maxDepth; depth++ {
		if !main && shared != nil {
			// Stay ahead of the main thread: one ply past what it has
			// completed, two for the odd-numbered helpers.
			if lead := int(atomic.LoadInt32(&shared.mainDepth)) + 1 + int(seed%2); lead > depth {
				depth = lead
			}
			if depth > maxDepth {
				break
			}
		}
		if budget > 0 && depth > startDepth {
			elapsed := time.Since(start)
			// Stop outright once the budget is spent. The prediction below
			// is not enough on its own: in a trivial position the early
			// iterations take microseconds, so a predicted cost of three
			// times the last one rounds to nothing and the search keeps
			// deepening until one iteration explodes. Two games out of 500
			// hung on exactly that.
			if elapsed >= budget {
				break
			}
			// Otherwise start the next iteration however little time is left.
			// It will usually be cut off, and that used to waste the time; now
			// a move that completes and beats the standing choice is played,
			// so nothing is lost. The budget is per move, so there is nothing
			// to save the time for either.
		}
		// Aspiration window: the score at depth N is usually close to the
		// score at N-1, so search a narrow window around it. Most searches
		// then run with far tighter bounds and prune much harder; the
		// occasional miss costs one re-search with a full window.
		alpha, beta := negInf, posInf
		if ev.Aspiration && depth >= 3 && depth > startDepth {
			const window = 0.5
			alpha, beta = prevScore-window, prevScore+window
		}

	researchFullWindow:
		if main && testRootScores != nil {
			clear(testRootScores)
		}
		bestScore := negInf
		var iterBest game.Move
		var tied []game.Move

		ordered := make([]game.Move, len(legal))
		copy(ordered, legal)
		ctx.orderMoves(g, ordered, best, 0, color)
		if rng != nil && len(ordered) > 2 {
			// Helpers keep the best-known move first and shuffle the rest.
			rest := ordered[1:]
			rng.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
		}

		for _, m := range ordered {
			undo, _ := makeSearchMove(g, m)
			// Every move after the first is searched against the best score so
			// far, as the tree below does at every node; the root used the same
			// window for all of them. A hair below the best, not at it, so a
			// move that is exactly equal still returns an exact score and the
			// tie-break among equal moves keeps working.
			moveAlpha := alpha
			if bestScore-1e-6 > moveAlpha {
				moveAlpha = bestScore - 1e-6
			}
			score := ctx.search(g, color.Other(), color, depth-1, 1, moveAlpha, beta)
			g.Board.UnmakeMove(undo)
			if ctx.aborted {
				// A child cut off mid-search returned nothing usable.
				break
			}
			if main && testRootScores != nil {
				testRootScores[m] = score
			}
			if score > bestScore {
				bestScore, iterBest = score, m
				tied = tied[:0]
				tied = append(tied, m)
			} else if score == bestScore {
				tied = append(tied, m)
			}
		}
		if ctx.aborted {
			// The clock ran out inside this iteration. Every move that did
			// complete was searched one ply deeper than the move about to be
			// played, so one that beat the standing choice on an exact score
			// is the better move by the deeper search. The standing choice is
			// ordered first, so a different iterBest means exactly that; the
			// moves that were never reached are no worse off than before.
			if iterBest != (game.Move{}) && iterBest != ordered[0] && bestScore > alpha && bestScore < beta {
				best = iterBest
			}
			break
		}
		// The true score fell outside the aspiration window, so the search
		// result is only a bound: redo this depth with a full window.
		if (bestScore <= alpha || bestScore >= beta) && (alpha != negInf || beta != posInf) {
			alpha, beta = negInf, posInf
			goto researchFullWindow
		}
		prevScore = bestScore
		completed = depth
		if main && shared != nil {
			atomic.StoreInt32(&shared.mainDepth, int32(depth))
		}

		if len(tied) > 1 {
			// Same anti-repetition tie-break as the simple search: prefer a
			// move that does not repeat a position already seen.
			if g.TrackRepetition {
				fresh := tied[:0:0]
				for _, m := range tied {
					if g.CountIfPlayed(m.From, m.To) == 0 {
						fresh = append(fresh, m)
					}
				}
				if len(fresh) > 0 {
					tied = fresh
				}
			}
			iterBest = tied[randIntn(len(tied))]
		}
		best = iterBest
	}
	return best, prevScore, true
}

// TotalNodes reports nodes visited by the most recent search, including
// quiescence nodes.
// lastSearchDepth is the deepest iteration the most recent move choice
// completed, for seeing what a time budget actually buys.
var lastSearchDepth int64

// LastSearchDepth reports it.
func LastSearchDepth() int { return int(atomic.LoadInt64(&lastSearchDepth)) }

func TotalNodes() int {
	return int(atomic.LoadInt64(&lastSearchNodes) + atomic.LoadInt64(&quiesceNodes))
}

// ResetNodes clears the counters between measurements.
func ResetNodes() {
	atomic.StoreInt64(&lastSearchNodes, 0)
	atomic.StoreInt64(&quiesceNodes, 0)
}

// playedKeys hashes the positions already seen in the game, so the search
// can recognise that reaching one again is a repetition.
//
// Built once per move choice rather than kept incrementally: a game is a
// few hundred positions and a search is millions of nodes, so this is
// noise, and an incremental version would have to be kept correct across
// every make and unmake.
func playedKeys(g *game.Game) map[uint64]int {
	boards := g.PlayedBoards()
	if len(boards) == 0 {
		return nil
	}
	out := make(map[uint64]int, len(boards))
	turn := g.Turn
	// Positions alternate colours, so walk back from the current side to
	// move: the key includes whose turn it is and two positions with
	// different sides to move are not the same position.
	for i := len(boards) - 1; i >= 0; i-- {
		out[zobristBoard(&boards[i], turn)]++
		turn = turn.Other()
	}
	return out
}

// isCaptureMove reports whether m takes a piece, en passant included.
//
// Every pruning rule used to ask "is there a piece on the destination",
// and for en passant there is not, so the one capture that removes a pawn
// from a third square was quiet to the ordering, to futility pruning, to
// reductions, and invisible to quiescence.
func isCaptureMove(g *game.Game, m game.Move) bool {
	if _, ok := g.Board.PieceAt(m.To); ok {
		return true
	}
	if p, ok := g.Board.PieceAt(m.From); ok && p.Type == board.Pawn && m.From.File != m.To.File {
		if ep, has := g.Board.EPSquare(); has && ep == m.To {
			return true
		}
	}
	return false
}

// pawnReachesLastRank reports whether m is a promotion.
func pawnReachesLastRank(g *game.Game, m game.Move) bool {
	p, ok := g.Board.PieceAt(m.From)
	return ok && p.Type == board.Pawn && (m.To.Rank == 7 || m.To.Rank == 0)
}

// makeSearchMove plays m on the board and applies promotion, which the
// board layer does not know. One copy of the rule: the main search, the
// root and quiescence each had their own, and quiescence's had none.
func makeSearchMove(g *game.Game, m game.Move) (board.Undo, bool) {
	undo := g.Board.MakeMove(m.From, m.To)
	if p, ok := g.Board.PieceAt(m.To); ok && p.Type == board.Pawn &&
		((p.Color == board.White && m.To.Rank == 7) || (p.Color == board.Black && m.To.Rank == 0)) {
		g.Board.SetPiece(m.To, board.Piece{Color: p.Color, Type: board.Queen})
		return undo, true
	}
	return undo, false
}

// deadPosition reports a position no legal sequence can mate from: bare
// kings, one minor piece, or bishops all on one colour. Scored as the
// draw it is, where a bishop used to be worth three pawns and the search
// steered into dead endings as though they were won.
func deadPosition(b *board.Board) bool {
	var buf [32]board.ColoredPiece
	minors, bishopShade, sameShade := 0, -1, true
	for _, p := range b.AppendAllPieces(buf[:0]) {
		switch p.Type {
		case board.Pawn, board.Rook, board.Queen:
			return false
		case board.Knight:
			minors++
			sameShade = false
		case board.Bishop:
			minors++
			shade := (p.Sq.File + p.Sq.Rank) & 1
			if bishopShade == -1 {
				bishopShade = shade
			} else if shade != bishopShade {
				sameShade = false
			}
		}
	}
	return minors <= 1 || sameShade
}

// lateMovePruned reports whether a quiet move this late in the list, at
// this depth, is not worth searching at all.
//
// Late move reductions look at such moves shallower; this stops looking.
// The threshold is Stockfish's: (3 + depth^2) / (2 - improving) moves,
// so at depth 3 the seventh quiet move is the first to go and at depth 5
// the fifteenth. Never a capture, a promotion, a check, a killer, or any
// move while in check, since those are exactly the moves the ordering
// cannot vouch for, and never past depth 5, where a pruned move's
// subtree would have been large enough to matter.
func lateMovePruned(depth, moveIndex int, improving, inCheck, isCapture, promoted, givesCheck, isKiller bool) bool {
	if depth > 5 || inCheck || isCapture || promoted || givesCheck || isKiller {
		return false
	}
	div := 2
	if improving {
		div = 1
	}
	return moveIndex >= (3+depth*depth)/div
}

// reverseFutilityMargin is how far a static evaluation must stand beyond
// the bound, in pawns, for a zero-window node at this depth to return it
// unsearched. Half a pawn plus a third per ply: 1.9 at depth 4, 2.95 at
// depth 7.
func reverseFutilityMargin(depth int) float64 { return 0.5 + 0.35*float64(depth) }

// reverseFutilityCuts is the deep reverse futility rule: depths 4 to 7,
// zero-window nodes only, not in check, not on a mate-bound window.
func reverseFutilityCuts(depth int, staticEval, alpha, beta float64, maximizing, inCheck bool) bool {
	const mateBound = mateScore - maxSearchPly
	if depth < 4 || depth > 7 || inCheck || !zeroWindow(alpha, beta) || alpha <= -mateBound || beta >= mateBound {
		return false
	}
	margin := reverseFutilityMargin(depth)
	if maximizing {
		return staticEval-margin >= beta
	}
	return staticEval+margin <= alpha
}

// nullMoveAllowed gates the null move on the static evaluation standing
// at or beyond the bound: from below it almost never cuts.
func nullMoveAllowed(staticEval, bound float64, maximizing bool) bool {
	if maximizing {
		return staticEval >= bound
	}
	return staticEval <= bound
}

// nullMoveReduction is the historical 3 plus a ply per four of depth plus
// a ply per pawn and a half the static evaluation stands beyond the
// bound, capped at two for the margin and at the depth itself, so the null
// search ends in quiescence at worst.
func nullMoveReduction(depth int, marginOverBound float64) int {
	r := 3 + depth/4
	bonus := int(marginOverBound / 1.5)
	if bonus > 2 {
		bonus = 2
	}
	if bonus > 0 {
		r += bonus
	}
	if r > depth {
		r = depth
	}
	return r
}

// zeroWindow reports a principal-variation-search null window. The
// windows are built as alpha+1e-6, and beta-alpha then comes out a hair
// above 1e-6 in floating point, so a test against exactly 1e-6 fired for
// some alphas and not others.
func zeroWindow(alpha, beta float64) bool { return beta-alpha <= 2e-6 }

// iirReduces is internal iterative reduction: a node at depth 6 or more
// with no table move is searched a ply shallower. Without a table move
// the ordering is weak and a full-depth search here mostly serves to
// find one; the shallower search finds it too, and the re-search that
// iterative deepening amounts to is cheaper than the wasted depth.
func iirReduces(depth int, hasTTMove bool) bool { return depth >= 6 && !hasTTMove }

// Move ordering quality, measured rather than assumed.
//
// Alpha-beta's cost is set by how often a cutoff is found with the first
// move searched: first-move cutoffs cost one subtree, fifth-move cutoffs
// cost five. This records, at every node that cuts, which move did it, so
// the ordering can be improved against a number instead of a hunch.
//
// Off by default and read through atomics, since matches search in
// parallel; when off the recording is one branch on a package bool.
var OrderingStats bool

var (
	cutoffAtIndex [8]int64
	cutoffTotal   int64
)

// recordCutoff notes that the move at index i caused a cutoff.
func recordCutoff(i int) {
	if i > len(cutoffAtIndex)-1 {
		i = len(cutoffAtIndex) - 1
	}
	atomic.AddInt64(&cutoffAtIndex[i], 1)
	atomic.AddInt64(&cutoffTotal, 1)
}

// OrderingReport gives the share of cutoffs found with the first move
// searched, the distribution by move index (the last bucket is that index
// and beyond), and the total number of cutoffs seen.
func OrderingReport() (firstMoveRate float64, dist [8]int64, total int64) {
	total = atomic.LoadInt64(&cutoffTotal)
	for i := range cutoffAtIndex {
		dist[i] = atomic.LoadInt64(&cutoffAtIndex[i])
	}
	if total > 0 {
		firstMoveRate = float64(dist[0]) / float64(total)
	}
	return firstMoveRate, dist, total
}

// ResetOrderingStats clears the counters between measurements.
func ResetOrderingStats() {
	for i := range cutoffAtIndex {
		atomic.StoreInt64(&cutoffAtIndex[i], 0)
	}
	atomic.StoreInt64(&cutoffTotal, 0)
}

// testAbortAtNodes, when set by a test, aborts every search once that many
// nodes have been visited. Zero in production.
var testAbortAtNodes int64

// testRootScores, when a test sets it to a map, receives the score of every
// root move that completed in the most recent iteration, so a test can
// check what an aborted iteration was allowed to conclude. Nil in
// production.
var testRootScores map[game.Move]float64

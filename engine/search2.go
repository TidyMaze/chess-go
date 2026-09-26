package engine

import (
	"math"
	"math/bits"
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
	c.fifty = [maxSearchPly]int{}
	c.prevMove = game.Move{}
	c.history = [2][64][64]int32{}
	// 196 KB, and only written when continuation history is on. Clearing it
	// regardless showed up as memclr in the profile. The flag is on the
	// context, not on ev, because reset runs before ev is assigned.
	if c.contDirty {
		c.cont = [2][64][6][64]int32{}
		c.contDirty = false
	}
	c.staticKnown = [maxSearchPly]bool{}
	c.excluded = [maxSearchPly]game.Move{}
	c.path = [maxSearchPly]uint64{}
	c.abortAtNodes = 0
	c.checkMask = 2047
	c.stop = nil
	c.played = nil
	c.lmrTuned = nil
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
	} else if r > 1 {
		r++
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
	ev      *Eval
	killers [maxSearchPly][2]game.Move
	history [2][64][64]int32
	// cont is continuation history: how a quiet move of a piece type to a
	// square did right after the opponent's move to a square. 196 KB,
	// cleared per move like history.
	cont [2][64][6][64]int32
	// staticAt holds the static evaluation noted at each ply of the current
	// line, when one was computed, for the improving test two plies later.
	contDirty bool
	// excluded[ply] is a move the search at that ply must not play. It is
	// how a singular check asks "how good is this position without the
	// move the table likes?" without a second search stack.
	excluded    [maxSearchPly]game.Move
	staticAt    [maxSearchPly]float64
	staticKnown [maxSearchPly]bool
	quiescence  bool
	nodes       int
	extensions  bool
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
	// lmrTuned is the per-search LMR table for c.ev.Tune, built lazily by
	// lmrValue and cached here so it is never rebuilt per node.
	lmrTuned *[64][64]int
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
	// fifty[ply] is the halfmove clock at that ply. The search makes its
	// moves straight on the board and never touched Game.HalfmoveClock,
	// so every node saw the root's value and the fifty move rule was
	// invisible below the root.
	fifty [maxSearchPly]int
	// orderKeys[ply] are the ordering keys of that node's move list, in
	// step with the list as pickMove hands its moves out, each carrying
	// its capture's exchange value.
	orderKeys [maxSearchPly][128]int64
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

func sqIndex(s board.Sq) int { return int(s.Rank)*8 + int(s.File) }

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

// maxHistory is the ceiling a history entry approaches but never passes,
// under the gravity update below.
const maxHistory = 16384

// applyHistory moves an entry toward the ceiling by a bonus, scaled down
// the closer it already is: the standard gravity form. A plain running sum
// grows without limit, so every move that has ever caused a cutoff ends up
// at the same large number and the table stops telling them apart, which
// is why a reduction keyed on history read the same for nearly every quiet
// move here.
func applyHistory(entry *int32, bonus int32) {
	b := bonus
	if b > maxHistory {
		b = maxHistory
	} else if b < -maxHistory {
		b = -maxHistory
	}
	abs := b
	if abs < 0 {
		abs = -abs
	}
	*entry += b - int32(int64(*entry)*int64(abs)/maxHistory)
}

func (c *searchCtx) recordHistory(color board.Color, g *game.Game, m game.Move, depth int) {
	bonus := int32(depth * depth)
	if c.ev != nil && c.ev.HistGravity {
		applyHistory(&c.history[color][sqIndex(m.From)][sqIndex(m.To)], bonus)
		if slot := c.contSlot(color, g, m); slot != nil {
			applyHistory(slot, bonus)
		}
		return
	}
	c.history[color][sqIndex(m.From)][sqIndex(m.To)] += bonus
	if slot := c.contSlot(color, g, m); slot != nil {
		*slot += bonus
	}
}

// contSlot is the continuation-history cell for m as a reply to the
// previous move, or nil when the feature is off or there is no previous
// move (the root, or a null move).
// noteStatic records the static evaluation computed at ply, so the node
// two plies down can ask whether the side to move is improving.
func (c *searchCtx) noteStatic(ply int, static float64) {
	if ply >= 0 && ply < maxSearchPly {
		c.staticAt[ply], c.staticKnown[ply] = static, true
	}
}

// improving says whether the side to move stands better by static
// evaluation than it did two plies ago. Without a static score here or
// there it says no, which is what the pruning rule assumed before.
func (c *searchCtx) improving(ply int, static float64, haveStatic, maximizing bool) bool {
	if c.ev == nil || !c.ev.Improving || !haveStatic || ply < 2 || !c.staticKnown[ply-2] {
		return false
	}
	if maximizing {
		return static > c.staticAt[ply-2]
	}
	return static < c.staticAt[ply-2]
}

// histLMRDivisor turns a history score into plies of reduction. History
// accumulates depth*depth per cutoff and is halved between iterations, so
// the scale is thousands for a move that keeps refuting; the divisor is
// fitted so an ordinary good quiet move moves the reduction by one ply and
// only a persistent refutation moves it by two.
var histLMRDivisor = 16384

// maxHistLMRShift bounds the adjustment either way, so a stale table can
// never turn a reduction into an extension or bury a move entirely.
const maxHistLMRShift = 2

// historyAdjustedReduction shifts a late move reduction by what the
// history tables think of the move: less for a move that keeps causing
// cutoffs, more for one that never does. Bounded by the same rules the
// plain reduction respects, so it stays within [0, depth-2].
func (c *searchCtx) historyAdjustedReduction(reduction int, color board.Color, g *game.Game, m game.Move, depth int) int {
	if c.ev == nil || !c.ev.HistLMR {
		return reduction
	}
	h := int(c.history[color][sqIndex(m.From)][sqIndex(m.To)])
	if slot := c.contSlot(color, g, m); slot != nil {
		h += int(*slot)
	}
	shift := h / histLMRDivisor
	if shift > maxHistLMRShift {
		shift = maxHistLMRShift
	} else if shift < -maxHistLMRShift {
		shift = -maxHistLMRShift
	}
	r := reduction - shift
	if r < 0 {
		r = 0
	}
	if max := depth - 2; r > max {
		r = max
		if r < 0 {
			r = 0
		}
	}
	return r
}

// singularMinDepth is the shallowest node worth a singular test. Strong
// engines use six to eight, inside searches that reach depth 20 and more.
// This engine reaches 6 to 13, and instrumenting the gates showed why that
// matters: at a threshold of 7, only 31 of 8619 live nodes were deep
// enough and the test never once fired. At 4 it fires, with a two-ply
// exclusion search behind it, which is as much depth as there is to spend.
const singularMinDepth = 4

// singularMargin is how far below the stored score the exclusion window
// sits, in pawns, scaled by depth. A move is singular when nothing else
// comes within this of it.
func singularMargin(depth int) float64 { return 0.02 * float64(depth) }

// singularExtension reports the extra plies the table's move has earned.
//
// The question is whether this move is the only one that holds the
// position. It is asked by searching the same node with that move barred,
// to a reduced depth, against a null window a margin below what the table
// says the move is worth. If every alternative fails below that window,
// nothing else comes close and the move is worth one more ply.
//
// Requires a table entry deep enough to be worth trusting and a lower
// bound or exact score: an upper bound says only that the move was not
// good enough, which is not an opinion about the alternatives. Never
// recurses, since the exclusion search has a move barred at this ply and
// the guard refuses to start another.
func (c *searchCtx) singularExtension(g *game.Game, tt *TranspositionTable, key uint64, m game.Move, color board.Color, depth, ply int, maximizingFor board.Color) int {
	if c.ev == nil || !c.ev.Singular || tt == nil {
		return 0
	}
	if depth < singularMinDepth || ply == 0 || ply >= maxSearchPly-2 {
		return 0
	}
	if c.excludedAt(ply) != (game.Move{}) {
		return 0
	}
	score, ttDepth, flag, ok := tt.entryFor(key)
	if !ok || ttDepth < depth-3 || (flag != ttLowerBound && flag != ttExact) {
		return 0
	}
	// The raw entry is a distance from the node that stored it, not from
	// the root, but a mate is refused either way and anything else is not
	// adjusted, so it needs no conversion here.
	if score >= mateBound || score <= -mateBound {
		return 0
	}
	target := score - singularMargin(depth)
	c.excluded[ply] = m
	v := c.search(g, color, maximizingFor, depth/2, ply, target-1e-6, target)
	c.excluded[ply] = game.Move{}
	if c.aborted {
		return 0
	}
	if v < target {
		return 1
	}
	return 0
}

// excludedAt is the move barred at this ply, if any.
func (c *searchCtx) excludedAt(ply int) game.Move {
	if ply < 0 || ply >= maxSearchPly {
		return game.Move{}
	}
	return c.excluded[ply]
}

func (c *searchCtx) contSlot(color board.Color, g *game.Game, m game.Move) *int32 {
	if c.ev == nil || !c.ev.ContHist || c.prevMove == (game.Move{}) {
		return nil
	}
	p, ok := g.Board.PieceAt(m.From)
	if !ok {
		return nil
	}
	c.contDirty = true
	return &c.cont[color][sqIndex(c.prevMove.To)][p.Type][sqIndex(m.To)]
}

func (c *searchCtx) penalizeHistory(color board.Color, g *game.Game, failed []game.Move, depth int) {
	malus := int32(depth * depth)
	gravity := c.ev != nil && c.ev.HistGravity
	for _, m := range failed {
		if !isCaptureMove(g, m) {
			idxFrom, idxTo := sqIndex(m.From), sqIndex(m.To)
			if gravity {
				applyHistory(&c.history[color][idxFrom][idxTo], -malus)
				if slot := c.contSlot(color, g, m); slot != nil {
					applyHistory(slot, -malus)
				}
				continue
			}
			c.history[color][idxFrom][idxTo] -= malus
			if c.history[color][idxFrom][idxTo] < -1<<16 {
				c.history[color][idxFrom][idxTo] = -1 << 16
			}
			if slot := c.contSlot(color, g, m); slot != nil && *slot > -1<<16 {
				*slot -= malus
			}
		}
	}
}

func (c *searchCtx) ageHistory() {
	for co := 0; co < 2; co++ {
		for f := 0; f < 64; f++ {
			for t := 0; t < 64; t++ {
				c.history[co][f][t] /= 2
			}
		}
	}
}

// scoreMove ranks a move for ordering: transposition-table move first,
// then captures by MVV-LVA, then killers, then history.
func (c *searchCtx) scoreMove(g *game.Game, m game.Move, ttMove game.Move, ply int, color board.Color) (int, int16) {
	if m == ttMove {
		return 1 << 30, 0
	}
	victim, isDirectCapture := g.Board.PieceAt(m.To)
	attacker, hasAttacker := g.Board.PieceAt(m.From)
	isEP := !isDirectCapture && hasAttacker && attacker.Type == board.Pawn && m.From.File != m.To.File
	if isEP {
		if ep, has := g.Board.EPSquare(); has && ep == m.To {
			victim = board.Piece{Type: board.Pawn}
			isDirectCapture = true
		}
	}
	if isDirectCapture {
		if c.ev != nil && c.ev.MainSEE {
			// Winning captures first by what they win, losing captures
			// after every quiet move.
			if mvvLvaPiece[victim.Type] >= mvvLvaPiece[attacker.Type] {
				return 1<<20 + (mvvLvaPiece[victim.Type]-mvvLvaPiece[attacker.Type])*100 + mvvLvaPiece[victim.Type], 0
			}
			// An exchange never wins more than the victim, so this is the
			// most exchangeScore can give; pickMove asks for the real one
			// only if the capture ever comes up best.
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
	// contSlot's entry, read on the piece already looked up.
	if hasAttacker && c.ev != nil && c.ev.ContHist && c.prevMove != (game.Move{}) {
		score += int(c.cont[color][sqIndex(c.prevMove.To)][attacker.Type][sqIndex(m.To)])
	}
	return score, 0
}

// orderKey packs a move's ordering score, its index in the generated list
// and its exchange value into one integer. No two keys of a list are equal,
// and the larger key is the higher score or, on equal scores, the earlier
// move: the order a stable descending sort gives, whatever algorithm puts
// the keys in order. Room for any sum of two int32 history entries, lists
// under 4096 moves, and exchanges within a byte (they stay within a king's
// value, 20, either way).
func orderKey(score, idx int, sv int16) int64 {
	return int64(score)<<20 | int64(0xFFF-idx)<<8 | int64(uint8(sv))
}

// keySEE is the exchange value orderKey packed.
func keySEE(k int64) int16 { return int16(int8(uint8(k))) }

// keyIndex is the generated-list index orderKey packed.
func keyIndex(k int64) int { return 0xFFF - int(k>>8&0xFFF) }

// seeUnknown is the exchange value of a key whose exchange is not yet
// evaluated. Its score is the most the capture could be worth, and
// pickMove evaluates it only once it is the best candidate left.
const seeUnknown = math.MinInt8

// pickMove brings the best of ms[j:] to position j, with its key.
//
// Picking on demand instead of sorting the whole list: a node that cuts
// off on its first moves never pays to order the rest, and the swap is
// safe because the keys carry the tie-break. A pending exchange that comes
// out best is evaluated and the pick run again: every other key is a
// score or a bound above one, so the best settled key is the true best.
func pickMove(b *board.Board, ms []game.Move, keys []int64, j int) {
	best := bestKey(keys, j)
	for keySEE(keys[best]) == seeUnknown {
		sc, sv := exchangeScore(b, ms[best])
		keys[best] = orderKey(sc, keyIndex(keys[best]), sv)
		best = bestKey(keys, j)
	}
	ms[j], ms[best] = ms[best], ms[j]
	keys[j], keys[best] = keys[best], keys[j]
}

// bestKey is the index of the largest of keys[j:].
func bestKey(keys []int64, j int) int {
	best, top := j, keys[j]
	for k, v := range keys[j+1:] {
		if v > top {
			best, top = j+1+k, v
		}
	}
	return best
}

// exchangeScore places a capture of a cheaper piece by its exchange
// evaluation: winning captures first by what they win, losing captures
// after every quiet move.
func exchangeScore(b *board.Board, m game.Move) (int, int16) {
	victim, _ := b.PieceAt(m.To)
	x := see(b, m)
	if x < 0 {
		return -1<<20 + x*100, int16(x)
	}
	return 1<<20 + x*100 + mvvLvaPiece[victim.Type], int16(x)
}

// scoreMoves keys every move of ms for pickMove, into keys. The scores
// read the history tables, which the children's searches change, so they
// are all taken before the first move is searched, as for a sort.
func (c *searchCtx) scoreMoves(g *game.Game, ms []game.Move, keys []int64, ttMove game.Move, ply int, color board.Color) {
	for i, m := range ms {
		sc, sv := c.scoreMove(g, m, ttMove, ply, color)
		keys[i] = orderKey(sc, i, sv)
	}
}

// orderMoves sorts ms by descending score, ties in generation order.
func (c *searchCtx) orderMoves(g *game.Game, ms []game.Move, ttMove game.Move, ply int, color board.Color) {
	var keys []int64
	if ply < maxSearchPly && len(ms) <= len(c.orderKeys[ply]) {
		keys = c.orderKeys[ply][:len(ms)]
	} else {
		keys = make([]int64, len(ms))
	}
	c.scoreMoves(g, ms, keys, ttMove, ply, color)
	for j := range ms {
		pickMove(&g.Board, ms, keys, j)
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
	// A hard ceiling on depth of recursion. Extensions keep the depth from
	// decreasing, so a long enough forcing line walks past the end of every
	// per-ply array. Each write was guarded individually and one read of
	// c.fifty was not, which is a crash waiting for a feature that extends
	// often enough to reach it. There is nothing worth searching at ply 64,
	// so this is a leaf like any other.
	if ply >= maxSearchPly {
		return evalPositionFor(g, color, maximizingFor, c.ev)
	}
	// Publish this node's halfmove clock so the evaluation can fade a
	// score toward the draw as the fifty move rule closes in. Without it
	// every node evaluated at the root's clock and a shuffle looked
	// exactly as good as a pawn push.
	if c.ev != nil && ply < maxSearchPly {
		c.ev.FiftyClock, c.ev.fiftyKnown = c.fifty[ply], true
	}
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
	var key uint64
	if ply < maxSearchPly && c.path[ply] != 0 {
		key = c.path[ply]
	} else {
		key = zobristBoard(&g.Board, color)
		if ply < maxSearchPly {
			c.path[ply] = key
		}
	}
	// A hundred halfmoves without a capture or a pawn move is a draw, here
	// and not only at the horizon. fadeForFiftyMove returned 0 at a leaf,
	// but an interior node past the limit went on searching, and a capture
	// or pawn push found below it brought the won score back. Lichess
	// lhT8MqvU was drawn that way a rook and a bishop up: at clock 99 the
	// engine saw a mate in six behind a quiet move and played it. Before the
	// table probe, whose key knows nothing of the clock. The one exception
	// is FIDE 9.3: if the move that reached 100 gave checkmate, the mate
	// stands.
	if ply > 0 && c.fifty[ply] >= 100 {
		if moves.IsInCheck(&g.Board, color) && !g.HasAnyLegalMoveInCheck(color, true) {
			return terminalScore(g, color, maximizingFor, ply)
		}
		return 0
	}
	if ply > 0 && depth > 0 && !(c.ev != nil && c.ev.NoRepetition) && c.isRepetition(key, ply) {
		return 0
	}
	if deadPosition(&g.Board) {
		return 0
	}
	if tt != nil && depth > 0 {
		if score, cutoff, m, okMove := tt.probeWithMove(key, depth, ply, c.fifty[ply], maximizingFor, alpha, beta); cutoff {
			return score
		} else if okMove {
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
	if depth <= 0 {
		if c.quiescence {
			return quiesceWithKey(g, key, color, maximizingFor, alpha, beta, c.ev, 0, ply, c.fifty[ply])
		}
		var moveBuf [96]game.Move
		legal, _ := g.AppendLegalMovesInCheck(moveBuf[:0], color)
		if len(legal) == 0 {
			return terminalScore(g, color, maximizingFor, ply)
		}
		return evalPositionFor(g, color, maximizingFor, c.ev)
	}

	inCheck := moves.IsInCheck(&g.Board, color)

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
	t := c.ev.tune()
	staticEval, haveStatic := 0.0, false
	futile := c.ev != nil && c.ev.Futility && !inCheck && depth <= 3 &&
		alpha > negInf && beta < posInf &&
		alpha > -mateBound && beta < mateBound
	if futile {
		staticEval, haveStatic = evalPositionFor(g, color, maximizingFor, c.ev), true
		margin := t.marginFutility(depth)
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
		if t.cutsRFP(depth, staticEval, alpha, beta, maximizing, inCheck) {
			return staticEval
		}
	}
	// Razoring: depths 1 to 3, a static evaluation standing far below the
	// window. Quiescence decides, not the static score, because the
	// captures it sees are exactly what a big deficit tends to hide.
	if c.ev != nil && c.ev.Razoring && c.quiescence && depth <= 3 && !inCheck &&
		alpha > negInf && beta < posInf && alpha > -mateBound && beta < mateBound {
		if !haveStatic {
			staticEval, haveStatic = evalPositionFor(g, color, maximizingFor, c.ev), true
		}
		if t.cutsRazor(depth, staticEval, alpha, beta, maximizing) {
			q := quiesceWithKey(g, key, color, maximizingFor, alpha, beta, c.ev, 0, ply, c.fifty[ply])
			if maximizing && q <= alpha {
				return q
			}
			if !maximizing && q >= beta {
				return q
			}
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
	if nullOK && c.ev.NullPieces && !nullAllowedByMaterial(&g.Board, color) {
		nullOK = false
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
			r = t.reductionNull(depth, over)
		}
		ep, hadEP := g.Board.EPSquare()
		if !c.ev.KeepNullMoveEP {
			g.Board.SetEPSquare(board.Sq{}, false)
		}
		if ply < maxSearchPly {
			c.moveStack[ply] = game.Move{}
		}
		if ply+1 < maxSearchPly {
			nullKey := key ^ zobristBlackToMove
			if hadEP && !c.ev.KeepNullMoveEP {
				nullKey ^= zobristEP[ep.File]
			}
			c.path[ply+1] = nullKey
			tt.prefetch(nullKey)
			// No move is played, so the clock stands where it was. Unset,
			// the child read whatever a sibling left here, and a stale 100
			// made passing an instant draw.
			c.fifty[ply+1] = c.fifty[ply]
		}
		score := c.searchNull(g, color.Other(), maximizingFor, depth-r, ply+1, alpha, beta, true)
		if ply+1 < maxSearchPly {
			c.path[ply+1] = 0
		}
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
	prevMove := c.prevMove
	// The pruning paths above may have priced the position; if so, keep the
	// number for the node two plies down and ask it about this one.
	if haveStatic {
		c.noteStatic(ply, staticEval)
	} else if ply < maxSearchPly {
		c.staticKnown[ply] = false
	}
	improvingHere := c.improving(ply, staticEval, haveStatic, maximizing)

	best := negInf
	if !maximizing {
		best = posInf
	}
	var bestMove game.Move
	ttSearched := false

	// Staged move search: try the transposition table move first.
	// In the vast majority of positions with a TT hit, the TT move refutes
	// the position immediately (beta cutoff). Searching it before generating
	// and sorting the full legal move list saves ~80% of move generation overhead.
	if ttMove != (game.Move{}) && ttMove != c.excludedAt(ply) && g.IsLegalMoveFor(ttMove, color) {
		m := ttMove
		ttExtension := c.singularExtension(g, tt, key, m, color, depth, ply, maximizingFor)
		isCapture := isCaptureMove(g, m)
		exchange := 0
		if isCapture && c.ev != nil && c.ev.MainSEE && depth <= 6 {
			attacker, _ := g.Board.PieceAt(m.From)
			victim, onSquare := g.Board.PieceAt(m.To)
			if !onSquare {
				victim = board.Piece{Type: board.Pawn}
			}
			if mvvLvaPiece[victim.Type] < mvvLvaPiece[attacker.Type] {
				exchange = see(&g.Board, m)
			}
		}

		childClock := childFiftyClock(g, m, c.fifty[ply])
		undo, promoted := makeSearchMove(g, m)
		if ply < maxSearchPly {
			c.moveStack[ply] = m
		}
		lmp := c.ev != nil && c.ev.LMP && zeroWindow(alpha, beta)
		givesCheck := false
		if c.extensions || futile || lmp || (isCapture && c.ev != nil && c.ev.MainSEE && exchange < -depth) {
			givesCheck = moves.IsInCheck(&g.Board, color.Other())
		}
		extension := 0
		if c.extensions && ply < maxSearchPly-2 && givesCheck {
			extension = 1
		}
		if ply+1 < maxSearchPly {
			c.path[ply+1] = zobristUpdate(key, &g.Board, m, undo, promoted)
			tt.prefetch(c.path[ply+1])
			c.fifty[ply+1] = childClock
		}

		if ttExtension > extension {
			extension = ttExtension
		}
		value := c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
		g.Board.UnmakeMove(undo)
		// Every node below set c.prevMove to its own previous move. The
		// cutoff bookkeeping and the ordering of the rest of this node's
		// moves read it as this node's, so put it back.
		c.prevMove = prevMove
		if ply+1 < maxSearchPly {
			c.path[ply+1] = 0
		}
		if c.aborted {
			return 0
		}

		ttSearched = true
		best, bestMove = value, m
		if maximizing {
			if best > alpha {
				alpha = best
			}
		} else {
			if best < beta {
				beta = best
			}
		}

		if beta <= alpha {
			if OrderingStats {
				recordCutoff(0)
			}
			if !isCapture {
				c.recordKiller(ply, m)
				c.recordHistory(color, g, m, depth)
				if ply > 0 {
					c.recordCounter(color, c.moveStack[ply-1], m)
				}
			}
			if tt != nil && depth > 0 {
				flag := ttExact
				if best <= origAlpha {
					flag = ttUpperBound
				} else if best >= origBeta {
					flag = ttLowerBound
				}
				tt.storeWithMove(key, best, depth, ply, flag, maximizingFor, bestMove)
			}
			return best
		}
	}

	// Pseudo-legal moves, each proved legal only when its turn comes: a
	// node that cuts off on its first moves never tests the rest. The list
	// is ordered with its illegal moves in it, and the ordering is a stable
	// sort on a score that does not depend on the list, so the legal moves
	// come out in the same order as if they had been filtered first.
	//
	// Except in check, where most pseudo-legal moves are illegal and most
	// of those are rejected without being played: scoring and sorting the
	// whole list to search the few legal moves cost more than filtering it
	// up front, after which the zero Legality tests nothing.
	var moveBuf [96]game.Move
	var list []game.Move
	var legality game.Legality
	if inCheck {
		list = g.AppendLegalMovesGivenCheck(moveBuf[:0], color, true)
	} else {
		list, legality = g.AppendPseudoLegalMoves(moveBuf[:0], color, false)
	}
	if !ttSearched {
		// The first legal move in generation order backs the table entry
		// when every move is pruned, and its absence is mate or stalemate.
		first := firstLegal(g, list, legality)
		if first < 0 {
			return terminalScore(g, color, maximizingFor, ply)
		}
		bestMove = list[first]
		list = list[first:]
	}

	// Ordered on demand, one pick per move searched, when the keys fit the
	// per-ply buffer; sorted up front otherwise.
	picking := ply < maxSearchPly && len(list) <= len(c.orderKeys[ply])
	var keys []int64
	if picking {
		keys = c.orderKeys[ply][:len(list)]
		c.scoreMoves(g, list, keys, ttMove, ply, color)
	} else {
		c.orderMoves(g, list, ttMove, ply, color)
	}

	// legal compacts the legal moves over the front of list as they are
	// met, so legal[:i] is the history malus's list of moves tried before
	// the i-th. i counts legal moves only: the reductions, pruning and
	// history read it as "how late in the ordering is this move". It only
	// writes at or below j, so the moves still to pick are untouched.
	legal := list[:0]
	for j := 0; j < len(list); j++ {
		if picking {
			pickMove(&g.Board, list, keys, j)
		}
		m := list[j]
		if m == c.excludedAt(ply) {
			if legality.IsLegal(&g.Board, m) {
				legal = append(legal, m)
			}
			continue
		}
		// The table move was proved legal by IsLegalMoveFor before it
		// was searched.
		if ttSearched && m == ttMove {
			legal = append(legal, m)
			continue
		}
		isCapture := isCaptureMove(g, m)
		exchange := 0
		if isCapture && c.ev != nil && c.ev.MainSEE && depth <= 6 {
			attacker, _ := g.Board.PieceAt(m.From)
			victim, onSquare := g.Board.PieceAt(m.To)
			if !onSquare {
				victim = board.Piece{Type: board.Pawn}
			}
			if mvvLvaPiece[victim.Type] < mvvLvaPiece[attacker.Type] {
				if picking && m != ttMove {
					exchange = int(keySEE(keys[j]))
				} else {
					exchange = see(&g.Board, m)
				}
			}
		}

		// Make/unmake rather than copying the board into a child Game:
		// this is the hot path, and the copy was the largest per-node cost
		// left. Promotion is handled here because the board layer does not
		// know the rule.
		childClock := childFiftyClock(g, m, c.fifty[ply])
		isAdvPawn := c.ev != nil && c.ev.PawnPush && advancedPawnPush(&g.Board, m, color)
		needsTest := legality.NeedsTest(&g.Board, m)
		undo, promoted := makeSearchMove(g, m)
		// The legality test rides on the make the search needs anyway. The
		// promoted piece is the mover's own, so it cannot change whether
		// the mover is in check.
		if needsTest && moves.IsInCheck(&g.Board, color) {
			g.Board.UnmakeMove(undo)
			continue
		}
		i := len(legal)
		legal = append(legal, m)
		if ply < maxSearchPly {
			c.moveStack[ply] = m
		}
		// Recurse on the same Game: search reads the board through g and
		// takes the side to move as a parameter, so there is no need to
		// build a child object at all.

		lmp := c.ev != nil && c.ev.LMP && zeroWindow(alpha, beta)
		givesCheck := false
		if c.extensions || futile || lmp || (isCapture && c.ev != nil && c.ev.MainSEE && exchange < -depth) {
			givesCheck = moves.IsInCheck(&g.Board, color.Other())
		}

		// Losing captures at shallow depth on a zero window are not worth
		// their subtree either.
		if isCapture && c.ev != nil && c.ev.MainSEE && i > 0 && seePrunes(depth, exchange, inCheck, givesCheck, zeroWindow(alpha, beta)) {
			g.Board.UnmakeMove(undo)
			continue
		}

		maxLMP := 5
		if c.ev != nil && c.ev.DeepLMP {
			maxLMP = 8
		}
		if lmp && i > 0 && !isAdvPawn && t.prunedLMP(depth, i, improvingHere, inCheck, isCapture, promoted, givesCheck, c.isKiller(ply, m), maxLMP) {
			g.Board.UnmakeMove(undo)
			continue
		}

		// Forward futility: a quiet, non-checking, non-promoting move this
		// far short of the bound is not going to reach it, so skip its
		// whole subtree. The first move is always searched so that `best`
		// is backed by a real score.
		if futile && i > 0 && !isCapture && !givesCheck && !promoted && !isAdvPawn {
			margin := t.marginFutility(depth)
			if (maximizing && staticEval+margin <= alpha) ||
				(!maximizing && staticEval-margin >= beta) {
				g.Board.UnmakeMove(undo)
				continue
			}
		}

		// Late move reductions: the ordering above says moves after the
		// first few are unlikely to be best, so look at them shallower.
		reduction := 0
		if depth >= 3 && i >= 3 && !isCapture && !inCheck && !isAdvPawn && !(c.ev != nil && c.ev.NoLMR) {
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
				reduction = c.lmrReduction(depth, i, !zeroWindow(alpha, beta), c.isKiller(ply, m), promoted)
			}
			reduction = c.historyAdjustedReduction(reduction, color, g, m, depth)
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

		if ply+1 < maxSearchPly {
			c.path[ply+1] = zobristUpdate(key, &g.Board, m, undo, promoted)
			tt.prefetch(c.path[ply+1])
			c.fifty[ply+1] = childClock
		}

		var value float64
		if i == 0 && !ttSearched {
			value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
		} else {
			// Principal variation search: try a zero-width window first.
			if maximizing {
				value = c.search(g, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, alpha, alpha+1e-6)
				twoStep := c.ev != nil && c.ev.LMRTwoStep
				if value > alpha && reduction > 0 && twoStep {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, alpha+1e-6)
				}
				if value > alpha && (!twoStep || value < beta) {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			} else {
				value = c.search(g, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, beta-1e-6, beta)
				twoStep := c.ev != nil && c.ev.LMRTwoStep
				if value < beta && reduction > 0 && twoStep {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, beta-1e-6, beta)
				}
				if value < beta && (!twoStep || value > alpha) {
					value = c.search(g, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			}
		}

		g.Board.UnmakeMove(undo)
		if ply+1 < maxSearchPly {
			c.path[ply+1] = 0
		}
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
				c.recordHistory(color, g, m, depth)
				if c.ev != nil && c.ev.HistoryMalus && i > 0 {
					c.penalizeHistory(color, g, legal[:i], depth)
				}
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
		tt.storeWithMove(key, best, depth, ply, flag, maximizingFor, bestMove)
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
	ctx.fifty[0] = g.HalfmoveClock
	completed := 0
	if main {
		atomic.StoreInt32(&lastCutOffMove, 0)
	}
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
			// Stay ahead of the main thread: staggered one to three plies
			// past what it has completed.
			if lead := int(atomic.LoadInt32(&shared.mainDepth)) + 1 + int(seed%3); lead > depth {
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
		delta := ev.tune().aspirationDelta()
		if ev.Aspiration && depth >= 3 && depth > startDepth {
			alpha, beta = prevScore-delta, prevScore+delta
		}

	researchFullWindow:
		if main && testRootScores != nil {
			clear(testRootScores)
		}
		bestScore := negInf
		var iterBest game.Move
		var tied []game.Move
		standingScore := negInf
		standingExact := false

		ordered := make([]game.Move, len(legal))
		copy(ordered, legal)
		ctx.orderMoves(g, ordered, best, 0, color)
		if rng != nil && len(ordered) > 2 {
			// Helpers keep the best-known move first and shuffle the rest.
			rest := ordered[1:]
			rng.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
		}

		for i, m := range ordered {
			ctx.fifty[1] = childFiftyClock(g, m, ctx.fifty[0])
			undo, promoted := makeSearchMove(g, m)
			ctx.path[1] = zobristUpdate(ctx.path[0], &g.Board, m, undo, promoted)
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
			ctx.path[1] = 0
			g.Board.UnmakeMove(undo)
			if ctx.aborted {
				// A child cut off mid-search returned nothing usable.
				break
			}
			if i == 0 {
				standingScore = score
				if score > alpha && score < beta && !testForceStandingFailLow {
					standingExact = true
				}
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
			if iterBest != (game.Move{}) && iterBest != ordered[0] && standingExact && bestScore > standingScore+0.01 && bestScore < beta {
				best = iterBest
				if main {
					atomic.StoreInt32(&lastCutOffMove, 1)
				}
			}
			break
		}
		// The true score fell outside the aspiration window, so the search
		// result is only a bound: redo this depth with a widened window.
		if (bestScore <= alpha || bestScore >= beta) && (alpha != negInf || beta != posInf) {
			delta *= 2
			if delta > 3.0 {
				alpha, beta = negInf, posInf
			} else {
				if bestScore <= alpha {
					alpha = prevScore - delta
				}
				if bestScore >= beta {
					beta = prevScore + delta
				}
			}
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
		if ctx.ev != nil && ctx.ev.HistoryAging {
			ctx.ageHistory()
		}
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
// childFiftyClock is the halfmove clock after m, given the clock before
// it. A capture or a pawn move resets it, which is what "making progress"
// means under the fifty move rule, and is exactly the distinction the
// search could not see. Must be called before the move is made.
func childFiftyClock(g *game.Game, m game.Move, parent int) int {
	if _, captured := g.Board.PieceAt(m.To); captured {
		return 0
	}
	if p, ok := g.Board.PieceAt(m.From); ok && p.Type == board.Pawn {
		return 0
	}
	return parent + 1
}

// firstLegal is the index of the first legal move of a pseudo-legal list,
// or -1 when there is none. Usually the first move, and usually without a
// test, since only the king, pinned pieces and en passant need one.
func firstLegal(g *game.Game, list []game.Move, legality game.Legality) int {
	for i, m := range list {
		if legality.IsLegal(&g.Board, m) {
			return i
		}
	}
	return -1
}

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
			shade := (int(p.Sq.File) + int(p.Sq.Rank)) & 1
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
	return lateMovePrunedMax(depth, moveIndex, improving, inCheck, isCapture, promoted, givesCheck, isKiller, 5)
}

func lateMovePrunedMax(depth, moveIndex int, improving, inCheck, isCapture, promoted, givesCheck, isKiller bool, maxDepth int) bool {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if depth > maxDepth || inCheck || isCapture || promoted || givesCheck || isKiller {
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
	if depth < 4 || depth > 7 || inCheck || !zeroWindow(alpha, beta) || alpha <= -mateBound || beta >= mateBound {
		return false
	}
	margin := reverseFutilityMargin(depth)
	if maximizing {
		return staticEval-margin >= beta
	}
	return staticEval+margin <= alpha
}

// nullAllowedByMaterial reports whether the side to move has the two non-pawn
// pieces that make passing a safe lower bound; with fewer, zugzwang is common.
func nullAllowedByMaterial(b *board.Board, color board.Color) bool {
	n := 0
	for t := board.Knight; t <= board.Queen; t++ {
		n += bits.OnesCount64(b.PieceBitboard(color, t))
	}
	return n >= 2
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

// lastCutOffMove is 1 when the main thread's most recent search played a move
// taken from the iteration the clock cut off, 0 when it came from a completed one.
var lastCutOffMove int32

// LastMoveFromCutOffIteration reports whether the latest search's move came
// from a cut-off iteration rather than from its deepest completed one.
func LastMoveFromCutOffIteration() bool { return atomic.LoadInt32(&lastCutOffMove) == 1 }

// testAbortAtNodes, when set by a test, aborts every search once that many
// nodes have been visited. Zero in production.
var testAbortAtNodes int64

// testForceStandingFailLow, when set by a test, forces standingExact to false
// to test that an aborted iteration rejects switching when standing move failed low.
var testForceStandingFailLow bool

// testRootScores, when a test sets it to a map, receives the score of every
// root move that completed in the most recent iteration, so a test can
// check what an aborted iteration was allowed to conclude. Nil in
// production.
var testRootScores map[game.Move]float64

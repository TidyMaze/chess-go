package engine

import (
	"math"
	"sync"

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

// LastSearchNodes is the node count of the most recent search, for
// reporting nodes per second. Not safe to read from concurrent searches;
// intended for single-threaded benchmarking.
var LastSearchNodes int

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
	if victim, isCapture := g.Board.PieceAt(m.To); isCapture {
		attacker, _ := g.Board.PieceAt(m.From)
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
	return int(c.history[color][sqIndex(m.From)][sqIndex(m.To)])
}

func (c *searchCtx) orderMoves(g *game.Game, ms []game.Move, ttMove game.Move, ply int, color board.Color) {
	// Insertion sort by descending score: move lists are short (tens of
	// entries), so this beats a general sort with its allocation and
	// comparator indirection.
	scores := make([]int, len(ms))
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
	c.nodes++
	tt := c.ev.table()

	var ttMove game.Move
	key := zobristHash(g)
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
	if tt != nil && depth > 0 {
		if score, ok := tt.probe(key, depth, maximizingFor, alpha, beta); ok {
			return score
		}
		if e := &tt.entries[key&tt.mask]; e.key32 == keyUpper(key) {
			ttMove = game.Move{From: indexToSq(e.from), To: indexToSq(e.to)}
		}
	}
	origAlpha, origBeta := alpha, beta

	var moveBuf [64]game.Move
	legal, inCheck := g.AppendLegalMovesInCheck(moveBuf[:0], color)
	if len(legal) == 0 {
		return terminalScore(g, color, maximizingFor, depth)
	}
	if depth <= 0 {
		if c.quiescence {
			return quiesce(g, color, maximizingFor, alpha, beta, c.ev, 0)
		}
		return PositionScoreEval(&g.Board, maximizingFor, c.ev)
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
	staticEval := 0.0
	futile := c.ev != nil && c.ev.Futility && !inCheck && depth <= 3 &&
		alpha > negInf && beta < posInf &&
		alpha > -mateBound && beta < mateBound
	if futile {
		staticEval = PositionScoreEval(&g.Board, maximizingFor, c.ev)
		margin := futilityMargin[depth]
		if maximizing && staticEval-margin >= beta {
			return staticEval - margin
		}
		if !maximizing && staticEval+margin <= alpha {
			return staticEval + margin
		}
	}

	if c.ev.useNullMove() && depth >= 3 && !inCheck {
		score := c.search(g, color.Other(), maximizingFor, depth-3, ply+1, alpha, beta)
		if maximizing && score >= beta {
			return score
		}
		if !maximizing && score <= alpha {
			return score
		}
	}

	c.orderMoves(g, legal, ttMove, ply, color)

	best := negInf
	if !maximizing {
		best = posInf
	}
	bestMove := legal[0]

	for i, m := range legal {
		_, isCapture := g.Board.PieceAt(m.To)

		// Make/unmake rather than copying the board into a child Game:
		// this is the hot path, and the copy was the largest per-node cost
		// left. Promotion is handled here because the board layer does not
		// know the rule.
		undo := g.Board.MakeMove(m.From, m.To)
		promoted := false
		if p, ok := g.Board.PieceAt(m.To); ok && p.Type == board.Pawn {
			if (p.Color == board.White && m.To.Rank == 7) || (p.Color == board.Black && m.To.Rank == 0) {
				g.Board.SetPiece(m.To, board.Piece{Color: p.Color, Type: board.Queen})
				promoted = true
			}
		}
		// Recurse on the same Game: search reads the board through g and
		// takes the side to move as a parameter, so there is no need to
		// build a child object at all.

		givesCheck := false
		if c.extensions || futile {
			givesCheck = moves.IsInCheck(&g.Board, color.Other())
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
				r := lmrTable(depth, i)
				if r > reduction {
					reduction = r
				}
				// Never reduce into quiescence: leave at least one ply.
				if reduction > depth-2 {
					reduction = depth - 2
				}
				if reduction < 1 {
					reduction = 1
				}
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
			if !isCapture {
				c.recordKiller(ply, m)
				c.recordHistory(color, m, depth)
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
		return game.Move{}, false
	}
	if ev == nil {
		ev = &Eval{}
	}
	if ev.Table == nil {
		ev.Table = NewTranspositionTable(20)
	}
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.ev, ctx.quiescence, ctx.extensions = ev, useQuiescence, ev.Extensions
	ctx.nodes = 0
	ctx.played = playedKeys(g)
	ctx.path[0] = zobristHash(g)
	defer func() { LastSearchNodes = ctx.nodes }()

	best := legal[0]
	prevScore := 0.0
	for depth := 1; depth <= maxDepth; depth++ {
		// Aspiration window: the score at depth N is usually close to the
		// score at N-1, so search a narrow window around it. Most searches
		// then run with far tighter bounds and prune much harder; the
		// occasional miss costs one re-search with a full window.
		alpha, beta := negInf, posInf
		if ev.Aspiration && depth >= 3 {
			const window = 0.5
			alpha, beta = prevScore-window, prevScore+window
		}

	researchFullWindow:
		bestScore := negInf
		var iterBest game.Move
		var tied []game.Move

		ordered := make([]game.Move, len(legal))
		copy(ordered, legal)
		ctx.orderMoves(g, ordered, best, 0, color)

		for _, m := range ordered {
			undo := g.Board.MakeMove(m.From, m.To)
			if p, ok := g.Board.PieceAt(m.To); ok && p.Type == board.Pawn {
				if (p.Color == board.White && m.To.Rank == 7) || (p.Color == board.Black && m.To.Rank == 0) {
					g.Board.SetPiece(m.To, board.Piece{Color: p.Color, Type: board.Queen})
				}
			}
			score := ctx.search(g, color.Other(), color, depth-1, 1, alpha, beta)
			g.Board.UnmakeMove(undo)
			if score > bestScore {
				bestScore, iterBest = score, m
				tied = tied[:0]
				tied = append(tied, m)
			} else if score == bestScore {
				tied = append(tied, m)
			}
		}
		// The true score fell outside the aspiration window, so the search
		// result is only a bound: redo this depth with a full window.
		if (bestScore <= alpha || bestScore >= beta) && (alpha != negInf || beta != posInf) {
			alpha, beta = negInf, posInf
			goto researchFullWindow
		}
		prevScore = bestScore

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
	return best, true
}

// TotalNodes reports nodes visited by the most recent search, including
// quiescence nodes.
func TotalNodes() int { return LastSearchNodes + quiesceNodes }

// ResetNodes clears the counters between measurements.
func ResetNodes() { LastSearchNodes, quiesceNodes = 0, 0 }

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

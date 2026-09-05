package engine

import (
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

type searchCtx struct {
	ev         *Eval
	killers    [maxSearchPly][2]game.Move
	history    [2][64][64]int
	quiescence bool
	nodes      int
	extensions bool
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
	c.history[color][sqIndex(m.From)][sqIndex(m.To)] += depth * depth
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
	return c.history[color][sqIndex(m.From)][sqIndex(m.To)]
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

	var key uint64
	var ttMove game.Move
	if tt != nil && depth > 0 {
		key = zobristHash(g)
		if score, ok := tt.probe(key, depth, maximizingFor, alpha, beta); ok {
			return score
		}
		if e := &tt.entries[key&tt.mask]; e.key == key {
			ttMove = e.best
		}
	}
	origAlpha, origBeta := alpha, beta

	var moveBuf [64]game.Move
	legal := g.AppendLegalMoves(moveBuf[:0], color)
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
	inCheck := moves.IsInCheck(&g.Board, color)

	if c.ev.useNullMove() && depth >= 3 && !inCheck {
		passed := game.Game{Board: g.Board, Turn: color.Other()}
		score := c.search(&passed, color.Other(), maximizingFor, depth-3, ply+1, alpha, beta)
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
		next := game.Game{Board: g.Board, Turn: color}
		next.ApplyMove(m.From, m.To)

		// Late move reductions: the ordering above says moves after the
		// first few are unlikely to be best, so look at them shallower.
		reduction := 0
		if depth >= 3 && i >= 3 && !isCapture && !inCheck {
			reduction = 1
		}

		// Check extension: a forced sequence should not be cut off
		// half-way. If the move gives check, spend an extra ply so the
		// search sees how the check resolves instead of evaluating a
		// position that is about to change sharply.
		extension := 0
		if c.extensions && ply < maxSearchPly-2 && moves.IsInCheck(&next.Board, color.Other()) {
			extension = 1
			reduction = 0
		}

		var value float64
		if i == 0 {
			value = c.search(&next, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
		} else {
			// Principal variation search: try a zero-width window first.
			if maximizing {
				value = c.search(&next, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, alpha, alpha+1e-6)
				if value > alpha {
					value = c.search(&next, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			} else {
				value = c.search(&next, color.Other(), maximizingFor, depth-1-reduction+extension, ply+1, beta-1e-6, beta)
				if value < beta {
					value = c.search(&next, color.Other(), maximizingFor, depth-1+extension, ply+1, alpha, beta)
				}
			}
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
	if len(legal) == 0 {
		return game.Move{}, false
	}
	if ev == nil {
		ev = &Eval{}
	}
	if ev.Table == nil {
		ev.Table = NewTranspositionTable(20)
	}
	ctx := &searchCtx{ev: ev, quiescence: useQuiescence, extensions: ev.Extensions}

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
			next := game.Game{Board: g.Board, Turn: color}
			next.ApplyMove(m.From, m.To)
			score := ctx.search(&next, color.Other(), color, depth-1, 1, alpha, beta)
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

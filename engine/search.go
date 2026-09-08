package engine

import (
	"math/rand"
	"sync/atomic"

	"chess/board"
	"chess/game"
	"chess/moves"
)

func randIntn(n int) int { return rand.Intn(n) }

// SeedRandom fixes the tie-break RNG so that two runs of a measurement
// see the same positions and the same tie-breaks, making arms comparable
// instead of each arm getting its own sample.
func SeedRandom(seed int64) { rand.Seed(seed) }

const mateScore = 1000

// quiesceNodes counts quiescence nodes so nodes-per-second reflects the
// whole search, not just the main tree.
var quiesceNodes int64

func terminalScore(g *game.Game, color, maximizingFor board.Color, depthLeft int) float64 {
	if moves.IsInCheck(&g.Board, color) {
		// `color` is the side to move and has no legal moves: it is
		// checkmated. That's terrible for `color` and great for the other
		// side -- so the sign is negative when the mated side is the one
		// we're maximizing for.
		sign := 1.0
		if color == maximizingFor {
			sign = -1.0
		}
		return sign * (mateScore + float64(depthLeft))
	}
	return 0
}

// mvvLvaValue scores a move for ordering: most valuable victim, least
// valuable attacker. Trying "pawn takes queen" before "queen takes pawn"
// makes alpha-beta cut off far sooner, because the best move is usually
// found first -- which matters more than any other single search tweak,
// since the whole point of alpha-beta is that good ordering prunes more.
var mvvLvaPiece = [6]int{board.Pawn: 1, board.Knight: 3, board.Bishop: 3, board.Rook: 5, board.Queen: 9, board.King: 20}

func moveOrderScore(g *game.Game, m game.Move) int {
	victim, isCapture := g.Board.PieceAt(m.To)
	if !isCapture {
		return 0 // quiet moves last
	}
	attacker, _ := g.Board.PieceAt(m.From)
	// Higher is better: big victim, small attacker.
	return 100 + mvvLvaPiece[victim.Type]*10 - mvvLvaPiece[attacker.Type]
}

// orderInPlace sorts the caller's slice in place, best-looking moves
// first.
func orderInPlace(g *game.Game, out []game.Move) []game.Move {
	captureFirst := func(m game.Move) int {
		return -moveOrderScore(g, m)
	}
	// simple insertion sort by key: stable, and the slices here are small
	// (at most a few dozen moves), so O(n^2) is not a concern.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && captureFirst(out[j-1]) > captureFirst(out[j]) {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

func Minimax(g *game.Game, color, maximizingFor board.Color, depth int, alpha, beta float64, weights Weights) float64 {
	return minimaxOpts(g, color, maximizingFor, depth, alpha, beta, &Eval{Weights: weights}, false)
}

// minimaxOpts adds quiescence: at the search horizon, keep following
// captures until the position is quiet. Without it the engine happily
// stops mid-exchange and scores a position it has only half-evaluated
// (the horizon effect) -- e.g. counting a queen it just "won" without
// seeing the recapture on the very next ply.
func minimaxOpts(g *game.Game, color, maximizingFor board.Color, depth int, alpha, beta float64, ev *Eval, useQuiescence bool) float64 {
	// Transposition probe: the same position is reached by many different
	// move orders, and without this the search re-explores each arrival
	// from scratch.
	var key uint64
	tt := ev.table()
	if tt != nil && depth > 0 {
		key = zobristHash(g)
		if score, ok := tt.probe(key, depth, maximizingFor, alpha, beta); ok {
			return score
		}
	}
	origAlpha, origBeta := alpha, beta

	var moveBuf [48]game.Move
	legalMoves := g.AppendLegalMoves(moveBuf[:0], color)
	if len(legalMoves) == 0 {
		return terminalScore(g, color, maximizingFor, depth)
	}
	if depth == 0 {
		if useQuiescence {
			return quiesce(g, color, maximizingFor, alpha, beta, ev, 0)
		}
		return evalPosition(g, maximizingFor, ev)
	}

	maximizing := color == maximizingFor

	// Null-move pruning: let the side to move "pass" and search the reply
	// shallower. If even giving the opponent a free move leaves the
	// position good enough to cause a cutoff, the real move will too, so
	// the whole subtree can be skipped. Disabled in check (passing is
	// nonsense there) and near the leaves (no depth left to save).
	if ev.useNullMove() && depth >= 3 && !moves.IsInCheck(&g.Board, color) {
		// The board is copied here, so unlike search2 this cannot corrupt
		// the caller's position. The en passant square still has to go:
		// a null hands the move to the opponent, and any en passant right
		// belonged to the side now passing. Leaving it set lets the
		// opponent capture en passant onto a square nobody double-pushed
		// to, so the null-move score is computed from an illegal position.
		passed := game.Game{Board: g.Board, Turn: color.Other()}
		passed.Board.SetEPSquare(board.Sq{}, false)
		const reduction = 2
		score := minimaxOpts(&passed, color.Other(), maximizingFor, depth-1-reduction, alpha, beta, ev, useQuiescence)
		if maximizing && score >= beta {
			return score
		}
		if !maximizing && score <= alpha {
			return score
		}
	}

	best := negInf
	if !maximizing {
		best = posInf
	}
	for _, m := range orderInPlace(g, legalMoves) {
		// Stack value, not game.From's heap pointer: the search builds one
		// child position per node, and heap-allocating a Game (which holds
		// the whole 144-cell board array) for each was the single largest
		// source of allocated bytes in the profile.
		next := game.Game{Board: g.Board, Turn: color}
		next.ApplyMove(m.From, m.To)
		value := minimaxOpts(&next, color.Other(), maximizingFor, depth-1, alpha, beta, ev, useQuiescence)
		if maximizing {
			if value > best {
				best = value
			}
			if best > alpha {
				alpha = best
			}
		} else {
			if value < best {
				best = value
			}
			if best < beta {
				beta = best
			}
		}
		if beta <= alpha {
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
		tt.store(key, best, depth, flag, maximizingFor)
	}
	return best
}

const negInf = -1e18
const posInf = 1e18

// quiesce searches only captures from a leaf position, so the evaluation
// is taken from a position where no immediate material swing is pending.
// maxQuiescePly caps it: a long forced capture sequence is possible but
// rare, and an unbounded extension can blow up the node count.
const maxQuiescePly = 4

func quiesce(g *game.Game, color, maximizingFor board.Color, alpha, beta float64, ev *Eval, ply int) float64 {
	atomic.AddInt64(&quiesceNodes, 1)
	if deadPosition(&g.Board) {
		return 0
	}
	// Terminal first. Quiescence used to stand pat in any position at all,
	// so a capture that delivered mate was scored as the material it took
	// and a stalemate as the material on the board.
	var moveBuf [96]game.Move
	legal, inCheck := g.AppendLegalMovesInCheck(moveBuf[:0], color)
	if len(legal) == 0 {
		return terminalScore(g, color, maximizingFor, 0)
	}
	standPat := evalPositionFor(g, color, maximizingFor, ev)
	if ply >= ev.quiescePly() {
		return standPat
	}
	maximizing := color == maximizingFor

	// Stand-pat: the side to move can decline to capture, so a quiet
	// evaluation is a lower bound for the maximizer (upper for minimizer).
	// Not in check: a check cannot be declined, so every evasion is
	// searched and the static score bounds nothing.
	best := standPat
	if inCheck {
		best = negInf
		if !maximizing {
			best = posInf
		}
	} else if maximizing {
		if standPat >= beta {
			return standPat
		}
		if standPat > alpha {
			alpha = standPat
		}
	} else {
		if standPat <= alpha {
			return standPat
		}
		if standPat < beta {
			beta = standPat
		}
	}

	for _, m := range legal {
		isCapture := isCaptureMove(g, m)
		promotes := pawnReachesLastRank(g, m)
		if !inCheck && !isCapture && !promotes {
			continue
		}
		// Static exchange pruning: skip captures that lose material on
		// their face (a big attacker taking a small defended victim).
		// Never a checking capture: Qxf7 mate is a queen taking a defended
		// pawn, and it was being pruned as one.
		losesOnItsFace := false
		if !inCheck && isCapture && !promotes && ev.useSEEPruning() {
			victim, onSquare := g.Board.PieceAt(m.To)
			if !onSquare {
				victim = board.Piece{Type: board.Pawn}
			}
			attacker, _ := g.Board.PieceAt(m.From)
			losesOnItsFace = mvvLvaPiece[attacker.Type] > mvvLvaPiece[victim.Type] &&
				moves.IsAttackedBy(&g.Board, m.To, color.Other())
		}
		undo, _ := makeSearchMove(g, m)
		if losesOnItsFace && !moves.IsInCheck(&g.Board, color.Other()) {
			g.Board.UnmakeMove(undo)
			continue
		}
		value := quiesce(g, color.Other(), maximizingFor, alpha, beta, ev, ply+1)
		g.Board.UnmakeMove(undo)
		if maximizing {
			if value > best {
				best = value
			}
			if best > alpha {
				alpha = best
			}
		} else {
			if value < best {
				best = value
			}
			if best < beta {
				beta = best
			}
		}
		if beta <= alpha {
			break
		}
	}
	return best
}

// ChooseMove picks the best move for color at the given depth. Ties for
// the best score are broken randomly instead of always the first move:
// two near-identical engines (e.g. a mutated challenger vs its parent
// champion in training) evaluate similarly and, without this, replay the
// literal same game and repeat into an instant draw every time.
func ChooseMove(g *game.Game, color board.Color, depth int, weights Weights) (game.Move, bool) {
	return chooseMoveOpts(g, color, depth, &Eval{Weights: weights}, false)
}

func chooseMoveOpts(g *game.Game, color board.Color, depth int, ev *Eval, useQuiescence bool) (game.Move, bool) {
	legalMoves := orderInPlace(g, g.AllLegalMoves(color))
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}

	bestScore := negInf
	best := make([]game.Move, 0, len(legalMoves))
	for _, m := range legalMoves {
		next := game.Game{Board: g.Board, Turn: color}
		next.ApplyMove(m.From, m.To)
		score := minimaxOpts(&next, color.Other(), color, depth-1, negInf, posInf, ev, useQuiescence)
		if score > bestScore {
			bestScore = score
			best = best[:0]
			best = append(best, m)
		} else if score == bestScore {
			best = append(best, m)
		}
	}

	// Prefer a tied-best move that doesn't repeat a position already seen,
	// when the game is tracking repetition (the real game-playing loops,
	// not the search's own throwaway positions). A shallow heuristic-only
	// search has no other way to distinguish a repeating shuffle from real
	// progress, and relying on random tie-break luck alone to avoid an
	// eternal 2-cycle turned out not to be reliable enough in practice.
	if g.TrackRepetition && len(best) > 1 {
		fresh := best[:0:0]
		for _, m := range best {
			if g.CountIfPlayed(m.From, m.To) == 0 {
				fresh = append(fresh, m)
			}
		}
		if len(fresh) > 0 {
			best = fresh
		}
	}
	return best[rand.Intn(len(best))], true
}

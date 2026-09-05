package engine

import (
	"math/rand"

	"chess/board"
	"chess/game"
	"chess/moves"
)

func randIntn(n int) int { return rand.Intn(n) }

const mateScore = 1000

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

// orderInPlace: try captures first, a cheap move-ordering heuristic that
// lets alpha-beta prune more. Sorts the caller's slice in place -- it
// used to copy, which allocated once per search node.
func orderInPlace(g *game.Game, out []game.Move) []game.Move {
	captureFirst := func(m game.Move) int {
		if _, occupied := g.Board.PieceAt(m.To); occupied {
			return 0
		}
		return 1
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
	return minimaxOpts(g, color, maximizingFor, depth, alpha, beta, weights, false)
}

// minimaxOpts adds quiescence: at the search horizon, keep following
// captures until the position is quiet. Without it the engine happily
// stops mid-exchange and scores a position it has only half-evaluated
// (the horizon effect) -- e.g. counting a queen it just "won" without
// seeing the recapture on the very next ply.
func minimaxOpts(g *game.Game, color, maximizingFor board.Color, depth int, alpha, beta float64, weights Weights, useQuiescence bool) float64 {
	var moveBuf [48]game.Move
	legalMoves := g.AppendLegalMoves(moveBuf[:0], color)
	if len(legalMoves) == 0 {
		return terminalScore(g, color, maximizingFor, depth)
	}
	if depth == 0 {
		if useQuiescence {
			return quiesce(g, color, maximizingFor, alpha, beta, weights, 0)
		}
		return PositionScore(&g.Board, maximizingFor, weights)
	}

	maximizing := color == maximizingFor
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
		value := minimaxOpts(&next, color.Other(), maximizingFor, depth-1, alpha, beta, weights, useQuiescence)
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

const negInf = -1e18
const posInf = 1e18

// quiesce searches only captures from a leaf position, so the evaluation
// is taken from a position where no immediate material swing is pending.
// maxQuiescePly caps it: a long forced capture sequence is possible but
// rare, and an unbounded extension can blow up the node count.
const maxQuiescePly = 4

func quiesce(g *game.Game, color, maximizingFor board.Color, alpha, beta float64, weights Weights, ply int) float64 {
	standPat := PositionScore(&g.Board, maximizingFor, weights)
	if ply >= maxQuiescePly {
		return standPat
	}
	maximizing := color == maximizingFor

	// Stand-pat: the side to move can decline to capture, so a quiet
	// evaluation is a lower bound for the maximizer (upper for minimizer).
	if maximizing {
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

	var moveBuf [48]game.Move
	best := standPat
	for _, m := range g.AppendLegalMoves(moveBuf[:0], color) {
		if _, isCapture := g.Board.PieceAt(m.To); !isCapture {
			continue
		}
		next := game.Game{Board: g.Board, Turn: color}
		next.ApplyMove(m.From, m.To)
		value := quiesce(&next, color.Other(), maximizingFor, alpha, beta, weights, ply+1)
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
	return chooseMoveOpts(g, color, depth, weights, false)
}

func chooseMoveOpts(g *game.Game, color board.Color, depth int, weights Weights, useQuiescence bool) (game.Move, bool) {
	legalMoves := orderInPlace(g, g.AllLegalMoves(color))
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}

	bestScore := negInf
	best := make([]game.Move, 0, len(legalMoves))
	for _, m := range legalMoves {
		next := game.Game{Board: g.Board, Turn: color}
		next.ApplyMove(m.From, m.To)
		score := minimaxOpts(&next, color.Other(), color, depth-1, negInf, posInf, weights, useQuiescence)
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

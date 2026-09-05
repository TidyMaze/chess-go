// Package engine implements position evaluation, alpha-beta search, and
// the self-play training loop.
package engine

import "chess/board"

// Weights is indexed by PieceType rather than keyed by it: this is read
// for every piece at every evaluated node, and a map lookup there showed
// up as ~10% of total CPU in the profile.
type Weights *[6]float64

func DefaultWeights() Weights {
	return &[6]float64{
		board.Pawn: 1, board.Knight: 3, board.Bishop: 3,
		board.Rook: 5, board.Queen: 9, board.King: 0,
	}
}

func CloneWeights(w Weights) Weights {
	out := *w
	return &out
}

var defaultWeights = DefaultWeights()

// centerBonus: fixed (non-evolving) positional nudge. Pure material
// evaluation can't tell a knight on a rim square from one commanding the
// center; small relative to material so it only breaks ties.
var centerBonus = [6]float64{board.Knight: 0.1, board.Bishop: 0.05, board.Pawn: 0.02}

func centerDistance(s board.Sq) float64 {
	df := abs(float64(s.File) - 3.5)
	dr := abs(float64(s.Rank) - 3.5)
	if df > dr {
		return df
	}
	return dr
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// materialAndCentralization: fused single pass over PiecesOf(color) --
// computes both material and the centralization bonus together instead
// of two separate scans, since PiecesOf is non-trivial cost at the node
// counts search reaches.
func materialAndCentralization(b *board.Board, color board.Color, weights Weights, usePST bool) (material, center float64) {
	var buf [16]board.PieceAtSquare
	for _, ps := range b.AppendPiecesOf(buf[:0], color) {
		material += weights[ps.Type]
		if usePST {
			center += pstValue(ps.Type, ps.Sq, color)
		} else if bonus := centerBonus[ps.Type]; bonus != 0 {
			center += bonus * (3.5 - centerDistance(ps.Sq))
		}
	}
	return
}

// Eval bundles everything the evaluation needs. It travels with the
// search instead of living in package-level state, so two engines with
// different evaluations can play each other concurrently -- which is
// exactly what the Elo ladder does.
type Eval struct {
	Weights Weights
	UsePST  bool
}

func (e *Eval) weightsOrDefault() Weights {
	if e == nil || e.Weights == nil {
		return defaultWeights
	}
	return e.Weights
}

func (e *Eval) usePST() bool { return e != nil && e.UsePST }

func MaterialScore(b *board.Board, color board.Color, weights Weights) float64 {
	if weights == nil {
		weights = defaultWeights
	}
	own, _ := materialAndCentralization(b, color, weights, false)
	enemy, _ := materialAndCentralization(b, color.Other(), weights, false)
	return own - enemy
}

// kingDrivingBonus: endgame technique -- with a winning material edge,
// push the enemy king to the board edge and bring your own king closer
// to help deliver mate.
func kingDrivingBonus(b *board.Board, color board.Color) float64 {
	own := b.KingSquare(color)
	enemy := b.KingSquare(color.Other())
	pushToEdge := centerDistance(enemy)
	df := abs(float64(own.File - enemy.File))
	dr := abs(float64(own.Rank - enemy.Rank))
	kingsDistance := df
	if dr > kingsDistance {
		kingsDistance = dr
	}
	return pushToEdge*0.1 + (7-kingsDistance)*0.05
}

func PositionScore(b *board.Board, color board.Color, weights Weights) float64 {
	return PositionScoreEval(b, color, &Eval{Weights: weights})
}

func PositionScoreEval(b *board.Board, color board.Color, ev *Eval) float64 {
	weights := ev.weightsOrDefault()
	usePST := ev.usePST()
	ownMaterial, ownCenter := materialAndCentralization(b, color, weights, usePST)
	enemyMaterial, enemyCenter := materialAndCentralization(b, color.Other(), weights, usePST)
	score := (ownMaterial - enemyMaterial) + (ownCenter - enemyCenter)

	if score >= 4 {
		score += kingDrivingBonus(b, color)
	} else if score <= -4 {
		score -= kingDrivingBonus(b, color.Other())
	}
	return score
}

package engine

import (
	"chess/board"
	"chess/moves"
)

// Positional evaluation terms beyond material and piece-square tables.
// Each is cheap (one pass over the pieces plus per-file summaries) and
// expresses something the search cannot discover at shallow depth: pawn
// structure and king safety only pay off many moves later, well past the
// horizon.

// StructureWeights scales each term. The first hand-picked set measured
// -89 Elo: guessed weights are as likely to hurt as help, and the passed
// pawn bonus in particular double-counted with the endgame pawn table,
// which already rewards advancement heavily. Weights are variables so
// they can be swept and measured rather than assumed.
type StructureWeights struct {
	PassedBase    float64
	PassedPerRank float64
	Isolated      float64
	Doubled       float64
	RookOpen      float64
	RookSemiOpen  float64
	KingShield    float64
}

// Tuned by sweeping variants against the previous best, 60 games each:
//
//	hand-picked full set                 -89 Elo
//	quarter-strength full set            -12 Elo
//	rook files + isolated only           +23 Elo
//	pawn structure + rook files          +95 Elo   <- kept
//
// The passed-pawn and king-shield terms were the harmful ones. Passed
// pawns double-count with the endgame pawn table, which already rewards
// advancement heavily; the king-shield term is too crude to be worth its
// weight (it counts pawns on neighbouring files without caring where
// they are or whether the king is actually under attack).
var defaultStructureWeights = StructureWeights{
	Isolated: 0.12, Doubled: 0.15, RookOpen: 0.20, RookSemiOpen: 0.10,
}

// DefaultStructureWeights reports the built-in weights. The live values
// travel on Eval so concurrent engines do not share them.
func DefaultStructureWeights() StructureWeights { return defaultStructureWeights }

// pawnFiles summarises pawn placement per file for one side: how many
// pawns, and the most advanced one (from that side's point of view).
type pawnFiles struct {
	count    [8]int
	mostAdv  [8]int // rank from the owner's perspective, -1 if none
	anyPawns bool
}

func scanPawns(b *board.Board, color board.Color) pawnFiles {
	var pf pawnFiles
	for i := range pf.mostAdv {
		pf.mostAdv[i] = -1
	}
	var buf [16]board.PieceAtSquare
	for _, ps := range b.AppendPiecesOf(buf[:0], color) {
		if ps.Type != board.Pawn {
			continue
		}
		rank := ps.Sq.Rank
		if color == board.Black {
			rank = 7 - rank
		}
		pf.count[ps.Sq.File]++
		if rank > pf.mostAdv[ps.Sq.File] {
			pf.mostAdv[ps.Sq.File] = rank
		}
		pf.anyPawns = true
	}
	return pf
}

// structureScore evaluates pawn structure, rook placement and king
// shelter for one side.
func structureScore(b *board.Board, color board.Color, own, enemy pawnFiles, phase float64, w StructureWeights) float64 {
	score := 0.0
	var buf [16]board.PieceAtSquare

	for _, ps := range b.AppendPiecesOf(buf[:0], color) {
		file := ps.Sq.File
		switch ps.Type {
		case board.Pawn:
			rank := ps.Sq.Rank
			if color == board.Black {
				rank = 7 - rank
			}
			// Passed: no enemy pawn ahead on this file or either
			// neighbouring file. Worth more the further it has advanced,
			// and worth much more once the pieces come off.
			if isPassed(file, rank, enemy) {
				score += (w.PassedBase + w.PassedPerRank*float64(rank)) * (2 - phase)
			}
			// Isolated: no friendly pawn on either neighbouring file, so
			// it can never be defended by a pawn.
			if !hasNeighbourPawn(file, own) {
				score -= w.Isolated
			}
			// Doubled: pawns stacked on one file block each other.
			if own.count[file] > 1 {
				score -= w.Doubled / float64(own.count[file])
			}
		case board.Rook:
			// Rooks want files without pawns in the way.
			if own.count[file] == 0 {
				if enemy.count[file] == 0 {
					score += w.RookOpen
				} else {
					score += w.RookSemiOpen
				}
			}
		case board.King:
			// King safety: friendly pawns directly in front of the king
			// are its shelter. Only matters while there are pieces left to
			// attack it, so it fades out with the phase.
			shield := 0
			for df := -1; df <= 1; df++ {
				f := file + df
				if f < 0 || f > 7 {
					continue
				}
				if own.count[f] > 0 {
					shield++
				}
			}
			score += float64(shield) * w.KingShield * phase
		}
	}
	return score
}

func isPassed(file, rank int, enemy pawnFiles) bool {
	for df := -1; df <= 1; df++ {
		f := file + df
		if f < 0 || f > 7 {
			continue
		}
		// enemy.mostAdv is from the enemy's perspective; an enemy pawn on
		// enemy-rank r sits on our rank 7-r.
		if enemy.mostAdv[f] >= 0 && 7-enemy.mostAdv[f] > rank {
			return false
		}
	}
	return true
}

func hasNeighbourPawn(file int, own pawnFiles) bool {
	for _, f := range [2]int{file - 1, file + 1} {
		if f >= 0 && f <= 7 && own.count[f] > 0 {
			return true
		}
	}
	return false
}

// structurePieces is structureScore over an already-collected piece
// list, so the evaluation does not walk the board again per side.
func structurePieces(pieces []board.ColoredPiece, color board.Color, own, enemy pawnFiles, phase float64, w StructureWeights) float64 {
	score := 0.0
	for _, ps := range pieces {
		if ps.Color != color {
			continue
		}
		file := ps.Sq.File
		switch ps.Type {
		case board.Pawn:
			rank := ps.Sq.Rank
			if color == board.Black {
				rank = 7 - rank
			}
			if isPassed(file, rank, enemy) {
				score += (w.PassedBase + w.PassedPerRank*float64(rank)) * (2 - phase)
			}
			if !hasNeighbourPawn(file, own) {
				score -= w.Isolated
			}
			if own.count[file] > 1 {
				score -= w.Doubled / float64(own.count[file])
			}
		case board.Rook:
			if own.count[file] == 0 {
				if enemy.count[file] == 0 {
					score += w.RookOpen
				} else {
					score += w.RookSemiOpen
				}
			}
		case board.King:
			shield := 0
			for df := -1; df <= 1; df++ {
				f := file + df
				if f < 0 || f > 7 {
					continue
				}
				if own.count[f] > 0 {
					shield++
				}
			}
			score += float64(shield) * w.KingShield * phase
		}
	}
	return score
}

// mobilityWeights is the value of one available square, per piece type.
//
// Mobility is the most standard evaluation term this engine did not
// have. It captures something material and piece-square tables cannot:
// a knight on a good square with every exit covered is worth less than
// the same knight with somewhere to go, and a rook behind its own pawns
// is not doing anything wherever it stands.
//
// Pawns and kings are excluded. A pawn's mobility is already what the
// pawn tables and the structure terms describe, and king "mobility" in
// the middlegame is a liability rather than an asset, so counting it
// would push the king into the open.
// DefaultMobilityWeights are the starting values, replaced by the fit.
func DefaultMobilityWeights() [6]float64 { return defaultMobilityWeights }

// Hand-picked, and deliberately kept that way. Fitting these against
// Stockfish static evaluations produced 0.055 / 0.100 / 0.085 / 0.065,
// which fits 14% better and plays 82 Elo worse: -73 +/- 35 against +9 +/-
// 34 for these values, over 400 games each. See engine/tuned.go for why.
var defaultMobilityWeights = [6]float64{
	board.Knight: 0.04, board.Bishop: 0.045, board.Rook: 0.025, board.Queen: 0.015,
}

// mobilityScore counts the squares each piece can reach, using
// pseudo-legal targets: whether a move leaves its own king in check is a
// question for the search, and the extra cost of answering it here would
// be far larger than the accuracy it buys.
func mobilityScore(b *board.Board, pieces []board.ColoredPiece, color board.Color, weights *[6]float64) float64 {
	var buf [28]board.Sq
	score := 0.0
	for _, p := range pieces {
		if p.Color != color {
			continue
		}
		w := weights[p.Type]
		if w == 0 {
			continue
		}
		score += w * float64(len(moves.AppendLegalTargets(buf[:0], b, p.Sq, color, p.Type)))
	}
	return score
}

// King safety by counting attackers.
//
// The existing king term counts shelter pawns, which says whether the
// king has a roof but nothing about whether anyone is coming. Every fit
// so far has pushed that term around without conviction and one drove it
// to zero, which is what a term too crude to be worth its weight looks
// like.
//
// This is the standard form: count the enemy pieces that attack the
// squares around the king, weight them by what they are, and make the
// penalty superlinear in how many there are. Two attackers are far more
// than twice one attacker, because mating nets need pieces to cooperate,
// and a linear term cannot say that at any weight.
//
// It matters more now than it would have earlier: before castling
// existed, kings sat on e1 all game and every king term was describing a
// situation that never varied.
var kingAttackerWeight = [6]float64{
	board.Knight: 2, board.Bishop: 2, board.Rook: 3, board.Queen: 5,
}

// kingDanger rises faster than the number of attackers, then flattens:
// past four attackers the king is lost and more does not change that.
var kingDangerScale = [8]float64{0, 0.10, 0.35, 0.70, 1.00, 1.15, 1.25, 1.30}

// kingSafetyPenalty is how bad `color`'s king position is, as a positive
// number to be subtracted from color's score.
func kingSafetyPenalty(b *board.Board, pieces []board.ColoredPiece, color board.Color, phase float64, weight float64) float64 {
	if weight == 0 || phase < 0.25 {
		// In an endgame the king is a fighting piece and being near the
		// action is correct, so the whole idea inverts. Left to the
		// endgame king tables and kingDrivingBonus.
		return 0
	}
	king := b.KingSquare(color)
	enemy := color.Other()

	var buf [28]board.Sq
	attackers, weightSum := 0, 0.0
	for _, p := range pieces {
		if p.Color != enemy {
			continue
		}
		w := kingAttackerWeight[p.Type]
		if w == 0 {
			continue
		}
		hits := 0
		for _, t := range moves.AppendLegalTargets(buf[:0], b, p.Sq, enemy, p.Type) {
			df, dr := t.File-king.File, t.Rank-king.Rank
			if df >= -1 && df <= 1 && dr >= -1 && dr <= 1 {
				hits++
			}
		}
		if hits > 0 {
			attackers++
			weightSum += w * float64(hits)
		}
	}
	if attackers == 0 {
		return 0
	}
	if attackers >= len(kingDangerScale) {
		attackers = len(kingDangerScale) - 1
	}
	return weight * kingDangerScale[attackers] * weightSum * phase
}

// Three terms the evaluation does not have, each cheap and each standard.
//
// Added together because the useful lesson from mobility and king safety
// is that a term worth 5 to 15 Elo cannot be confirmed on its own at any
// affordable sample size: 400 games resolve +/- 34. Several such terms
// stacked can be confirmed, and then the ones that turn out to be dead
// weight can be removed one at a time.
type extraWeights struct {
	RookSeventh float64 // a rook on the seventh cuts off the king and eats pawns
	RookDoubled float64 // two rooks on one file are worth more than two rooks
	Tempo       float64 // having the move is worth something in itself
}

var defaultExtras = extraWeights{RookSeventh: 0.20, RookDoubled: 0.12, Tempo: 0.06}

// extraScore evaluates the terms above for one side.
func extraScore(pieces []board.ColoredPiece, color board.Color, phase float64, w extraWeights) float64 {
	seventh := 6
	if color == board.Black {
		seventh = 1
	}
	score := 0.0
	var rookFiles [8]int
	for _, p := range pieces {
		if p.Color != color || p.Type != board.Rook {
			continue
		}
		rookFiles[p.Sq.File]++
		if p.Sq.Rank == seventh {
			// Worth most while the enemy king is still stuck on its back
			// rank, which is what phase approximates.
			score += w.RookSeventh * phase
		}
	}
	for _, n := range rookFiles {
		if n > 1 {
			score += w.RookDoubled
		}
	}
	return score
}

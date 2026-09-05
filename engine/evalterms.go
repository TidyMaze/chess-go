package engine

import "chess/board"

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
var structureWeights = StructureWeights{
	Isolated: 0.12, Doubled: 0.15, RookOpen: 0.20, RookSemiOpen: 0.10,
}

// SetStructureWeights replaces the tuning constants (used by the sweep).
func SetStructureWeights(w StructureWeights) { structureWeights = w }

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
func structureScore(b *board.Board, color board.Color, own, enemy pawnFiles, phase float64) float64 {
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
				score += (structureWeights.PassedBase + structureWeights.PassedPerRank*float64(rank)) * (2 - phase)
			}
			// Isolated: no friendly pawn on either neighbouring file, so
			// it can never be defended by a pawn.
			if !hasNeighbourPawn(file, own) {
				score -= structureWeights.Isolated
			}
			// Doubled: pawns stacked on one file block each other.
			if own.count[file] > 1 {
				score -= structureWeights.Doubled / float64(own.count[file])
			}
		case board.Rook:
			// Rooks want files without pawns in the way.
			if own.count[file] == 0 {
				if enemy.count[file] == 0 {
					score += structureWeights.RookOpen
				} else {
					score += structureWeights.RookSemiOpen
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
			score += float64(shield) * structureWeights.KingShield * phase
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

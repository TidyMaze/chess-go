package engine

import "chess/board"

// Evaluation parameters fitted by Texel tuning (Osterlund, 2014) rather
// than chosen by hand: see cmd `tune`. 166,201 positions from 1,994
// self-play games, each labelled with the result of the game it came
// from, fitted by coordinate descent against the squared error between
// sigmoid(K * score) and that result, K = 0.30.
//
// Squared error fell 1.78% on the training slice and 2.10% on a shuffled
// held-out slice the fit never saw. The held-out slice improving slightly
// more than the training slice is the sign that wanted checking: it means
// the fit found real signal rather than memorising these particular
// games.
//
// A lower prediction error is not the same thing as more Elo, so these
// are behind Player.Tuned and have to earn their place in a match before
// becoming the default.
//
// What the fit changed, and what it is saying:
//
//   - The rook drops from 5.00 to 4.70 while the bonus for a rook on an
//     open file climbs from 0.20 to 0.76. Taken together: a rook's worth
//     depends on the file it stands on far more than the hand-written
//     values allowed, so value moves out of the piece and into its
//     placement.
//   - The pawn table is scaled almost to nothing (0.10) while the bishop
//     and rook tables roughly double. The pawn table was pulling much
//     harder than the data supports, which is consistent with the earlier
//     finding that a hand-added passed-pawn bonus made things worse by
//     double-counting with it.
//   - The king table goes to zero. In self-play games between engines of
//     this strength, where the king sits in the middlegame simply does
//     not predict who wins, so the fit has nothing to hold it up. This is
//     the parameter to distrust most: it is a statement about this
//     training set, not about chess, and a real king-safety term (attack
//     counting rather than counting shelter pawns) is the fix rather than
//     a scalar on a table.
func TunedWeights() Weights {
	return &[6]float64{
		board.Pawn: 1, board.Knight: 3.35, board.Bishop: 3.05,
		board.Rook: 4.70, board.Queen: 9.50, board.King: 0,
	}
}

func TunedPSTScale() [6]float64 {
	return [6]float64{
		board.Pawn: 0.10, board.Knight: 1.00, board.Bishop: 1.70,
		board.Rook: 2.00, board.Queen: 1.10, board.King: 0.00,
	}
}

func TunedStructure() StructureWeights {
	return StructureWeights{
		PassedBase: 0.02, PassedPerRank: 0.14,
		Isolated: 0.02, Doubled: 0.01,
		RookOpen: 0.76, RookSemiOpen: 0.86,
		KingShield: 0.04,
	}
}

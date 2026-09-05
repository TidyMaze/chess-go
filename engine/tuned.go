package engine

import "chess/board"

// Evaluation parameters fitted by Texel-style tuning (Osterlund, 2014).
//
// **These are not used in play, and should not be.** They are kept
// because what they measured is worth more than what they contain.
//
// Three fits were run against three different targets. All three
// improved the objective they were given. None improved play:
//
//	target                        fit improvement    measured Elo
//	self-play game results        1.8% / 2.1%        -16 +/- 48
//	Stockfish depth-12 search     11.5% / 10.8%      +20 +/- 26
//	Stockfish static evaluation   14.0% / 14.4%      -55 +/- 34
//
// (Training slice / held-out slice. The held-out slice improved as much
// as the training one every time, so none of this is overfitting: the
// fits are real, they are just fitting the wrong thing.)
//
// The mobility weights make the point most sharply, because they are one
// isolated term. Hand-picked values measured +9 +/- 34. The fitted values
// are two to four times larger, fit 14% better, and measured -73 +/- 35
// on their own. That is an 82 Elo swing in the wrong direction, bought
// with a better fit.
//
// The reason is that the target is the wrong objective. Stockfish's
// static evaluation is an NNUE built to be corrected by a twenty-ply
// search: it can afford to say almost nothing about king placement,
// because its search sees the attack coming. A five-ply search cannot,
// so it needs an evaluation that overstates exactly the things a deep
// search would discover for itself. Fitting one to the other strips out
// the terms this engine most depends on, which is visible directly in
// the fitted numbers: the king piece-square table goes to zero and the
// queen table to 0.40.
//
// Squared prediction error and playing strength are different objectives,
// and past a point they diverge. The way to tune for strength is to
// optimise against game results (SPSA is the standard method), not
// against another engine's opinion.
func TunedWeights() Weights {
	return &[6]float64{
		board.Pawn: 1, board.Knight: 3.95, board.Bishop: 3.80,
		board.Rook: 5.45, board.Queen: 10.80, board.King: 0,
	}
}

func TunedPSTScale() [6]float64 {
	return [6]float64{
		board.Pawn: 0.70, board.Knight: 1.00, board.Bishop: 1.40,
		board.Rook: 1.40, board.Queen: 0.40, board.King: 0.00,
	}
}

// TunedMobility is the value of one reachable square per piece.
//
// These were the most useful thing the fit produced. The hand-picked
// values (0.04 / 0.045 / 0.025 / 0.015) measured +9 +/- 34 Elo, which is
// what a guess is worth. The fit puts every one of them two to four
// times higher and, unlike the hand-picked set, ranks them the way the
// term should be ranked: a bishop's squares are worth most because a
// blocked bishop is nearly dead, and a queen's are worth least because
// she has plenty regardless.
func TunedMobility() [6]float64 {
	return [6]float64{
		board.Knight: 0.055, board.Bishop: 0.100,
		board.Rook: 0.085, board.Queen: 0.065,
	}
}

func TunedStructure() StructureWeights {
	return StructureWeights{
		PassedBase: 0.02, PassedPerRank: 0.07,
		Isolated: 0.18, Doubled: 0.01,
		RookOpen: 0.40, RookSemiOpen: 0.36,
		KingShield: 0.26,
	}
}

package engine

import "chess/board"

// Evaluation parameters fitted by Texel-style tuning (Osterlund, 2014),
// but distilled from Stockfish rather than from game outcomes.
//
// The first attempt fitted to self-play results: 166,201 positions each
// labelled with whether White went on to win. It fitted (squared error
// down 1.78% training, 2.10% held out) and produced no Elo at all,
// -16 +/- 48 over 200 games. The fitted values said why. The king
// piece-square table was scaled to zero and the bonus for a rook on a
// semi-open file came out larger than for a fully open one, which is not
// chess. Between two ~1900 engines the result of a game is a single very
// noisy bit about a position, so real terms get washed out.
//
// This set is fitted instead to Stockfish's own evaluation of 60,000
// distinct positions at depth 12: a real number from a ~3600 player
// rather than one noisy bit from a peer. Both sides go through the same
// sigmoid (K = 0.30) so the fit has to improve the evaluation rather than
// rescale it, and positions where Stockfish found a forced mate are
// dropped, since a saturated target says nothing about positional terms.
//
// Squared error fell 11.50% on the training slice and 10.78% on a
// shuffled held-out slice, against 1.78%/2.10% for the outcome fit.
//
// The values are also chess-sensible in ways the outcome fit was not,
// which is the stronger evidence that the teacher signal was the problem:
//
//   - The bishop is now worth slightly more than the knight (4.35 vs
//     4.25). Nothing told the fit that; it is in the data.
//   - An open file beats a semi-open one for a rook (0.80 vs 0.72). The
//     outcome fit had these the wrong way round.
//   - King shelter goes from 0 to 0.22 and the king table from 0 to 0.30.
//     Where the king stands does predict Stockfish's judgement even
//     though it did not predict the result of games at this level, which
//     is exactly the blind spot the outcome fit had.
//
// Piece values come out high against the textbook 1/3/3/5/9 because they
// are fitted jointly with K and with the tables, so only their ratios
// carry meaning: roughly 1 : 4.25 : 4.35 : 5.85 : 12.
func TunedWeights() Weights {
	return &[6]float64{
		board.Pawn: 1, board.Knight: 4.25, board.Bishop: 4.35,
		board.Rook: 5.85, board.Queen: 12.0, board.King: 0,
	}
}

func TunedPSTScale() [6]float64 {
	return [6]float64{
		board.Pawn: 0.90, board.Knight: 1.30, board.Bishop: 3.30,
		board.Rook: 2.00, board.Queen: 2.70, board.King: 0.30,
	}
}

func TunedStructure() StructureWeights {
	return StructureWeights{
		PassedBase: 0.02, PassedPerRank: 0.07,
		Isolated: 0.18, Doubled: 0.07,
		RookOpen: 0.80, RookSemiOpen: 0.72,
		KingShield: 0.22,
	}
}

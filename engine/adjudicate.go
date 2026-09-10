package engine

import "chess/board"

// Adjudication ends a game whose result both engines already agree on.
//
// A match spends most of its time on positions nobody is going to save: a
// side three pieces down plays another forty plies, and a dead-drawn rook
// endgame grinds to the ply cap. Ending those early is what makes a
// measurement affordable, and the safety condition is agreement. One
// engine can be wrong about its own position; two differently configured
// engines, both reporting a six-pawn gap the same way for several plies
// running, are as reliable as the result would have been.
//
// Zero switches a rule off, which is what the differential test uses to
// check adjudication against playing it out.
var (
	// AdjudicateWinPawns is the gap, in pawns, both sides must report.
	AdjudicateWinPawns = 6.0
	// AdjudicateWinPlies is how many consecutive plies they must report it.
	AdjudicateWinPlies = 6
	// AdjudicateDrawPawns is the band around zero both sides must stay in.
	//
	// Measured on this engine's own evaluation, not copied from Fishtest:
	// in games that ended level, the last twenty plies had |score| median
	// 0.121 and p75 0.316, and only 45% of them inside a 0.10 band. With a
	// sixteen-ply streak required, 0.45^16 is one in a million, and the
	// rule had never fired: every drawn game ran to the ply cap.
	AdjudicateDrawPawns = 0.35
	// AdjudicateDrawPlies is how many consecutive plies they must stay in it.
	AdjudicateDrawPlies = 10
	// AdjudicateDrawAfterPly is the earliest ply a draw may be called, so an
	// opening that is level by nature is not called a draw.
	AdjudicateDrawAfterPly = 60
)

// adjudicator watches the scores a game reports and says when the result
// is settled. One per game.
type adjudicator struct {
	leader     board.Color
	winStreak  int
	drawStreak int
}

// observe takes the side the score favours and the size of the gap in
// pawns, and reports the winner once both sides have agreed for long
// enough. Called once per ply, so consecutive plies are the two engines
// alternately confirming the same thing.
func (a *adjudicator) observe(favours board.Color, gapPawns float64, ply int) (board.Color, bool) {
	if AdjudicateWinPawns <= 0 || AdjudicateWinPlies <= 0 {
		return board.White, false
	}
	if gapPawns < AdjudicateWinPawns {
		a.winStreak = 0
		return board.White, false
	}
	if a.winStreak == 0 || a.leader != favours {
		a.leader, a.winStreak = favours, 1
		return board.White, false
	}
	a.winStreak++
	if a.winStreak >= AdjudicateWinPlies {
		return a.leader, true
	}
	return board.White, false
}

// observeDraw reports a draw once both sides have called the position
// level for long enough, and not before the opening is over.
func (a *adjudicator) observeDraw(absScore float64, ply int) (board.Color, bool, bool) {
	if AdjudicateDrawPawns <= 0 || AdjudicateDrawPlies <= 0 {
		return board.White, false, false
	}
	if absScore > AdjudicateDrawPawns {
		a.drawStreak = 0
		return board.White, false, false
	}
	a.drawStreak++
	if ply >= AdjudicateDrawAfterPly && a.drawStreak >= AdjudicateDrawPlies {
		return board.White, false, true
	}
	return board.White, false, false
}

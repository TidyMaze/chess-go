package engine

// razorMargin is how far below the window a static evaluation must stand
// before the node is worth verifying with a quiescence search instead of
// searching it at full width.
//
// It grows with depth because the remaining plies are what a deficit has
// to be recovered in: three pawns down with one ply left is settled,
// three pawns down with three plies left is not. The shape matches
// reverseFutilityMargin, which is the same bet taken from the other side
// of the window.
func razorMargin(depth int) float64 { return 2.0 + 1.5*float64(depth) }

// razorCuts reports whether a node is far enough below its window to be
// worth a quiescence check rather than a full search.
//
// It answers only "verify this", never "prune this". The caller runs
// quiescence and keeps the result only if that agrees, because a static
// evaluation cannot see the capture that wins the material straight
// back, and those are exactly the positions a big deficit produces.
func razorCuts(depth int, staticEval, alpha, beta float64, maximizing bool) bool {
	if depth < 1 || depth > 3 {
		return false
	}
	if maximizing {
		return staticEval+razorMargin(depth) <= alpha
	}
	return staticEval-razorMargin(depth) >= beta
}

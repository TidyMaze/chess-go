package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestEngineCapturesFreeQueen(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{0, 4}, board.Piece{board.White, board.Rook})
	b.Place(board.Sq{0, 6}, board.Piece{board.Black, board.Queen})
	g := game.From(b, board.White)
	move, ok := ChooseMove(g, board.White, 2, nil)
	if !ok || move.From != (board.Sq{0, 4}) || move.To != (board.Sq{0, 6}) {
		t.Errorf("expected rook to capture queen, got %+v ok=%v", move, ok)
	}
}

func TestChooseMoveVariesAmongTiedBestMoves(t *testing.T) {
	seen := map[game.Move]bool{}
	for i := 0; i < 30; i++ {
		g := game.New()
		move, _ := ChooseMove(g, board.White, 1, nil)
		seen[move] = true
	}
	if len(seen) <= 1 {
		t.Errorf("expected tie-break randomization to produce more than one distinct move, got %v", seen)
	}
}

func TestFindsMateInOne(t *testing.T) {
	// White queen e1-e8 delivers back-rank mate: black king h8 boxed in by
	// its own pawns on g7/h7, queen's rank-8 ray covers f8 and g8.
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{4, 0}, board.Piece{board.White, board.Queen})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{6, 6}, board.Piece{board.Black, board.Pawn})
	b.Place(board.Sq{7, 6}, board.Piece{board.Black, board.Pawn})
	g := game.From(b, board.White)
	move, ok := ChooseMove(g, board.White, 2, nil)
	if !ok {
		t.Fatalf("expected a move")
	}
	g.ApplyMove(move.From, move.To)
	if !g.IsCheckmate(board.Black) {
		t.Errorf("expected mate in one, got move %+v, checkmate=%v", move, g.IsCheckmate(board.Black))
	}
}

// Converting K+R vs K into mate exercises the whole stack together: mate
// scoring, the king-driving endgame heuristic, and the anti-repetition
// tie-break. It regressed to 0/10 once from a single inverted sign in
// terminalScore (the engine scored delivering mate as catastrophic and so
// avoided it), which none of the narrower unit tests caught -- they all
// passed while the engine could not win a won endgame.
func TestConvertsKingRookVsKingIntoMate(t *testing.T) {
	attempts, successes := 5, 0
	for attempt := 0; attempt < attempts; attempt++ {
		b := board.NewEmpty()
		b.Place(board.Sq{4, 0}, board.Piece{board.White, board.King})
		b.Place(board.Sq{0, 4}, board.Piece{board.White, board.Rook})
		b.Place(board.Sq{4, 4}, board.Piece{board.Black, board.King})
		g := game.From(b, board.White)
		g.EnableRepetitionTracking()
		// The fifty-move rule would legitimately draw this before the mate
		// lands on slower runs, so this checks the search's ability to
		// finish, not the draw rules (covered separately in game tests).
		for plies := 0; plies < 200; plies++ {
			if g.IsCheckmate(g.Turn) || g.IsStalemate(g.Turn) || g.KingCaptured {
				break
			}
			move, ok := ChooseMove(g, g.Turn, 2, nil)
			if !ok {
				break
			}
			g.ApplyMove(move.From, move.To)
		}
		if g.IsCheckmate(g.Turn) {
			successes++
		}
	}
	if successes < attempts {
		t.Errorf("expected all %d attempts to reach checkmate, got %d", attempts, successes)
	}
}

package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestKingSafetyOpenFilePenalty(t *testing.T) {
	// Position C: Black rook on e8 directly opposing White king on e1 with open e-file.
	gOpen, err := game.ParseFEN("4r1k1/8/8/8/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	piecesOpen := gOpen.Board.AppendAllPieces(nil)
	penOpen := kingSafetyPenalty(&gOpen.Board, piecesOpen, board.White, 1.0, 1.0)
	if penOpen <= 0 {
		t.Errorf("king on open e-file with enemy rook on board must have positive safety penalty, got %v", penOpen)
	}

	// Position D: Black knight on e8 instead of rook. No enemy major piece.
	// Open file penalty should be 0 because knight cannot exploit open files.
	gNoMajors, err := game.ParseFEN("4n1k1/8/8/8/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	piecesNoMajors := gNoMajors.Board.AppendAllPieces(nil)
	penNoMajors := kingSafetyPenalty(&gNoMajors.Board, piecesNoMajors, board.White, 1.0, 1.0)
	if penNoMajors != 0 {
		t.Errorf("king on open e-file with NO enemy major pieces should have 0 safety penalty, got %v", penNoMajors)
	}

	// Position E: White pawn on e2 shielding the king from the rook on e8.
	gShielded, err := game.ParseFEN("4r1k1/8/8/8/8/8/4P3/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	piecesShielded := gShielded.Board.AppendAllPieces(nil)
	penShielded := kingSafetyPenalty(&gShielded.Board, piecesShielded, board.White, 1.0, 1.0)
	if penShielded >= penOpen {
		t.Errorf("shielded king penalty (%v) must be strictly less than open-file penalty (%v)", penShielded, penOpen)
	}
}

func TestRootCheckExtension(t *testing.T) {
	// Position where White has a check: Qh5+ or similar.
	// FEN: r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3
	g, err := game.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3")
	if err != nil {
		t.Fatal(err)
	}
	pWith := Strong(2)
	pWith.Extensions = true
	pWithout := Strong(2)
	pWithout.Extensions = false

	mWith, _, okWith := pWith.pickScored(g, nil)
	mWithout, _, okWithout := pWithout.pickScored(g, nil)
	if !okWith || !okWithout {
		t.Fatalf("players failed to return moves: with=%v, without=%v", okWith, okWithout)
	}
	_ = mWith
	_ = mWithout
}

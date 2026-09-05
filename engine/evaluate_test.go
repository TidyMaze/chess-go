package engine

import (
	"chess/board"
	"testing"
)

func TestEqualMaterialScoresZero(t *testing.T) {
	b := board.Initial()
	if got := MaterialScore(&b, board.White, nil); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestExtraQueenScoresPositive(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{3, 3}, board.Piece{board.White, board.Queen})
	if MaterialScore(&b, board.White, nil) <= 0 {
		t.Errorf("expected positive score for white")
	}
	if MaterialScore(&b, board.Black, nil) >= 0 {
		t.Errorf("expected negative score for black")
	}
}

func TestCentralizedKnightScoresHigherThanCorner(t *testing.T) {
	center := board.NewEmpty()
	center.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	center.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	center.Place(board.Sq{3, 3}, board.Piece{board.White, board.Knight})

	corner := board.NewEmpty()
	corner.Place(board.Sq{0, 0}, board.Piece{board.White, board.King})
	corner.Place(board.Sq{7, 7}, board.Piece{board.Black, board.King})
	corner.Place(board.Sq{0, 7}, board.Piece{board.White, board.Knight})

	if PositionScore(&center, board.White, nil) <= PositionScore(&corner, board.White, nil) {
		t.Errorf("expected centralized knight to score higher")
	}
}

func TestKingDrivingBonusOnlyWithDecisiveMaterial(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{5, 5}, board.Piece{board.White, board.King})
	b.Place(board.Sq{0, 0}, board.Piece{board.Black, board.King})
	b.Place(board.Sq{3, 3}, board.Piece{board.White, board.Rook})
	if PositionScore(&b, board.White, nil) <= MaterialScore(&b, board.White, nil) {
		t.Errorf("expected king-driving bonus with a decisive rook advantage")
	}
}

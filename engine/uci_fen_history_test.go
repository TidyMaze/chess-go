package engine

import (
	"strings"
	"testing"
)

// shuffleFEN is White a queen, a rook and a knight up, White to move. The
// knight goes b1-c3-b1 and the black king h8-g8-h8 twice, which leaves
// Black to move with Kg8-h8 recreating this exact position a third time:
// the one move that saves the game.
const shuffleFEN = "7k/5ppp/8/8/8/8/R7/1NKQ4 w - - 0 1"

var shuffleMoves = []string{"b1c3", "h8g8", "c3b1", "g8h8", "b1c3", "h8g8", "c3b1"}

// cutechess and python-chess send "position fen <start> moves ...". The
// game built from that has to remember the positions the moves went
// through, as the startpos path does, or the search cannot see the
// threefold that saves the losing side.
func TestUCIPositionFromFENKeepsTheHistory(t *testing.T) {
	g := uciPosition(strings.Fields("fen " + shuffleFEN + " moves " + strings.Join(shuffleMoves, " ")))
	if n := len(g.PlayedBoards()); !g.TrackRepetition || n != len(shuffleMoves)+1 {
		t.Errorf("TrackRepetition=%v with %d played positions, want true and %d", g.TrackRepetition, n, len(shuffleMoves)+1)
	}
	m, ok := PlayerPick(repetitionPlayer(6), g)
	if !ok {
		t.Fatal("no move")
	}
	if got := m.UCI(); got != "g8h8" {
		t.Errorf("the losing side played %s, want g8h8 (threefold repetition, a draw)", got)
	}
}

// The same shuffle, but the first occurrence comes right after a7-a5, when
// the board holds an a6 en passant square no white pawn can use. FIDE
// counts it as the same position, so Kg8-h8 is still the third one.
func TestSearchCountsTheOccurrenceAfterAnUnusableDoublePush(t *testing.T) {
	g := uciPosition(strings.Fields("fen 7k/p4ppp/8/8/8/8/R7/1NKQ4 b - - 0 1 moves a7a5 " + strings.Join(shuffleMoves, " ")))
	m, ok := PlayerPick(repetitionPlayer(6), g)
	if !ok {
		t.Fatal("no move")
	}
	if got := m.UCI(); got != "g8h8" {
		t.Errorf("the losing side played %s, want g8h8 (threefold repetition, a draw)", got)
	}
}

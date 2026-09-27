package moves

import (
	"math/rand"
	"testing"

	"chess/board"
)

// walkLine steps from a towards b one square at a time along a rank, file
// or diagonal. It returns the squares strictly between them and the whole
// line through both, edge to edge, or two zeros when they are not aligned.
func walkLine(a, b int) (between, line uint64) {
	if a == b {
		return 0, 0
	}
	fa, ra, fb, rb := a%8, a/8, b%8, b/8
	df, dr := sign(fb-fa), sign(rb-ra)
	if fa != fb && ra != rb && abs(fb-fa) != abs(rb-ra) {
		return 0, 0
	}
	for f, r := fa+df, ra+dr; f != fb || r != rb; f, r = f+df, r+dr {
		between |= 1 << (r*8 + f)
	}
	for _, s := range [2]int{1, -1} {
		for f, r := fa, ra; f >= 0 && f < 8 && r >= 0 && r < 8; f, r = f+s*df, r+s*dr {
			line |= 1 << (r*8 + f)
		}
	}
	return between, line
}

func sign(x int) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestBetweenAndLineMatchTheWalk(t *testing.T) {
	for a := 0; a < 64; a++ {
		for b := 0; b < 64; b++ {
			wantBetween, wantLine := walkLine(a, b)
			if got := Between(uint8(a), uint8(b)); got != wantBetween {
				t.Fatalf("Between(%d, %d) = %#x, want %#x", a, b, got, wantBetween)
			}
			if got := Line(uint8(a), uint8(b)); got != wantLine {
				t.Fatalf("Line(%d, %d) = %#x, want %#x", a, b, got, wantLine)
			}
		}
	}
	// a1 to h8: the long diagonal, six squares between.
	if got := Between(0, 63); got != 0x0040201008040200 {
		t.Errorf("Between(a1, h8) = %#x", got)
	}
	// e1 and e4 share the e-file.
	if got := Line(4, 28); got != board.FileMask[4] {
		t.Errorf("Line(e1, e4) = %#x, want the e-file", got)
	}
	// A knight's step is on no line.
	if Between(0, 10) != 0 || Line(0, 10) != 0 {
		t.Error("a1 and c2 are not aligned")
	}
}

// The bitboard pin finder replaced the eight cell walks. PinnedSet is a
// set, so the answer has to be the same set on any board, pins or not.
func TestPinnedSquaresMatchesTheWalkOnRandomBoards(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	pinsSeen, multi := 0, 0
	for trial := 0; trial < 20000; trial++ {
		b := sliderHeavyBoard(rng)
		for _, c := range []board.Color{board.White, board.Black} {
			got := PinnedSquares(&b, c)
			want := PinnedSquaresUnfiltered(&b, c)
			if got != want {
				t.Fatalf("trial %d, %v: PinnedSquares %#x, walk %#x", trial, c, uint64(got), uint64(want))
			}
			if want != 0 {
				pinsSeen++
			}
			if want.Len() > 1 {
				multi++
			}
		}
	}
	if pinsSeen < 1000 || multi < 100 {
		t.Fatalf("only %d boards with a pin, %d with several", pinsSeen, multi)
	}
	t.Logf("%d boards with a pin, %d with several", pinsSeen, multi)
}

// AttackedWith is IsAttackedBy against an occupancy the caller chooses. On
// the board's own occupancy it must agree with IsAttackedBy everywhere.
func TestAttackedWithOnTheBoardOccupancyIsIsAttackedBy(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	for trial := 0; trial < 3000; trial++ {
		b := sliderHeavyBoard(rng)
		occ := b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)
		for i := 0; i < 64; i++ {
			sq := board.Sq{File: int8(i % 8), Rank: int8(i / 8)}
			for _, by := range []board.Color{board.White, board.Black} {
				if got, want := AttackedWith(&b, uint8(i), by, occ), b.IsAttackedBy(sq, by); got != want {
					t.Fatalf("trial %d, %v on %v: AttackedWith %v, IsAttackedBy %v", trial, by, sq, got, want)
				}
			}
		}
	}
}

// With the king lifted off its square, AttackedWith on the destination
// answers whether a king step leaves the king in check, which is what
// make, IsInCheck and unmake answer. Lifting the king is what catches a
// step back along the checking line.
func TestAttackedWithKingLiftedMatchesMakeUnmake(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	compared, illegal := 0, 0
	for trial := 0; trial < 20000; trial++ {
		b := sliderHeavyBoard(rng)
		for _, c := range []board.Color{board.White, board.Black} {
			king := b.KingSquare(c)
			k := uint8(king.Rank*8 + king.File)
			occ := b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)
			for _, to := range b.AppendKingMoves(nil, king, c) {
				undo := b.MakeMove(king, to)
				want := b.IsInCheck(c)
				b.UnmakeMove(undo)
				got := AttackedWith(&b, uint8(to.Rank*8+to.File), c.Other(), occ&^(1<<k))
				if got != want {
					t.Fatalf("trial %d, %v king %v to %v: AttackedWith %v, make/unmake %v", trial, c, king, to, got, want)
				}
				compared++
				if want {
					illegal++
				}
			}
		}
	}
	if illegal < 1000 {
		t.Fatalf("only %d of %d king steps were into check", illegal, compared)
	}
	t.Logf("%d king steps, %d into check", compared, illegal)
}

func BenchmarkPinnedSquaresWalk(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	boards := make([]board.Board, 256)
	for i := range boards {
		boards[i] = sliderHeavyBoard(rng)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PinnedSquaresUnfiltered(&boards[i&255], board.White)
	}
}

func BenchmarkPinnedSquaresXRay(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	boards := make([]board.Board, 256)
	for i := range boards {
		boards[i] = sliderHeavyBoard(rng)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PinnedSquares(&boards[i&255], board.White)
	}
}

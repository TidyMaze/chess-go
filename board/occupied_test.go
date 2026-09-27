package board

import (
	"math/rand"
	"testing"
)

// occupiedPosLinear is the scan Move and Remove used before occupiedPos:
// walk the live prefix of the list until the square turns up.
func occupiedPosLinear(occ *[32]uint8, count int, sq uint8) int {
	for i := 0; i < count; i++ {
		if occ[i] == sq {
			return i
		}
	}
	return -1
}

// The list holds distinct squares in its live prefix and stale bytes after
// it (swap-remove leaves the old last entry behind), so the stale tail is
// filled with random squares that may repeat live ones. The searched square
// is always live, which is the only way Move and Remove call it.
func TestOccupiedPosMatchesTheLinearScan(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for trial := 0; trial < 20000; trial++ {
		var occ [32]uint8
		count := 1 + rng.Intn(32)
		perm := rng.Perm(64)
		for i := 0; i < count; i++ {
			occ[i] = uint8(perm[i])
		}
		for i := count; i < 32; i++ {
			occ[i] = uint8(rng.Intn(64))
		}
		for i := 0; i < count; i++ {
			sq := occ[i]
			if got, want := occupiedPos(&occ, sq), occupiedPosLinear(&occ, count, sq); got != want {
				t.Fatalf("occupiedPos(%v, %d) = %d, want %d", occ, sq, got, want)
			}
		}
	}
}

// Square 0 and square 1 are the byte values the zero-byte test is most
// likely to confuse, since a borrow out of a zero byte turns a following
// 0x01 into a false hit. The lowest hit must still be the real one.
func TestOccupiedPosIgnoresTheBorrowFalsePositive(t *testing.T) {
	var occ [32]uint8
	for i := range occ {
		occ[i] = 63
	}
	occ[9], occ[10] = 1, 0
	if got := occupiedPos(&occ, 1); got != 9 {
		t.Errorf("occupiedPos(1) = %d, want 9", got)
	}
	if got := occupiedPos(&occ, 0); got != 10 {
		t.Errorf("occupiedPos(0) = %d, want 10", got)
	}
	occ[31] = 7
	if got := occupiedPos(&occ, 7); got != 31 {
		t.Errorf("occupiedPos(7) = %d, want 31", got)
	}
}

// moveByLinearScan is Board.Move as it was before occupiedPos, the oracle
// for the occupied list: its order, its count and its stale tail.
func moveByLinearScan(b *Board, from, to Sq) {
	fromIdx, toIdx := index(from), index(to)
	moved := b.cells[fromIdx]
	captured := b.cells[toIdx] >= codePieceMin
	if captured {
		cap := decodePiece(b.cells[toIdx])
		capBit := uint64(1) << squareIndex(to)
		b.pieces[cap.Color][cap.Type] &^= capBit
		b.colorBB[cap.Color] &^= capBit
	}
	b.cells[fromIdx] = codeEmpty
	b.cells[toIdx] = moved
	if moved >= codePieceMin {
		p := decodePiece(moved)
		if p.Type == King {
			b.kings[p.Color] = to
		}
		fromBit := uint64(1) << squareIndex(from)
		toBit := uint64(1) << squareIndex(to)
		b.pieces[p.Color][p.Type] = (b.pieces[p.Color][p.Type] &^ fromBit) | toBit
		b.colorBB[p.Color] = (b.colorBB[p.Color] &^ fromBit) | toBit
	}
	if captured {
		toIdx := squareIndex(to)
		for i := 0; i < b.occupiedCount; i++ {
			if b.occupied[i] == toIdx {
				b.occupied[i] = b.occupied[b.occupiedCount-1]
				b.occupiedCount--
				break
			}
		}
	}
	fromIdxSq := squareIndex(from)
	for i := 0; i < b.occupiedCount; i++ {
		if b.occupied[i] == fromIdxSq {
			b.occupied[i] = squareIndex(to)
			break
		}
	}
}

// Any from and to, including an empty from, from == to, captures of the
// last list entry and moves of it, must leave exactly the board the linear
// scans left. The pair is drawn from occupied squares most of the time, so
// captures and last-entry cases come up often.
func TestMoveMatchesTheLinearScan(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	pick := func(b *Board) Sq {
		switch rng.Intn(4) {
		case 0:
			return squareFromIndex(uint8(rng.Intn(64)))
		case 1:
			return squareFromIndex(b.occupied[b.occupiedCount-1])
		}
		return squareFromIndex(b.occupied[rng.Intn(b.occupiedCount)])
	}
	for trial := 0; trial < 3000; trial++ {
		b := crowdedBoard(rng)
		for k := 0; k < 20 && b.occupiedCount > 1; k++ {
			from, to := pick(b), pick(b)
			if trial%50 == 0 {
				to = from
			}
			want := *b
			moveByLinearScan(&want, from, to)
			b.Move(from, to)
			if *b != want {
				t.Fatalf("Move(%v, %v): occupied %v (%d), want %v (%d)",
					from, to, b.occupied, b.occupiedCount, want.occupied, want.occupiedCount)
			}
		}
	}
}

func TestOccupiedPosReportsAMissingSquare(t *testing.T) {
	var occ [32]uint8
	for i := range occ {
		occ[i] = 63
	}
	if got := occupiedPos(&occ, 5); got != -1 {
		t.Errorf("occupiedPos of an absent square = %d, want -1", got)
	}
}

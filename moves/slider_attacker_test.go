package moves

import (
	"math/rand"
	"testing"

	"chess/board"
)

// rayWalkAttacker is the square-by-square ray walk AttackerOfType used for
// sliders before the bitboards: directions in queenDirs order, the first man
// met on a ray being the only one that attacks. It is the oracle, because
// static exchange evaluation takes whichever attacker comes back first and
// the search tree follows those values.
func rayWalkAttacker(b *board.Board, sq board.Sq, by board.Color, typ board.PieceType) (board.Sq, bool) {
	dirs := queenDirs[:]
	if typ == board.Bishop {
		dirs = bishopDirs[:]
	} else if typ == board.Rook {
		dirs = rookDirs[:]
	}
	for _, d := range dirs {
		from := sq
		for {
			from = board.Sq{File: from.File + int8(d[0]), Rank: from.Rank + int8(d[1])}
			if b.CellOffBoard(from) {
				break
			}
			if p, ok := b.CellPiece(from); ok {
				if p.Color == by && p.Type == typ {
					return from, true
				}
				break
			}
		}
	}
	return board.Sq{}, false
}

// sliderHeavyBoard scatters up to 30 men plus both kings, weighted towards
// sliders so that two pieces of one type often attack the same square, the
// case where the scan order decides the answer. A few squares are then
// emptied again, as static exchange evaluation does to its copy.
func sliderHeavyBoard(rng *rand.Rand) board.Board {
	b := board.NewEmpty()
	wk := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
	bk := wk
	for bk == wk {
		bk = board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
	}
	b.Place(wk, board.Piece{Color: board.White, Type: board.King})
	b.Place(bk, board.Piece{Color: board.Black, Type: board.King})
	types := []board.PieceType{board.Pawn, board.Knight, board.Bishop, board.Bishop, board.Rook, board.Rook, board.Queen, board.Queen}
	for n := rng.Intn(31); n > 0; n-- {
		sq := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
		if sq == wk || sq == bk {
			continue
		}
		b.Place(sq, board.Piece{Color: board.Color(rng.Intn(2)), Type: types[rng.Intn(len(types))]})
	}
	for n := rng.Intn(4); n > 0; n-- {
		sq := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
		if sq != wk && sq != bk {
			b.Remove(sq)
		}
	}
	return b
}

// Over random boards, every square, both colours and every piece type,
// AttackerOfType must return exactly what the ray walk returns, including
// which of two attackers of one type it picks.
func TestSliderAttackerMatchesTheRayWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	sliders := []board.PieceType{board.Bishop, board.Rook, board.Queen}
	compared, contested := 0, 0
	for trial := 0; trial < 3000; trial++ {
		b := sliderHeavyBoard(rng)
		for i := 0; i < 64; i++ {
			sq := board.Sq{File: int8(i % 8), Rank: int8(i / 8)}
			for _, by := range []board.Color{board.White, board.Black} {
				for _, typ := range sliders {
					want, wantOK := rayWalkAttacker(&b, sq, by, typ)
					got, gotOK := AttackerOfType(&b, sq, by, typ)
					if got != want || gotOK != wantOK {
						t.Fatalf("trial %d, %v %v on %v: bitboards %v %v, ray walk %v %v",
							trial, by, typ, sq, got, gotOK, want, wantOK)
					}
					compared++
					if wantOK && attackersOfType(&b, sq, by, typ) > 1 {
						contested++
					}
				}
			}
		}
	}
	// A comparison that never meets two attackers of one type proves
	// nothing about the order.
	t.Logf("%d comparisons, %d with two or more attackers of one type", compared, contested)
	if contested < 1000 {
		t.Fatalf("only %d of %d comparisons had two attackers of one type", contested, compared)
	}
}

// attackersOfType counts the pieces of one type and colour that attack sq,
// by the ray walk, one direction at a time.
func attackersOfType(b *board.Board, sq board.Sq, by board.Color, typ board.PieceType) int {
	dirs := queenDirs[:]
	if typ == board.Bishop {
		dirs = bishopDirs[:]
	} else if typ == board.Rook {
		dirs = rookDirs[:]
	}
	n := 0
	for _, d := range dirs {
		from := sq
		for {
			from = board.Sq{File: from.File + int8(d[0]), Rank: from.Rank + int8(d[1])}
			if b.CellOffBoard(from) {
				break
			}
			if p, ok := b.CellPiece(from); ok {
				if p.Color == by && p.Type == typ {
					n++
				}
				break
			}
		}
	}
	return n
}

// Two rooks attack e4, one from h4 (east) and one from e8 (north): the ray
// walk tries east before north, so h4 is the answer. Two queens, on a8
// (north-west) and h4 (east): diagonals come before files and ranks, so a8.
func TestSliderAttackerPicksTheFirstDirectionOfTwo(t *testing.T) {
	e4 := board.Sq{File: 4, Rank: 3}
	b := boardFrom(map[board.Sq]board.Piece{
		{File: 7, Rank: 3}: {Color: board.White, Type: board.Rook},
		{File: 4, Rank: 7}: {Color: board.White, Type: board.Rook},
	})
	if sq, ok := sliderAttacker(&b, e4, board.White, board.Rook); !ok || sq != (board.Sq{File: 7, Rank: 3}) {
		t.Errorf("rooks on h4 and e8: got %v %v, want h4", sq, ok)
	}
	q := boardFrom(map[board.Sq]board.Piece{
		{File: 0, Rank: 7}: {Color: board.Black, Type: board.Queen},
		{File: 7, Rank: 3}: {Color: board.Black, Type: board.Queen},
	})
	if sq, ok := sliderAttacker(&q, e4, board.Black, board.Queen); !ok || sq != (board.Sq{File: 0, Rank: 7}) {
		t.Errorf("queens on a8 and h4: got %v %v, want a8", sq, ok)
	}
}

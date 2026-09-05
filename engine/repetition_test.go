package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

func repetitionPlayer(d int) Player {
	return Player{Depth: d, UsePST: true, Quiescence: true, TTBits: 20,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true, Futility: true}
}

// The search had no repetition detection at all: it was only used as a
// tie-break between root moves that already scored equally. So a line
// that shuffled a piece back to where it started looked exactly as good
// as one that made progress, and every score along it was a fresh
// evaluation of the same position.
//
// The visible symptoms were the engine playing Be3-c1, Bc1-g5, Bg5-c1
// against Stockfish, matches running 36-40% draws, and an extra ply of
// search being worth +14 +/- 27 Elo when it should be worth 50-70: a
// deeper search with no repetition detection mostly finds longer ways to
// go round in circles.
func TestSearchAvoidsRepeatingAWonPosition(t *testing.T) {
	// White is a queen up with an easy win available. Repeating the
	// position throws away the whole advantage, so a search that
	// understands repetition must never choose it.
	b := board.NewEmpty()
	b.Place(board.Sq{4, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{3, 3}, board.Piece{board.White, board.Queen})
	b.Place(board.Sq{0, 1}, board.Piece{board.White, board.Rook})
	b.Place(board.Sq{4, 7}, board.Piece{board.Black, board.King})

	g := game.From(b, board.White)
	g.EnableRepetitionTracking()

	// Play a shuffle so the starting position is already on the record:
	// Qd4-d5-d4 would now be a repetition.
	for _, uci := range []string{"d4d5", "e8d8", "d5d4", "d8e8"} {
		m, ok := game.MoveFromUCI(uci)
		if !ok {
			t.Fatalf("bad move %q", uci)
		}
		g.ApplyMove(m.From, m.To)
	}

	// The position has now occurred twice. Repeating it a third time is a
	// draw, which from a queen and rook up is the worst legal outcome.
	before := len(g.PlayedBoards())
	if before < 4 {
		t.Fatalf("expected the game to have recorded its positions, got %d", before)
	}

	move, ok := PlayerPick(repetitionPlayer(5), g)
	if !ok {
		t.Fatal("expected a move")
	}
	g.ApplyMove(move.From, move.To)
	if g.IsThreefoldRepetition() {
		t.Errorf("engine repeated into a draw while a queen and rook up, playing %v", move)
	}
}

// A repetition inside the search line must score as a draw, not as
// whatever the position happened to evaluate to. With White a queen up,
// a line that repeats is worth 0 and a line that does not is worth a
// queen, so the search must prefer the second.
func TestRepetitionInsideSearchScoresAsDraw(t *testing.T) {
	b := board.NewEmpty()
	b.Place(board.Sq{4, 0}, board.Piece{board.White, board.King})
	b.Place(board.Sq{3, 3}, board.Piece{board.White, board.Queen})
	b.Place(board.Sq{4, 7}, board.Piece{board.Black, board.King})
	g := game.From(b, board.White)

	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true,
		Structure: true, Table: NewTranspositionTable(18)}
	ctx := &searchCtx{ev: ev, quiescence: true, extensions: true}
	key := zobristHash(g)
	ctx.path[0] = key

	// Claim the position was already reached earlier on this line, which
	// is what a repetition looks like from inside the search.
	ctx.path[1] = key
	if !ctx.isRepetition(key, 2) {
		t.Fatal("isRepetition did not spot a key already on the path")
	}
	score := ctx.search(g, board.White, board.White, 3, 2, negInf, posInf)
	if score != 0 {
		t.Errorf("a repeated position scored %.2f, want 0 (a draw)", score)
	}
}

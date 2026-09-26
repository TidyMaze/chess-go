package engine

import (
	"bytes"
	"strings"
	"testing"

	"chess/board"
)

// botAtDepth is the Lichess bot's configuration (champion_bot.json with its
// network, no book) searching one thread to a fixed depth, so a test sees
// the same engine that played the game and gets the same answer every run.
func botAtDepth(t *testing.T, depth int) Player {
	t.Helper()
	c := ReadChampion("../champion_bot.json")
	c.NetFile, c.Book = "../champion_net.json", ""
	p, err := c.PlayerOrError()
	if err != nil {
		t.Skip(err)
	}
	p.TimeBudget, p.Threads, p.Depth, p.TTBits = 0, 1, depth, 18
	return p
}

// Lichess lhT8MqvU, ply 190: Stockfish mates in three here. Our engine
// scored it +1012 pawns at depth 17 and pushed d6, and kept finding a
// different mate every move until the fifty-move rule drew the game. A mate
// score counted the depth left in the search, not the distance to the mate,
// so a mate in nine searched with more depth left scored higher than a mate
// in three, and nothing pulled the engine toward the short one.
//
// Scored from the root, mate in three is five plies away and worth exactly
// mateScore - 5, at any depth deep enough to see it. Before the fix depth 10
// scored it 1005, depth 16 scored it 1011.
func TestMateScoreIsTheDistanceFromTheRoot(t *testing.T) {
	p := botAtDepth(t, 10)
	g := mustFEN(t, "5k1B/R7/6p1/3PK1P1/1p6/1P6/1P6/8 w - - 81 96")
	SeedRandom(1)
	_, score, ok := PlayerPickScored(p, g)
	if want := float64(mateScore - 5); !ok || score != want {
		t.Errorf("mate in 3 scored %v (ok %v), want %v", score, ok, want)
	}
}

// And the engine has to act on it: playing both sides from the same
// position, White mates within three moves whatever Black tries. Before the
// fix, depth 10 shuffled for eight moves without mating, and depths 13 and 14
// took five and six moves.
func TestEngineMatesInThreeFromTheLichessPosition(t *testing.T) {
	p := botAtDepth(t, 10)
	g := mustFEN(t, "5k1B/R7/6p1/3PK1P1/1p6/1P6/1P6/8 w - - 81 96")
	SeedRandom(1)
	var played []string
	for move := 1; move <= 3; move++ {
		m, ok := PlayerPick(p, g)
		if !ok {
			t.Fatalf("White has no move after %v", played)
		}
		g.ApplyMove(m.From, m.To)
		played = append(played, m.UCI())
		if g.IsCheckmate(g.Turn) {
			return
		}
		r, ok := PlayerPick(p, g)
		if !ok {
			t.Fatalf("Black has no move and is not mated after %v", played)
		}
		g.ApplyMove(r.From, r.To)
		played = append(played, r.UCI())
	}
	t.Errorf("no mate after three White moves: %v, position %s", played, g.FEN())
}

// The table is shared by every ply of the search, so a mate score has to be
// stored as a distance from the node that found it and turned back into a
// distance from the root wherever it is probed. Stored raw, a position
// reached again two plies deeper claims its mate two plies too soon, and
// the engine prefers a line whose mate is further away than it thinks.
func TestTableKeepsAMateDistanceAcrossPlies(t *testing.T) {
	tt := NewTranspositionTable(10)
	const key = 0x0123456789abcdef
	// Found 3 plies from the root, mate 7 plies from the root: 4 from here.
	tt.store(key, mateScore-7, 5, 3, ttExact, board.White)
	// Reached 5 plies from the root, the same position is still mated 4
	// plies later: 9 plies from the root.
	if got, ok := tt.probe(key, 5, 5, 0, board.White, negInf, posInf); !ok || got != mateScore-9 {
		t.Errorf("winning mate probed at ply 5: %v (ok %v), want %v", got, ok, mateScore-9)
	}
	// Reached at ply 1, the mate is 5 plies from the root.
	if got, _, _, _ := tt.probeWithMove(key, 5, 1, 0, board.White, negInf, posInf); got != mateScore-5 {
		t.Errorf("winning mate probed at ply 1: %v, want %v", got, mateScore-5)
	}

	// Being mated works the same way with the sign reversed: mated 6 plies
	// from the root, stored at ply 2, reached again at ply 4.
	const losing = 0x0fedcba987654321
	tt.store(losing, -(mateScore - 6), 5, 2, ttExact, board.White)
	if got, ok := tt.probe(losing, 5, 4, 0, board.White, negInf, posInf); !ok || got != -(mateScore-8) {
		t.Errorf("losing mate probed at ply 4: %v (ok %v), want %v", got, ok, -(mateScore - 8))
	}

	// A bound is adjusted before it is compared with the window: a lower
	// bound of mate in 9 from the root, reached at ply 1, is mate in 5 from
	// the root and must still cut against a beta of mateScore - 6.
	const bound = 0x1111222233334444
	tt.store(bound, mateScore-9, 5, 5, ttLowerBound, board.White)
	if got, ok := tt.probe(bound, 5, 1, 0, board.White, 0, mateScore-6); !ok || got != mateScore-5 {
		t.Errorf("lower bound probed at ply 1: %v (ok %v), want a cutoff at %v", got, ok, mateScore-5)
	}

	// Ordinary scores are not distances and come back untouched.
	const plain = 0x5555666677778888
	tt.store(plain, 2.5, 5, 3, ttExact, board.White)
	if got, _ := tt.probe(plain, 5, 7, 0, board.White, negInf, posInf); got != 2.5 {
		t.Errorf("a plain score moved in the table: %v, want 2.5", got)
	}
}

// A GUI, the harness and the Lichess bot's logs read a mate score as
// "score mate N" in moves, not as a centipawn count near 100000.
func TestUCIReportsAMateScoreInMoves(t *testing.T) {
	script := strings.Join([]string{
		"position fen 6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1",
		"go depth 3",
		"quit",
	}, "\n")
	var out bytes.Buffer
	p := Strong(3)
	p.TTBits = 16
	ServeUCI(strings.NewReader(script), &out, p)
	if !strings.Contains(out.String(), " score mate 1 ") {
		t.Errorf("mate in one not reported as score mate 1:\n%s", out.String())
	}
}

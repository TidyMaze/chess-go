package engine

import (
	"os"
	"testing"

	"chess/board"
	"chess/game"
)

func TestAdvancedPawnPushCountsFromTheMoversSide(t *testing.T) {
	cases := []struct {
		name  string
		fen   string
		move  string
		mover board.Color
		want  bool
	}{
		{"White pawn to the sixth", "4k3/8/8/4P3/8/8/8/4K3 w - - 0 1", "e5e6", board.White, true},
		{"White pawn to the fifth", "4k3/8/8/8/4P3/8/8/4K3 w - - 0 1", "e4e5", board.White, false},
		{"Black pawn to its sixth", "4k3/8/8/4p3/8/8/8/4K3 b - - 0 1", "e5e4", board.Black, false},
		{"Black pawn to its seventh", "4k3/8/8/8/8/4p3/8/4K3 b - - 0 1", "e3e2", board.Black, true},
		{"a king walking forward is not a pawn push", "4k3/8/8/8/8/8/4K3/8 w - - 0 1", "e2e3", board.White, false},
	}
	for _, c := range cases {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		m, ok := game.MoveFromUCI(c.move)
		if !ok {
			t.Fatalf("%s: bad move %q", c.name, c.move)
		}
		if got := advancedPawnPush(&g.Board, m, c.mover); got != c.want {
			t.Errorf("%s: advancedPawnPush(%s) = %v, want %v", c.name, c.move, got, c.want)
		}
	}
}

// Off by default and reaching the search, so a race measures something.
func TestPawnPushFlagReachesTheSearch(t *testing.T) {
	p := Strong(4)
	if p.PawnPush {
		t.Fatal("pawnpush is on by default, so no race can measure it")
	}
	p.ApplyFeatures("pawnpush")
	if !p.PawnPush {
		t.Fatal(`ApplyFeatures("pawnpush") left the flag off`)
	}
	if !evalForPlayer(p).PawnPush {
		t.Error("the player carries pawnpush but the evaluation the search reads does not")
	}
}

// Exempting a move from reduction and pruning means searching more of it,
// so the node count has to rise where advanced pawn pushes exist at all.
//
// It rises by almost nothing: 64,454 nodes against 64,438 over four
// pawn-heavy positions, 0.02%. Move ordering already puts an advanced
// push early enough that reduction and pruning seldom reach it, so the
// technique that looked like the answer to the pawn gap is inert here.
// The test asserts only that the flag reaches the search, which is what
// stops someone implementing this a second time.
func TestPawnPushSearchesAdvancedPushesHarder(t *testing.T) {
	fens := []string{
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"8/1P6/2k5/8/8/5K2/6p1/8 w - - 0 1",
		"4k3/pp4P1/8/8/8/8/1P4pp/4K3 w - - 0 1",
		"r3k2r/pp1P1ppp/8/8/8/8/PP1p1PPP/R3K2R w KQkq - 0 1",
	}
	nodes := func(on bool) int {
		total := 0
		for _, fen := range fens {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatalf("ParseFEN(%q): %v", fen, err)
			}
			p := Strong(8)
			// The champion's own feature set: without late move pruning
			// and scaled reductions there is nothing for an exemption to
			// exempt, and the first version of this test measured a
			// search that never reduced these moves at all.
			p.ApplyFeatures("lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate,iir,improving,razoring")
			if on {
				p.ApplyFeatures("pawnpush")
			}
			ev := evalForPlayer(p)
			ev.Table = NewTranspositionTable(20)
			ChooseMoveIterative(g, g.Turn, 8, ev, true)
			total += LastSearchNodesValue()
		}
		return total
	}
	plain, exempt := nodes(false), nodes(true)
	if exempt == plain {
		t.Errorf("exempting advanced pushes searched %d nodes against %d plain, so the flag is not reaching the search",
			exempt, plain)
	}
	t.Logf("nodes %d plain, %d with advanced pushes exempt (%.2f%% change in nodes)", plain, exempt, 100*float64(exempt-plain)/float64(plain))
}

func TestEndgamePassedPawnBlockade(t *testing.T) {
	// FEN from lichess game nkVWv4cy turn 99: White to move.
	// Black has dangerous passed pawns on a3 and c4 with Rd3.
	// White must NOT abandon the blockade (e.g. Ke6, which was played in the game).
	// The drawing move is Qa1 (e1a1) blockading the a3 pawn.
	fen := "8/8/8/1p2K3/2p5/p2r4/2k5/4Q3 w - - 10 99"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chdir("..")
	defer func() { _ = os.Chdir("engine") }()
	champ := ReadChampion("champion.json")
	p, err := champ.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	p.ApplyFeatures("pawnpush")
	q := p
	q.Depth = 13
	q.TimeBudget = 0
	q.Threads = 1
	tt := NewTranspositionTable(20)
	m, score, ok := q.pickScored(g, tt)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("chosen move: %s (score %f, nodes %d)", m.UCI(), score, TotalNodes())
	if m.UCI() == "e5e6" {
		t.Errorf("blundered e5e6 (game blunder), abandoning blockade against advanced passed pawns; want e1a1")
	}
	if m.UCI() != "e1a1" {
		t.Errorf("expected blockade move e1a1 (Qa1), got %s", m.UCI())
	}
}

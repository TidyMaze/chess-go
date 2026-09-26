package engine

import (
	"fmt"
	"testing"

	"chess/board"
)

// fenOf is the FEN of the pieces on their squares ("e4": 'N'), Black to move.
func fenOf(pieces map[string]byte) string {
	out := ""
	for rank := '8'; rank >= '1'; rank-- {
		empty := 0
		for file := 'a'; file <= 'h'; file++ {
			p, ok := pieces[string(file)+string(rank)]
			if !ok {
				empty++
				continue
			}
			if empty > 0 {
				out += fmt.Sprint(empty)
				empty = 0
			}
			out += string(p)
		}
		if empty > 0 {
			out += fmt.Sprint(empty)
		}
		if rank > '1' {
			out += "/"
		}
	}
	return out + " b - - 0 1"
}

// botScorer returns the bot configuration's static score for White.
func botScorer(t *testing.T) func(fen string) float64 {
	t.Helper()
	ev := evalForPlayer(botAtDepth(t, 1))
	return func(fen string) float64 { return PositionScoreEval(&mustFEN(t, fen).Board, board.White, ev) }
}

// Bishop and knight mate only in a corner of the bishop's colour, two
// bishops in any corner. kingDrivingBonus measured the lone king's distance
// from the centre with a Chebyshev distance, flat along a whole edge, and
// the net scored the right corner below the middle of the edge, so nothing
// pointed at the corner the mate needs.
func TestLoneKingIsDrivenToTheMatingCorner(t *testing.T) {
	// White Kd3 and dark bishop f4, with a knight on e4 or a light bishop
	// on c4: none of them attacks a square the lone king is put on.
	botScore := botScorer(t)
	knight := func(king string) float64 {
		return botScore(fenOf(map[string]byte{"d3": 'K', "e4": 'N', "f4": 'B', king: 'k'}))
	}
	bishops := func(king string) float64 {
		return botScore(fenOf(map[string]byte{"d3": 'K', "c4": 'B', "f4": 'B', king: 'k'}))
	}

	// Bishop and knight, dark bishop: a1 and h8 are the mating corners.
	for _, right := range []string{"a1", "h8"} {
		for _, other := range []string{"a4", "d8", "h5", "e1", "a8", "h1"} {
			if r, o := knight(right), knight(other); r <= o {
				t.Errorf("bishop and knight: lone king on %s (mating corner) scores %.2f, on %s %.2f", right, r, other, o)
			}
		}
	}
	for _, wrong := range []string{"a8", "h1"} {
		for _, edge := range []string{"a4", "d8", "h5", "e1"} {
			if w, e := knight(wrong), knight(edge); w >= e {
				t.Errorf("bishop and knight: lone king on %s (wrong corner) scores %.2f, not below %s at %.2f", wrong, w, edge, e)
			}
		}
	}
	// Two bishops: every corner beats the middle of every edge.
	for _, corner := range []string{"a1", "h8", "a8", "h1"} {
		for _, edge := range []string{"a4", "d8", "h5", "e1"} {
			if c, e := bishops(corner), bishops(edge); c <= e {
				t.Errorf("two bishops: lone king on %s (corner) scores %.2f, on %s %.2f", corner, c, edge, e)
			}
		}
	}
	// And the strong king has to come and help.
	near := botScore("7k/8/5K2/8/4NB2/8/8/8 b - - 0 1")
	far := botScore("7k/8/8/8/4NB2/2K5/8/8 b - - 0 1")
	if near <= far {
		t.Errorf("bishop and knight, lone king on h8: strong king on f6 scores %.2f, on c3 %.2f", near, far)
	}
}

// The mop-up replaces the king drive against a bare king whenever the
// strong side has bishop and knight or bishops of both colours, whatever
// else it has; queen and rook mates, material that cannot mate, and
// anything a defender still has keep the old drive.
func TestMopUpWheneverBishopAndKnightOrBothBishopsFaceABareKing(t *testing.T) {
	for _, c := range []struct {
		fen  string
		want bool
	}{
		{"8/8/8/3k4/8/8/8/KBN5 w - - 0 1", true},
		{"8/8/8/3k4/8/8/8/KBB5 w - - 0 1", true},
		{"8/8/8/3k4/8/8/8/KBBN4 w - - 0 1", true},
		{"8/8/8/3k4/8/8/P7/KBN5 w - - 0 1", true},
		{"8/8/8/3k4/8/8/8/KBNQ4 w - - 0 1", true},
		{"8/8/8/3k4/8/8/8/KBBR4 w - - 0 1", true},
		{"8/8/8/3k4/8/8/8/KR6 w - - 0 1", false},    // rook mate keeps its drive
		{"8/8/8/3k4/8/8/8/KQ6 w - - 0 1", false},    // so does the queen's
		{"8/8/8/3k4/8/8/8/KRB5 w - - 0 1", false},   // and a rook with one bishop
		{"8/8/8/3k4/8/8/8/KB6 w - - 0 1", false},    // one bishop cannot mate
		{"8/8/8/3k4/8/8/8/KNN5 w - - 0 1", false},   // nor two knights
		{"8/8/8/3k4/8/8/8/KB1B4 w - - 0 1", false},  // nor bishops of one colour
		{"8/8/8/3k4/8/8/P7/KB6 w - - 0 1", false},   // nor one bishop and a pawn
		{"8/8/8/3k4/8/3p4/8/KBN5 w - - 0 1", false}, // the defender still has a pawn
		{"8/8/8/3k4/8/3n4/8/KBB5 w - - 0 1", false}, // or a piece
	} {
		if _, ok := mopUp(&mustFEN(t, c.fen).Board, board.White); ok != c.want {
			t.Errorf("%s: mop-up %v, want %v", c.fen, ok, c.want)
		}
	}
}

// botPlaysOut plays the bot configuration against itself from fen at a
// fixed depth and says how the game ended. Every move gets a fresh table,
// so the game measures what the evaluation steers toward: with one table
// for the whole game, as the lichess bot keeps, scores stored by earlier
// moves came back and the same code mated none of four bishop-and-knight
// games even against a one-ply defender.
func botPlaysOut(t *testing.T, fen string, depth, maxPlies int) (string, int) {
	t.Helper()
	p := botAtDepth(t, depth)
	g := mustFEN(t, fen)
	g.EnableRepetitionTracking()
	SeedRandom(1)
	for ply := 0; ; ply++ {
		switch {
		case g.IsCheckmate(g.Turn):
			return "mate", ply
		case g.IsStalemate(g.Turn):
			return "stalemate", ply
		case g.IsFiftyMoveDraw():
			return "fifty", ply
		case g.IsThreefoldRepetition():
			return "threefold", ply
		case ply == maxPlies:
			return "cap", ply
		}
		m, ok := PlayerPickWith(p, g, NewTranspositionTable(18))
		if !ok {
			return "no move", ply
		}
		g.ApplyMove(m.From, m.To)
	}
}

// The bot (net only, no tablebase) drew every bishop-and-knight and
// two-bishop ending of the bug hunt, by threefold or the fifty-move rule,
// and the lhT8MqvU suite's KBNK from clock 0 against Stockfish too. Here it
// plays both sides at depth 8, so the lone king defends as well as the
// attacker attacks.
func TestBotMatesWithBishopAndKnightOrTwoBishops(t *testing.T) {
	for _, c := range []struct{ name, fen string }{
		{"KBNK", "8/8/8/3k4/8/8/8/KBN5 w - - 0 1"},
		{"KBNK", "8/8/8/4k3/8/8/8/2BNK3 w - - 0 1"},
		{"KBBK", "8/8/8/3k4/8/8/8/KBB5 w - - 0 1"},
		{"KBBK", "8/8/8/4k3/8/8/8/2BBK3 w - - 0 1"},
	} {
		if end, plies := botPlaysOut(t, c.fen, 8, 100); end != "mate" || plies%2 == 0 {
			t.Errorf("%s %s: %s after %d plies, want the lone king mated within the fifty moves", c.name, c.fen, end, plies)
		}
	}
}

// The mop-up adds up to 16.8 pawns on top of the net. Given only to bishop
// and knight or two bishops, it made bishop and knight against a bare king
// score 23.5 where the same position with an extra queen scored 11.3 and
// with an extra pawn 9.7, so keeping the material was worth less than
// handing it to the lone king. Wherever the lone king stands, one more pawn
// or a queen must score more.
func TestMopUpNeverPaysForMaterial(t *testing.T) {
	botScore := botScorer(t)
	near := func(a, b string) bool {
		df, dr := int(a[0])-int(b[0]), int(a[1])-int(b[1])
		return df >= -1 && df <= 1 && dr >= -1 && dr <= 1
	}
	for _, set := range []struct {
		name  string
		white map[string]byte
	}{
		{"bishop and knight", map[string]byte{"e4": 'K', "d3": 'B', "e5": 'N'}},
		{"two bishops", map[string]byte{"e4": 'K', "d3": 'B', "e3": 'B'}},
	} {
		with := func(king, sq string, piece byte) string {
			pieces := map[string]byte{king: 'k'}
			for s, p := range set.white {
				pieces[s] = p
			}
			if sq != "" {
				pieces[sq] = piece
			}
			return fenOf(pieces)
		}
		checked := 0
		for file := 'a'; file <= 'h'; file++ {
			for rank := '1'; rank <= '8'; rank++ {
				king := string(file) + string(rank)
				touches := near(king, "g3") || near(king, "a2")
				for s := range set.white {
					touches = touches || near(king, s)
				}
				if touches {
					continue
				}
				checked++
				bare := botScore(with(king, "", 0))
				if pawn := botScore(with(king, "g3", 'P')); bare >= pawn {
					t.Errorf("lone king on %s: %s score %.2f, with a pawn more %.2f", king, set.name, bare, pawn)
				}
				if queen := botScore(with(king, "a2", 'Q')); bare >= queen {
					t.Errorf("lone king on %s: %s score %.2f, with a queen more %.2f", king, set.name, bare, queen)
				}
			}
		}
		if checked < 20 {
			t.Fatalf("%s: only %d lone king squares checked", set.name, checked)
		}
	}
}

// Found in review: with the pawn on c4 hanging to the lone king, the bot
// played c5 or Ke3 at every depth and let the king take it, to reach the
// bishop-and-knight ending the mop-up overvalued. Stockfish mates in 14
// after Be6, which protects the pawn; master played Be6.
func TestBotKeepsItsPawnAgainstABareKing(t *testing.T) {
	g := mustFEN(t, "8/8/8/8/1kP2N2/7B/5K2/8 w - - 0 1")
	SeedRandom(1)
	m, ok := PlayerPickWith(botAtDepth(t, 8), g, NewTranspositionTable(18))
	if !ok {
		t.Fatal("no move")
	}
	g.ApplyMove(m.From, m.To)
	for _, reply := range g.AllLegalMoves(board.Black) {
		if p, ok := g.Board.PieceAt(reply.To); ok && p.Color == board.White && p.Type == board.Pawn {
			t.Errorf("played %v%v: the lone king takes the pawn with %v%v", m.From, m.To, reply.From, reply.To)
		}
	}
}

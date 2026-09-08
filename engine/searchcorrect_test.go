package engine

import (
	"math"
	"testing"

	"chess/board"
	"chess/game"
)

// Correctness of the search, against a reference that cannot be wrong.
//
// Two bugs were found here by accident this session: a null move that left
// the en passant square set and corrupted the caller's board, and a pooled
// search context that was never cleared and made the same position score
// differently depending on what ran before it. Both were found by chasing
// a symptom. This finds them by construction instead.
//
// The method is a plain minimax with no pruning, no table, no ordering and
// no heuristics. It is slow and obviously correct, so any disagreement is
// a bug in the fast search rather than in the reference.
//
// The distinction that matters: alpha-beta, principal variation search, a
// transposition table and move ordering are all *exact*. They may change
// how long the search takes and must never change what it returns. Null
// move, late move reductions and futility pruning are *approximations*
// that deliberately trade accuracy for speed, so they are held to a bound
// rather than to equality.

// naiveMinimax is the reference: every move, every ply, no cleverness.
// Scores are from maximizingFor's point of view, matching the fast search.
func naiveMinimax(g *game.Game, color, maximizingFor board.Color, depth int, ev *Eval) float64 {
	// Terminal before depth, in that order, because the fast search checks
	// them in that order: a mate at the horizon is a mate, not a leaf.
	moves := g.AllLegalMoves(color)
	if len(moves) == 0 {
		return terminalScore(g, color, maximizingFor, depth)
	}
	if depth == 0 {
		return evalPosition(g, maximizingFor, ev)
	}
	maximizing := color == maximizingFor
	best := math.Inf(-1)
	if !maximizing {
		best = math.Inf(1)
	}
	for _, m := range moves {
		child := *g
		child.ApplyMove(m.From, m.To)
		v := naiveMinimax(&child, color.Other(), maximizingFor, depth-1, ev)
		if maximizing {
			if v > best {
				best = v
			}
		} else if v < best {
			best = v
		}
	}
	return best
}

// exactPlayer is the fast search with every approximation switched off, so
// it must agree with the reference to the last bit.
func exactPlayer(depth int) Player {
	return Player{
		Name: "exact", Depth: depth,
		UsePST: true, Tapered: true, Structure: true, Mobility: true, KingSafety: 0.01,
		Iterative: true,
		// Off: everything that trades accuracy for speed.
		Quiescence: false, NullMove: false, Futility: false,
		Extensions: false, Aspiration: false, SEEPruning: false, NoLMR: true,
		TTBits: 0,
	}
}

// A spread of positions: openings, a sharp middlegame, endgames, positions
// with castling rights, with an en passant square, and with each side to
// move. The en passant ones are deliberate: that is where the null-move
// bug lived.
var correctnessPositions = []string{
	"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
	"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
	"rnbqkbnr/ppp1pppp/8/3p4/4P3/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 2",
	"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3",
	"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"8/8/4k3/8/8/8/8/K6R w - - 0 1",
	"4k3/8/8/8/8/8/8/4K2R b K - 0 1",
	"6k1/5ppp/8/8/8/8/5PPP/6K1 w - - 0 1",
}

// Alpha-beta with an exact configuration must return exactly what plain
// minimax returns, at every depth, for whichever side is to move.
func TestExactSearchMatchesNaiveMinimax(t *testing.T) {
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		for depth := 1; depth <= 3; depth++ {
			p := exactPlayer(depth)
			got, ok := PlayerScoreWith(p, g, nil)
			if !ok {
				continue // no legal moves
			}
			want := naiveMinimax(g, g.Turn, g.Turn, depth, evalForPlayer(p))
			if math.Abs(got-want) > 1e-9 {
				t.Errorf("%s\n  depth %d, %s to move: alpha-beta %.9f, plain minimax %.9f",
					fen, depth, sideName(g.Turn), got, want)
			}
		}
	}
}

// Each exact optimisation must be free: adding it may change the time
// taken and must not change the answer. Anything that does is unsound.
func TestExactOptimisationsDoNotChangeTheScore(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Player)
	}{
		{"transposition table", func(p *Player) { p.TTBits = 16 }},
		{"aspiration window", func(p *Player) { p.Aspiration = true }},
		// Check extensions are deliberately absent: an extension searches a
		// ply deeper, so it returns a better-informed score and is expected
		// to differ. It is bounded below with the other approximations
		// instead.
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, fen := range correctnessPositions {
				g, err := game.ParseFEN(fen)
				if err != nil {
					t.Fatal(err)
				}
				for depth := 1; depth <= 4; depth++ {
					base := exactPlayer(depth)
					with := exactPlayer(depth)
					c.mod(&with)
					want, ok1 := PlayerScoreWith(base, g, nil)
					got, ok2 := PlayerScoreWith(with, g, nil)
					if !ok1 || !ok2 {
						continue
					}
					if math.Abs(got-want) > 1e-9 {
						t.Errorf("%s\n  depth %d, %s to move: with %s %.6f, without %.6f",
							fen, depth, sideName(g.Turn), c.name, got, want)
					}
				}
			}
		})
	}
}

// The approximations are allowed to differ, but not without limit. A
// heuristic that changes the evaluation by several pawns is not pruning,
// it is guessing.
func TestApproximationsStayWithinABound(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*Player)
		bound float64
	}{
		// Bounds are the measured worst case with a little room, not a wish.
		// Null move and futility stay under a pawn on this set; reductions
		// cost more because they change the depth the opponent's replies get
		// as well.
		{"null move", func(p *Player) { p.NullMove = true }, 1.5},
		{"late move reductions", func(p *Player) { p.NoLMR = false }, 2.5},
		{"futility pruning", func(p *Player) { p.Futility = true }, 1.5},
		{"check extensions", func(p *Player) { p.Extensions = true }, 2.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			worst, worstFEN := 0.0, ""
			for _, fen := range correctnessPositions {
				g, err := game.ParseFEN(fen)
				if err != nil {
					t.Fatal(err)
				}
				for depth := 1; depth <= 4; depth++ {
					base := exactPlayer(depth)
					with := exactPlayer(depth)
					c.mod(&with)
					want, ok1 := PlayerScoreWith(base, g, nil)
					got, ok2 := PlayerScoreWith(with, g, nil)
					if !ok1 || !ok2 {
						continue
					}
					if d := math.Abs(got - want); d > worst {
						worst, worstFEN = d, fen
					}
				}
			}
			t.Logf("%s: worst deviation %.3f pawns (%s)", c.name, worst, worstFEN)
			if worst > c.bound {
				t.Errorf("%s deviates by %.3f pawns, more than the %.1f allowed: that is "+
					"not pruning, it is guessing\n  %s", c.name, worst, c.bound, worstFEN)
			}
		})
	}
}

// The score must not depend on which side is to move beyond the sign.
// Mirroring a position vertically and swapping colours must negate it.
func TestEvaluationIsColourSymmetric(t *testing.T) {
	for _, fen := range []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"6k1/5ppp/8/8/8/8/5PPP/6K1 w - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		m, err := game.ParseFEN(mirrorFEN(fen))
		if err != nil {
			t.Fatalf("mirroring %s: %v", fen, err)
		}
		ev := evalForPlayer(exactPlayer(1))
		a := PositionScoreEval(&g.Board, board.White, ev)
		b := PositionScoreEval(&m.Board, board.Black, ev)
		if math.Abs(a-b) > 1e-9 {
			t.Errorf("%s\n  White's view %.6f, mirrored Black's view %.6f: the evaluation "+
				"is not colour symmetric, so one side is being favoured by a bug", fen, a, b)
		}
	}
}

func sideName(c board.Color) string {
	if c == board.White {
		return "White"
	}
	return "Black"
}

// mirrorFEN flips a position vertically and swaps the colours, which must
// produce an exactly equivalent position for the other side.
func mirrorFEN(fen string) string {
	var placement, rest string
	for i := 0; i < len(fen); i++ {
		if fen[i] == ' ' {
			placement, rest = fen[:i], fen[i:]
			break
		}
	}
	ranks := []string{}
	start := 0
	for i := 0; i <= len(placement); i++ {
		if i == len(placement) || placement[i] == '/' {
			ranks = append(ranks, placement[start:i])
			start = i + 1
		}
	}
	// Reverse the ranks and swap the case of every piece letter.
	out := ""
	for i := len(ranks) - 1; i >= 0; i-- {
		if out != "" {
			out += "/"
		}
		for j := 0; j < len(ranks[i]); j++ {
			ch := ranks[i][j]
			switch {
			case ch >= 'a' && ch <= 'z':
				out += string(ch - 'a' + 'A')
			case ch >= 'A' && ch <= 'Z':
				out += string(ch - 'A' + 'a')
			default:
				out += string(ch)
			}
		}
	}
	// Side to move flips; castling and en passant are dropped rather than
	// mirrored, because this is only used on positions without them.
	side := " b"
	if len(rest) > 2 && rest[1] == 'b' {
		side = " w"
	}
	return out + side + " - - 0 1"
}

// What a heuristic costs in play, rather than in evaluation.
//
// A pruning heuristic earns its place by being fast enough to buy back the
// accuracy it gives up, so a deviation in the score is not itself a fault.
// What matters is whether it makes the engine choose a worse move. This
// measures exactly that: take the move each configuration plays, score that
// move with the exact search, and compare against the best move the exact
// search found.
//
// This is the shape of test that would have caught the aspiration window,
// which picked a demonstrably worse move in five positions while its score
// looked plausible.
func TestHeuristicsDoNotPickWorseMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("plays every legal move at the root")
	}
	cases := []struct {
		name string
		mod  func(*Player)
		// Worst acceptable loss, in pawns, against the exact search's choice.
		bound float64
	}{
		{"transposition table", func(p *Player) { p.TTBits = 16 }, 0},
		{"aspiration window", func(p *Player) { p.Aspiration = true }, 0},
		{"null move", func(p *Player) { p.NullMove = true }, 1.0},
		// Reductions are the one heuristic here that changes the move
		// chosen, by 1.175 pawns at worst on this set. That is what a
		// reduction is: it looks at unpromising moves shallower and
		// sometimes misses one. Whether it pays for itself is an Elo
		// question, not a correctness one.
		{"late move reductions", func(p *Player) { p.NoLMR = false }, 1.5},
		{"futility pruning", func(p *Player) { p.Futility = true }, 1.0},
		{"check extensions", func(p *Player) { p.Extensions = true }, 1.0},
		{"late move pruning", func(p *Player) { p.LMP = true }, 1.5},
		{"everything at once", func(p *Player) {
			p.TTBits, p.Aspiration, p.NullMove = 16, true, true
			p.NoLMR, p.Futility, p.Extensions = false, true, true
		}, 1.0},
	}
	const depth = 4
	// Reference values for every root move, once per position.
	exact := map[string]map[game.Move]float64{}
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		exact[fen] = exactMoveValues(t, g, depth)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			worst, worstFEN := 0.0, ""
			for _, fen := range correctnessPositions {
				g, err := game.ParseFEN(fen)
				if err != nil {
					t.Fatal(err)
				}
				with := exactPlayer(depth)
				c.mod(&with)
				picked, ok := PlayerPick(with, g)
				if !ok {
					continue
				}
				values := exact[fen]
				best := math.Inf(-1)
				for _, v := range values {
					if v > best {
						best = v
					}
				}
				mine, known := values[picked]
				if !known {
					t.Fatalf("%s: the engine played a move that is not legal here", fen)
				}
				if loss := best - mine; loss > worst {
					worst, worstFEN = loss, fen
				}
			}
			t.Logf("%s: worst move loss %.3f pawns (%s)", c.name, worst, worstFEN)
			if worst > c.bound+1e-9 {
				t.Errorf("%s plays a move worth %.3f pawns less than the exact search's "+
					"choice, more than the %.1f allowed\n  %s",
					c.name, worst, c.bound, worstFEN)
			}
		})
	}
}

// exactMoveValues scores every legal move in a position with the reference
// search, from the mover's point of view. Computed once per position and
// shared by every configuration, because it is by far the slow part.
//
// maximizingFor stays the side that moved rather than negating the child's
// score: negation only gives the same answer if the evaluation is perfectly
// antisymmetric, and it is not, since evalPosition sets a side-to-move
// field before scoring.
func exactMoveValues(t *testing.T, g *game.Game, depth int) map[game.Move]float64 {
	t.Helper()
	mover := g.Turn
	ev := evalForPlayer(exactPlayer(depth))
	out := map[game.Move]float64{}
	for _, m := range g.AllLegalMoves(mover) {
		child := *g
		child.ApplyMove(m.From, m.To)
		// One ply is spent by the move itself, so the child is searched one
		// shallower and both sides of the comparison see the same horizon.
		out[m] = naiveMinimax(&child, child.Turn, mover, depth-1, ev)
	}
	return out
}

package engine

import (
	"math"
	"runtime"
	"sync"

	"chess/board"
	"chess/game"
)

// Player is anything that can pick a move: lets the Elo harness pit
// different *kinds* of engine (different depth, different evaluation,
// random) against each other, not just different weight sets.
type Player struct {
	Name  string
	Depth int
	// Weights nil means default.
	Weights Weights
	// Random plays uniformly at random, ignoring Depth/Weights.
	Random bool
	// Quiescence extends the search through captures at the leaves.
	Quiescence bool
	// UsePST swaps the simple centralization nudge for piece-square tables.
	UsePST bool
	// TTBits sizes the transposition table (2^TTBits entries); 0 disables.
	TTBits uint
	// NullMove enables null-move pruning.
	NullMove bool
	// Greedy plays a random capture when one is available, else a random
	// move. It sits between random and a 1-ply search, which is what the
	// bottom of the ladder needs: without a rung there, random vs depth-1
	// is a ~96% score, and the Elo formula's output at that extreme is
	// dominated by a handful of games rather than being a real reading.
	Greedy bool
	// MaterialOnly strips the positional terms, giving a weaker rung to
	// break up another saturating gap.
	MaterialOnly bool
	// QuiescePly caps the quiescence search depth (0 = default), which
	// gives finer-grained rungs between "no quiescence" and "full
	// quiescence" -- that jump alone is otherwise a 97% score, too
	// saturated to measure.
	QuiescePly int
	// Tapered enables middlegame/endgame blended evaluation.
	Tapered bool
	// Iterative uses the iterative-deepening search with killers,
	// history, PVS and late move reductions.
	Iterative bool
	// Extensions, Aspiration and SEEPruning are individually switchable so
	// each can be measured on its own.
	Extensions bool
	Aspiration bool
	SEEPruning bool
	// Structure enables the pawn-structure / king-safety evaluation terms.
	Structure bool
	// Futility enables futility pruning near the leaves.
	Futility bool
	// UCI delegates move choice to an external engine (Stockfish), giving
	// an externally-calibrated reference point rather than only measuring
	// against this engine's own ancestors.
	UCI      *UCIEngine
	UCIDepth int
}

func (p Player) pick(g *game.Game) (game.Move, bool) {
	if p.UCI != nil {
		depth := p.UCIDepth
		if depth <= 0 {
			depth = 1
		}
		return p.UCI.BestMove(g, depth)
	}
	if p.Random || p.Greedy {
		moves := g.AllLegalMoves(g.Turn)
		if len(moves) == 0 {
			return game.Move{}, false
		}
		if p.Greedy {
			captures := moves[:0:0]
			for _, m := range moves {
				if _, isCapture := g.Board.PieceAt(m.To); isCapture {
					captures = append(captures, m)
				}
			}
			if len(captures) > 0 {
				return captures[randIntn(len(captures))], true
			}
		}
		return moves[randIntn(len(moves))], true
	}
	ev := &Eval{Weights: p.Weights, UsePST: p.UsePST, NullMove: p.NullMove, MaterialOnly: p.MaterialOnly, QuiescePly: p.QuiescePly, Tapered: p.Tapered,
		Extensions: p.Extensions, Aspiration: p.Aspiration, SEEPruning: p.SEEPruning,
		Structure: p.Structure, Futility: p.Futility}
	if p.TTBits > 0 {
		ev.Table = NewTranspositionTable(p.TTBits)
	}
	if p.Iterative {
		return ChooseMoveIterative(g, g.Turn, p.Depth, ev, p.Quiescence)
	}
	return chooseMoveOpts(g, g.Turn, p.Depth, ev, p.Quiescence)
}

// AnchorPlayer is the fixed reference the Elo scale is pinned to: the
// original default-weights engine searching one ply. Defined as 0 Elo, so
// every measurement is comparable across the whole session regardless of
// how the champion changes.
func AnchorPlayer() Player {
	return Player{Name: "anchor(depth-1 default)", Depth: 1}
}

type MatchResult struct {
	Wins, Draws, Losses int
}

func (m MatchResult) Games() int { return m.Wins + m.Draws + m.Losses }
func (m MatchResult) Score() float64 {
	return (float64(m.Wins) + 0.5*float64(m.Draws)) / float64(m.Games())
}
func (m MatchResult) Elo() int { return EloFromWinRate(m.Score()) }

// EloMargin is the 95% confidence half-width on the Elo estimate, from
// the standard error of the score. Reporting an Elo without it invites
// reading noise as improvement -- which happened repeatedly with
// small-sample measurements earlier in this project.
func (m MatchResult) EloMargin() int {
	n := float64(m.Games())
	s := m.Score()
	if s <= 0 || s >= 1 {
		return 0
	}
	se := math.Sqrt(s * (1 - s) / n)
	lo := EloFromWinRate(math.Max(0.001, s-1.96*se))
	hi := EloFromWinRate(math.Min(0.999, s+1.96*se))
	return (hi - lo) / 2
}

// PlayMatch plays `games` games between a and b with alternating colours,
// concurrently across GOMAXPROCS goroutines, and reports a's record.
func PlayMatch(a, b Player, games, maxMoves int) MatchResult {
	return PlayMatchLive(a, b, games, maxMoves, nil)
}

// PlayMatchLive is PlayMatch with a hook on the first game, so a UI can
// watch one representative game of the match as it happens.
func PlayMatchLive(a, b Player, games, maxMoves int, live LiveHook) MatchResult {
	type outcome struct {
		score float64
	}
	scores := make([]float64, games)
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup

	for i := 0; i < games; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			white, black := a, b
			aIsWhite := i%2 == 0
			if !aIsWhite {
				white, black = b, a
			}
			var hook LiveHook
			if i == 0 {
				hook = live
			}
			winner, decisive := playPlayersLive(white, black, maxMoves, hook)
			perspective := board.White
			if !aIsWhite {
				perspective = board.Black
			}
			scores[i] = ResultScore(winner, decisive, perspective)
		}(i)
	}
	wg.Wait()

	var res MatchResult
	for _, s := range scores {
		switch s {
		case 1.0:
			res.Wins++
		case 0.5:
			res.Draws++
		default:
			res.Losses++
		}
	}
	return res
}

// PlayMatchSerial plays the games one at a time. Needed for an external
// UCI engine: one process behind one pipe cannot serve concurrent games.
func PlayMatchSerial(a, b Player, games, maxMoves int) MatchResult {
	var res MatchResult
	for i := 0; i < games; i++ {
		white, black := a, b
		aIsWhite := i%2 == 0
		if !aIsWhite {
			white, black = b, a
		}
		winner, decisive := playPlayersLive(white, black, maxMoves, nil)
		perspective := board.White
		if !aIsWhite {
			perspective = board.Black
		}
		switch engineScore := ResultScore(winner, decisive, perspective); engineScore {
		case 1.0:
			res.Wins++
		case 0.5:
			res.Draws++
		default:
			res.Losses++
		}
	}
	return res
}

func playPlayersLive(white, black Player, maxMoves int, live LiveHook) (board.Color, bool) {
	g := game.New()
	for plies := 0; plies < maxMoves && !g.IsOver(); plies++ {
		p := white
		if g.Turn == board.Black {
			p = black
		}
		move, ok := p.pick(g)
		if !ok {
			break
		}
		g.ApplyMove(move.From, move.To)
		if live != nil {
			live(g, plies+1, move.From, move.To)
		}
	}
	if g.IsCheckmate(g.Turn) || g.KingCaptured {
		return g.Turn.Other(), true
	}
	return board.White, false
}

// PlayerPick exposes a Player's move choice for benchmarking harnesses.
func PlayerPick(p Player, g *game.Game) (game.Move, bool) { return p.pick(g) }

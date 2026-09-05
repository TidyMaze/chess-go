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
	// Tuned swaps the hand-picked evaluation constants for the Texel-fitted
	// ones. Set alongside Weights only if you mean to override the fitted
	// material values.
	Tuned bool
	// Net replaces the whole hand-written evaluation with a trained one.
	Net *Net
	// NoCastle declines castling. Measurement only, see Eval.NoCastle.
	NoCastle bool
	// NoLMR disables late move reductions.
	NoLMR bool
	// Mobility adds the mobility evaluation term.
	Mobility bool
	// UCI delegates move choice to an external engine (Stockfish), giving
	// an externally-calibrated reference point rather than only measuring
	// against this engine's own ancestors.
	UCI      *UCIEngine
	UCIDepth int
}

// withTable is the transposition table this player reuses for the whole
// game. A fresh table was being allocated for every single move: at
// 2^20 entries that is a 24 MB allocation per move, and the profile at
// depth 7 showed roughly 16% of CPU in madvise and allocator traffic
// because of it. Reusing it is also stronger, not just faster, since
// entries from earlier moves in the same game are still valid and save
// the search rediscovering them.
func (p Player) pick(g *game.Game) (game.Move, bool) {
	return p.pickWith(g, nil)
}

func (p Player) pickWith(g *game.Game, reuse *TranspositionTable) (game.Move, bool) {
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
	ev.Net = p.Net
	ev.NoCastle = p.NoCastle
	ev.NoLMR = p.NoLMR
	ev.Mobility = p.Mobility
	if p.Tuned {
		if p.Weights == nil {
			ev.Weights = TunedWeights()
		}
		scale := TunedPSTScale()
		sw := TunedStructure()
		mob := TunedMobility()
		ev.PSTScale, ev.StructureW = &scale, &sw
		// The set was fitted with mobility in it, so the values are only
		// correct together: using them without the term double-counts what
		// mobility was absorbing.
		ev.Mobility, ev.MobilityW = true, &mob
	}
	switch {
	case reuse != nil:
		ev.Table = reuse
	case p.TTBits > 0:
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
	// One table per player per game, not one per move. They must not be
	// shared between the two players: a stored score is from one side's
	// point of view, and the entries also encode each engine's own
	// evaluation, which is exactly what an A/B is varying.
	tables := map[board.Color]*TranspositionTable{}
	for c, p := range map[board.Color]Player{board.White: white, board.Black: black} {
		if p.TTBits > 0 {
			tables[c] = NewTranspositionTable(p.TTBits)
		}
	}
	for plies := 0; plies < maxMoves && !g.IsOver(); plies++ {
		p := white
		if g.Turn == board.Black {
			p = black
		}
		move, ok := p.pickWith(g, tables[g.Turn])
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

// PlayMatchAgainstUCI plays a match against an external engine using one
// process per worker, so the games run in parallel.
//
// PlayMatchSerial exists because a single UCI process is a single
// conversation: two goroutines sharing it interleave their commands and
// corrupt each other's search. The fix is not a lock (that just
// serialises again) but one process per worker, which is what newOpponent
// supplies. On this machine that turns a calibration run from ten minutes
// into about one.
//
// Colours still alternate by game index, so a worker taking games 3 and 7
// plays the same colours it would have in the serial version and the
// result is unchanged apart from speed.
func PlayMatchAgainstUCI(me Player, newOpponent func() (Player, func(), error), games, maxMoves, workers int) (MatchResult, error) {
	if workers < 1 {
		workers = 1
	}
	if workers > games {
		workers = games
	}

	type outcome struct {
		wins, draws, losses int
		err                 error
	}
	results := make([]outcome, workers)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			opp, closeOpp, err := newOpponent()
			if err != nil {
				results[w].err = err
				return
			}
			defer closeOpp()
			for i := w; i < games; i += workers {
				white, black := me, opp
				meIsWhite := i%2 == 0
				if !meIsWhite {
					white, black = opp, me
				}
				winner, decisive := playPlayersLive(white, black, maxMoves, nil)
				perspective := board.White
				if !meIsWhite {
					perspective = board.Black
				}
				switch ResultScore(winner, decisive, perspective) {
				case 1.0:
					results[w].wins++
				case 0.5:
					results[w].draws++
				default:
					results[w].losses++
				}
			}
		}(w)
	}
	wg.Wait()

	var res MatchResult
	for _, r := range results {
		if r.err != nil {
			return res, r.err
		}
		res.Wins += r.wins
		res.Draws += r.draws
		res.Losses += r.losses
	}
	return res, nil
}

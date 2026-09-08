package engine

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

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
	// HalfKP is the king-conditioned network, used the way NNUE is.
	HalfKP *HalfKPNet
	// HalfKPBlend weights the hand-written evaluation against it.
	HalfKPBlend float64
	// NoCastle declines castling. Measurement only, see Eval.NoCastle.
	NoCastle bool
	// NoLMR disables late move reductions.
	NoLMR bool
	// LMP enables late move pruning: quiet moves past a depth-dependent
	// count are not searched at all.
	LMP bool
	// ScaledLMR scales the reduction with depth and move number.
	ScaledLMR bool
	// NoRepetition disables repetition detection. Measurement only.
	NoRepetition bool
	// NullReduction and NullScale tune null-move pruning. 0 means 3.
	NullReduction int
	NullScale     bool
	// KeepNullMoveEP reproduces the null-move en passant bug. Measurement
	// only, so its cost can be measured rather than guessed at.
	KeepNullMoveEP bool
	// Mobility adds the mobility evaluation term.
	Mobility bool
	// KingSafety weights the attacker-counting king danger term.
	KingSafety float64
	// Extras enables rook-on-seventh, doubled rooks and a tempo bonus.
	Extras bool
	// Shape enables outposts, connected and backward pawns, bad bishops.
	Shape bool
	// ShapeW overrides their weights.
	ShapeW *ShapeWeights
	// PSTScale multiplies the piece-square tables.
	PSTScale *[6]float64
	// MobilityW and StructureW override the evaluation constants, so a
	// tuner can vary them per player without touching package state.
	MobilityW  *[6]float64
	StructureW *StructureWeights
	// Tablebases gives exact endgame results.
	Tablebases *TablebaseSet
	// TimeBudget, when positive, replaces the fixed depth with a per-move
	// time budget: the search deepens until the next iteration would not
	// fit. Depth then becomes the ceiling rather than the target.
	TimeBudget time.Duration
	// Book is an opening book consulted before searching. A hit returns
	// the move a depth-46 search chose, which is forty plies deeper than
	// this engine reaches.
	Book *Book
	// UCI delegates move choice to an external engine (Stockfish), giving
	// an externally-calibrated reference point rather than only measuring
	// against this engine's own ancestors.
	UCI      *UCIEngine
	UCIDepth int
	// UCIMoveTimeMS, when positive, gives the external engine a per-move
	// clock instead of UCIDepth, so a timed match is timed on both sides.
	UCIMoveTimeMS int
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
	// The book comes first: a hit is a move from a far deeper search than
	// this engine can run, so searching the position instead would be
	// slower and worse.
	if m, ok := p.Book.Move(g); ok {
		return m, true
	}
	if p.UCI != nil {
		depth := p.UCIDepth
		if depth <= 0 {
			depth = 1
		}
		return p.UCI.BestMove(g, depth, p.UCIMoveTimeMS)
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
	ev := evalForPlayer(p)
	switch {
	case reuse != nil:
		ev.Table = reuse
	case p.TTBits > 0:
		ev.Table = NewTranspositionTable(p.TTBits)
	}
	if p.Iterative {
		depth := p.Depth
		if p.TimeBudget > 0 && depth < maxTimedDepth {
			// Under a clock the configured depth is a floor to search past,
			// not a target, so the ceiling is raised out of the way.
			depth = maxTimedDepth
		}
		return ChooseMoveIterativeTimed(g, g.Turn, depth, ev, p.Quiescence, p.TimeBudget)
	}
	return chooseMoveOpts(g, g.Turn, p.Depth, ev, p.Quiescence)
}

// maxTimedDepth caps a time-budgeted search, so a trivially simple
// position cannot spin to an absurd depth inside its budget.
const maxTimedDepth = 24

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

// MatchProgressEvery is how often a running match reports progress, and
// MatchProgress overrides where that report goes. A UI sets the hook to
// route it into a status file; left nil it prints to stdout.
var (
	MatchProgressEvery = 15 * time.Second
	MatchProgress      func(done, total int, elapsed, eta time.Duration)
)

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

	// Progress and a remaining-time estimate.
	//
	// A match is the slowest thing in this project, minutes to an hour,
	// and until now it printed nothing until it was finished. That is
	// indistinguishable from a hung process, and a run that looks hung
	// gets killed, which is how measurements get paid for twice.
	//
	// The estimate is games-completed over elapsed, which is honest here
	// because games are independent and run at a steady rate once all the
	// workers are busy.
	var done int64
	start := time.Now()
	progressStop := make(chan struct{})
	go func() {
		t := time.NewTicker(MatchProgressEvery)
		defer t.Stop()
		for {
			select {
			case <-progressStop:
				return
			case <-t.C:
				n := atomic.LoadInt64(&done)
				if n == 0 {
					continue
				}
				el := time.Since(start)
				per := el / time.Duration(n)
				eta := time.Duration(int64(games)-n) * per
				if MatchProgress != nil {
					MatchProgress(int(n), games, el, eta)
					continue
				}
				fmt.Printf("  %d/%d games (%.0f%%), %s elapsed, about %s left\n",
					n, games, 100*float64(n)/float64(games),
					el.Truncate(time.Second), eta.Truncate(time.Second))
			}
		}
	}()
	defer close(progressStop)

	for i := 0; i < games; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			defer atomic.AddInt64(&done, 1)

			white, black := a, b
			aIsWhite := i%2 == 0
			if !aIsWhite {
				white, black = b, a
			}
			var hook LiveHook
			if i == 0 {
				hook = live
			}
			// i/2 so the pair sharing an opening gets the same seed, and
			// the same opening is played twice with colours reversed.
			start := matchOpening(i / 2)
			winner, decisive := playFrom(start, white, black, maxMoves, hook)
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
		start := randomOpening(rand.New(rand.NewSource(int64(i/2)+1)), OpeningPlies)
		winner, decisive := playFrom(start, white, black, maxMoves, nil)
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

// OpeningPlies is how many random moves start each game.
//
// Without it every game in a match begins from the same position, and two
// engines that differ in one flag play very nearly the same game every
// time: recent 400-game matches were 40% draws, which wastes most of the
// sample. Random openings make the games independent, so a match of the
// same size resolves a smaller difference.
//
// Paired: games 2k and 2k+1 use the same opening with the colours
// swapped, so neither engine is handed the better half of the book.
var OpeningPlies = 6

// randomOpening plays OpeningPlies legal moves chosen by rnd. Positions
// where that leaves someone already lost are not filtered out: both
// engines get the same one, and a slightly unbalanced start is a
// perfectly good test of who handles it better.
// MatchOpenings, when set, replaces the random opening plies with real
// positions from analysed games.
//
// Random plies were introduced to stop two engines differing in one flag
// playing near-identical games, and they do that. They also start every
// measured game from a position no real game reaches, which is the same
// distribution problem that was measured in the training data: mean
// material gap 3.47 with 22% of positions lopsided, against 6.79 and 53%
// in real play. A match from real openings measures the engine where it
// is actually used.
var MatchOpenings []string

// MatchOpeningOffset shifts which openings a match uses.
//
// It exists so a long match can be run as several short ones and the
// results pooled. A 2,000 game match is a quarter of an hour during which
// nothing can be learned or changed; four 500 game chunks give the same
// precision with four points at which to stop early or adapt. Without an
// offset every chunk would replay the same openings and the pooled result
// would be one chunk measured four times.
var MatchOpeningOffset int

func matchOpening(pair int) *game.Game {
	pair += MatchOpeningOffset
	if n := len(MatchOpenings); n > 0 {
		if g, err := game.ParseFEN(MatchOpenings[pair%n]); err == nil {
			return g
		}
	}
	return randomOpening(rand.New(rand.NewSource(int64(pair)+1)), OpeningPlies)
}

func randomOpening(rnd *rand.Rand, plies int) *game.Game {
	g := game.New()
	for i := 0; i < plies; i++ {
		legal := g.AllLegalMoves(g.Turn)
		if len(legal) == 0 {
			break
		}
		m := legal[rnd.Intn(len(legal))]
		g.ApplyMove(m.From, m.To)
	}
	return g
}

func playPlayersLive(white, black Player, maxMoves int, live LiveHook) (board.Color, bool) {
	return playFrom(game.New(), white, black, maxMoves, live)
}

func playFrom(g *game.Game, white, black Player, maxMoves int, live LiveHook) (board.Color, bool) {
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

// PlayerPickWith is PlayerPick with a caller-supplied transposition
// table.
//
// Without it, every call allocates one: at TTBits 20 that is 24 MB per
// move. The self-play generator calls this for every move of every game,
// so a profile of the training workload was 58% Go runtime, a quarter of
// it in madvise alone, handing pages back to the operating system as
// fast as they were taken. Reuse also makes each search cheaper, since
// entries from earlier moves in the same game are still valid.
func PlayerPickWith(p Player, g *game.Game, reuse *TranspositionTable) (game.Move, bool) {
	return p.pickWith(g, reuse)
}

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
				start := randomOpening(rand.New(rand.NewSource(int64(i/2)+1)), OpeningPlies)
				winner, decisive := playFrom(start, white, black, maxMoves, nil)
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

// PlayerScore returns the player's evaluation of the position from the
// side to move's point of view, using its full search.
//
// This is the training signal for the self-play loop: the network learns
// to predict what a deeper search of the same engine concludes, so the
// evaluation is being fitted to its own search rather than to another
// engine's opinion.
func PlayerScore(p Player, g *game.Game) (float64, bool) {
	return PlayerScoreWith(p, g, nil)
}

// evalForPlayer is the one place a Player's settings become the Eval the
// search runs on.
//
// It exists because there used to be two: PlayerPick built one list of
// fields and PlayerScoreWith built another, and the second was missing
// HalfKPBlend, the tuned weight vectors, and every null-move and
// late-move-reduction setting. PlayerScoreWith is what labels training
// positions, so the teacher that produced the labels was quietly a
// different player from the one that was raced afterwards: the champion
// plays with 45% of the hand evaluation blended in and labelled with none
// of it.
func evalForPlayer(p Player) *Eval {
	ev := &Eval{
		Weights: p.Weights, UsePST: p.UsePST, NullMove: p.NullMove,
		MaterialOnly: p.MaterialOnly, QuiescePly: p.QuiescePly, Tapered: p.Tapered,
		Extensions: p.Extensions, Aspiration: p.Aspiration, SEEPruning: p.SEEPruning,
		Structure: p.Structure, Futility: p.Futility, Mobility: p.Mobility,
		KingSafety: p.KingSafety, Net: p.Net, HalfKP: p.HalfKP,
		HalfKPBlend: p.HalfKPBlend, Tablebases: p.Tablebases,
		NoCastle: p.NoCastle, NoLMR: p.NoLMR, ScaledLMR: p.ScaledLMR, LMP: p.LMP,
		NoRepetition: p.NoRepetition, KeepNullMoveEP: p.KeepNullMoveEP,
		NullReduction: p.NullReduction, NullScale: p.NullScale,
		Extras: p.Extras, Shape: p.Shape, ShapeW: p.ShapeW,
		PSTScale: p.PSTScale, MobilityW: p.MobilityW, StructureW: p.StructureW,
	}
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
	return ev
}

// PlayerScoreWith is PlayerScore with a caller-supplied transposition
// table.
//
// PlayerScore allocates one, and at TTBits 20 that is 24 MB per call. The
// training data generator calls it once per labelled position, thousands
// of times per game, so it was allocating and zeroing tens of gigabytes
// to label a single generation. Passing one table per worker removes it
// entirely, and reuse across positions also makes each search cheaper.
func PlayerScoreWith(p Player, g *game.Game, reuse *TranspositionTable) (float64, bool) {
	ev := evalForPlayer(p)
	switch {
	case reuse != nil:
		ev.Table = reuse
	case p.TTBits > 0:
		ev.Table = NewTranspositionTable(p.TTBits)
	}
	depth := p.Depth
	if depth < 1 {
		depth = 1
	}
	if len(g.AllLegalMoves(g.Turn)) == 0 {
		return 0, false
	}
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev, ctx.quiescence, ctx.extensions = ev, p.Quiescence, ev.Extensions
	// The root's own key, so a repetition at ply 1 is compared against this
	// position rather than against whatever the pooled context last held.
	ctx.path[0] = zobristHash(g)
	ctx.played = playedKeys(g)
	score := ctx.search(g, g.Turn, g.Turn, depth, 0, negInf, posInf)
	return score, true
}

// PlayerStaticEval is the player's evaluation of a position with no
// search, from White's point of view.
//
// The self-play loop needs this because its network is a residual: at
// play time the network's output is added to this, so the target it must
// be trained on is the search score minus this, not the search score
// itself. Training it on the full score makes the engine count the
// evaluation twice.
func PlayerStaticEval(p Player, b *board.Board) float64 {
	ev := &Eval{Weights: p.Weights, UsePST: p.UsePST, MaterialOnly: p.MaterialOnly,
		Tapered: p.Tapered, Structure: p.Structure, Mobility: p.Mobility,
		KingSafety: p.KingSafety, Net: p.Net, HalfKP: p.HalfKP}
	if p.Tuned {
		if p.Weights == nil {
			ev.Weights = TunedWeights()
		}
		scale := TunedPSTScale()
		sw := TunedStructure()
		mob := TunedMobility()
		ev.PSTScale, ev.StructureW = &scale, &sw
		ev.Mobility, ev.MobilityW = true, &mob
	}
	return PositionScoreEval(b, board.White, ev)
}

// QuiescenceScore is the position's score after resolving captures, from
// the side to move's point of view.
//
// Used to tell quiet positions from tactical ones. A position where the
// quiescence score differs sharply from the static score has a capture
// sequence pending, and no static evaluation can be expected to predict
// what that sequence is worth: that is the search's job. Training a
// network on such positions teaches it noise.
func QuiescenceScore(p Player, g *game.Game) float64 {
	ev := &Eval{Weights: p.Weights, UsePST: p.UsePST, MaterialOnly: p.MaterialOnly,
		QuiescePly: p.QuiescePly, Tapered: p.Tapered, SEEPruning: p.SEEPruning,
		Structure: p.Structure, Mobility: p.Mobility, KingSafety: p.KingSafety,
		Net: p.Net}
	return quiesce(g, g.Turn, g.Turn, negInf, posInf, ev, 0, 0)
}

// Strong is the engine's standard configuration, in one place.
//
// Every command used to build this list itself, which meant a term could
// be confirmed in a gauntlet and then quietly absent from calibration or
// from the training champion. The mobility and king-safety terms are here
// because they measured +16 +/- 12 Elo together over 3000 games, which
// clears its own margin; individually they were +9 +/- 34 and +13 +/- 34,
// inside theirs, which is why they were not adopted before and why the
// stacked measurement was worth running.
func Strong(depth int) Player {
	return Player{
		Name: "strong", Depth: depth,
		UsePST: true, Quiescence: true, Tapered: true, Structure: true,
		TTBits: 20, NullMove: true, Iterative: true,
		// Aspiration is back on. It was switched off on a measurement of
		// +86 +/- 35 Elo for removing it, and that measurement was real but
		// its cause was not the window: the table keyed every node on the
		// root's side to move, and a narrow window is precisely what
		// decides whether a poisoned entry is believed, so the window was
		// carrying the blame for the table. Turning it off hid the bug and
		// paid a large part of its cost anyway, which is why depth 6 could
		// not beat depth 5 either way.
		//
		// With the key fixed it searches 1.87x fewer nodes over the test
		// positions at depths 4 to 6, and its worst move loss against a
		// full reference search is smaller than without it, 1.038 pawns
		// against 1.867. Cheaper and no worse, so it stays.
		Extensions: true, Aspiration: true, SEEPruning: true, Futility: true,
		Mobility: true, KingSafety: 0.01,
	}
}

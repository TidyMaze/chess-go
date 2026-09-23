// Package engine implements position evaluation, alpha-beta search, and
// the self-play training loop.
package engine

import (
	"math/bits"

	"chess/board"
	"chess/game"
)

// Weights is indexed by PieceType rather than keyed by it: this is read
// for every piece at every evaluated node, and a map lookup there showed
// up as ~10% of total CPU in the profile.
type Weights *[6]float64

func DefaultWeights() Weights {
	return &[6]float64{
		board.Pawn: 1, board.Knight: 3, board.Bishop: 3,
		board.Rook: 5, board.Queen: 9, board.King: 0,
	}
}

func CloneWeights(w Weights) Weights {
	out := *w
	return &out
}

var defaultWeights = DefaultWeights()

// centerBonus: fixed (non-evolving) positional nudge. Pure material
// evaluation can't tell a knight on a rim square from one commanding the
// center; small relative to material so it only breaks ties.
var centerBonus = [6]float64{board.Knight: 0.1, board.Bishop: 0.05, board.Pawn: 0.02}

func centerDistance(s board.Sq) float64 {
	df := abs(float64(s.File) - 3.5)
	dr := abs(float64(s.Rank) - 3.5)
	if df > dr {
		return df
	}
	return dr
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// materialAndCentralization: fused single pass over PiecesOf(color) --
// computes both material and the centralization bonus together instead
// of two separate scans, since PiecesOf is non-trivial cost at the node
// counts search reaches.
func materialAndCentralization(b *board.Board, color board.Color, weights Weights, usePST bool) (material, center float64) {
	return materialAndPositional(b, color, weights, usePST, false, 1.0, &defaultPSTScale)
}

// gamePhase is 1.0 with all pieces on the board and 0.0 in a bare
// king-and-pawn ending.
func gamePhase(b *board.Board) float64 {
	knights := bits.OnesCount64(b.PieceBitboard(board.White, board.Knight) | b.PieceBitboard(board.Black, board.Knight))
	bishops := bits.OnesCount64(b.PieceBitboard(board.White, board.Bishop) | b.PieceBitboard(board.Black, board.Bishop))
	rooks := bits.OnesCount64(b.PieceBitboard(board.White, board.Rook) | b.PieceBitboard(board.Black, board.Rook))
	queens := bits.OnesCount64(b.PieceBitboard(board.White, board.Queen) | b.PieceBitboard(board.Black, board.Queen))
	total := float64(knights + bishops + 2*rooks + 4*queens)
	if total > maxPhase {
		total = maxPhase
	}
	return total / maxPhase
}

func materialAndPositional(b *board.Board, color board.Color, weights Weights, usePST, tapered bool, phase float64, scale *[6]float64) (material, center float64) {
	var buf [16]board.PieceAtSquare
	bishops := 0
	for _, ps := range b.AppendPiecesOf(buf[:0], color) {
		material += weights[ps.Type]
		if ps.Type == board.Bishop {
			bishops++
		}
		if tapered {
			center += pstValueTapered(ps.Type, ps.Sq, color, phase, scale)
		} else if usePST {
			center += pstValue(ps.Type, ps.Sq, color, scale)
		} else if bonus := centerBonus[ps.Type]; bonus != 0 {
			center += bonus * (3.5 - centerDistance(ps.Sq))
		}
	}
	// Bishop pair: two bishops cover both colour complexes and are worth
	// noticeably more than the sum of their parts, which per-piece values
	// cannot express.
	if (tapered || usePST) && bishops >= 2 {
		center += 0.3
	}
	return
}

// Eval bundles everything the evaluation needs. It travels with the
// search instead of living in package-level state, so two engines with
// different evaluations can play each other concurrently -- which is
// exactly what the Elo ladder does.
type Eval struct {
	Weights Weights
	UsePST  bool
	// Table is the transposition cache. Nil disables it. One table per
	// search (not shared across concurrent games) keeps it lock-free.
	Table *TranspositionTable
	// NullMove enables null-move pruning.
	NullMove bool
	// MaterialOnly strips positional terms from the evaluation.
	MaterialOnly bool
	// QuiescePly caps how far the quiescence search follows captures.
	// 0 means the default.
	QuiescePly int
	// Tapered blends middlegame and endgame piece-square tables by how
	// much material is left.
	Tapered bool
	// Extensions enables check extensions.
	Extensions bool
	// Aspiration enables aspiration windows in iterative deepening.
	Aspiration bool
	// SEEPruning skips plainly-losing captures in quiescence.
	SEEPruning bool
	// Structure enables pawn-structure, rook-placement and king-safety
	// terms.
	Structure bool
	// Futility enables futility and reverse-futility pruning near the
	// leaves (Heinz, 1998).
	Futility bool
	// Razoring verifies hopeless low-depth nodes with a quiescence
	// search instead of searching them at full width.
	Razoring bool
	// NullPieces skips the null move when the side to move has under two pieces.
	NullPieces bool
	// DrawScale shrinks scores in endgames the side ahead cannot usually win.
	DrawScale bool
	// PawnPush exempts a pawn push to the sixth rank or beyond from
	// late move reduction and pruning.
	PawnPush bool

	// Mobility adds a bonus per square each piece can reach.
	Mobility bool
	// KingSafety weights the attacker-counting king danger term. 0 is off.
	KingSafety float64
	// Extras enables rook-on-seventh, doubled rooks and a tempo bonus.
	Extras bool
	// Shape enables outposts, connected and backward pawns, bad bishops.
	Shape bool
	// ShapeW overrides their weights. Nil uses the defaults.
	ShapeW *ShapeWeights
	// MobilityW overrides the per-piece mobility weights. Nil uses the
	// defaults. Hand-picked weights measured +9 +/- 34, which is what a
	// guess is worth; these exist so the tuner can fit them instead.
	MobilityW *[6]float64
	// PSTScale and StructureW override the built-in evaluation constants,
	// so a tuned engine can play an untuned one in the same process. Nil
	// means use the defaults.
	PSTScale   *[6]float64
	StructureW *StructureWeights
	// NoRepetition turns off repetition detection in the search. A
	// measurement instrument only, like NoCastle: repetition is a rule of
	// chess and detecting it is not optional. It exists so one side of an
	// A/B can price the change.
	NoRepetition bool
	// NoLMR disables late move reductions. They assume the move ordering
	// is good enough that anything after the first few is not worth full
	// depth, which is a much bigger assumption at depth 4 than at 20.
	NoLMR bool
	// LMP enables late move pruning in the search.
	LMP bool
	// DeepLMP extends late move pruning to depths up to 8.
	DeepLMP bool
	// DeepRFP and NullGate: see Player.
	DeepRFP  bool
	NullGate bool
	// Countermoves: see Player.
	Countermoves bool
	// IIR: see Player.
	IIR bool
	// MainSEE: see Player.
	MainSEE bool
	// ScaledLMR reduces more with depth and move number instead of a flat
	// one ply.
	ScaledLMR bool
	// LMRTwoStep confirms a fail-high on a reduced search with a zero-window
	// full-depth search before opening a full window.
	LMRTwoStep bool
	// HistoryAging decays history table between iterative deepening iterations.
	HistoryAging bool
	// HistoryMalus penalizes quiet moves that failed to cause a beta cutoff.
	HistoryMalus bool
	// ContHist orders quiet moves by how they did against the previous move.
	ContHist bool
	// Improving feeds the static-evaluation trend to late move pruning.
	Improving bool
	// HistLMR adjusts the late move reduction by the move's history score.
	HistLMR bool
	// HistGravity bounds the history tables so they keep discriminating.
	HistGravity bool
	// Singular extends a transposition-table move that beats every
	// alternative by a margin on a reduced-depth search.
	Singular bool
	// NoCastle is a measurement instrument, never a playing mode. Castling
	// is a rule of chess and is implemented; this flag exists only so one
	// side of an A/B can decline it, which is the only way to price the
	// rule without maintaining a second engine. It defaults to false and
	// nothing that plays a real game sets it.
	//
	// It lives here rather than on the position because both players share
	// one Game, and it is applied to the root move list because that is
	// where the played move is chosen.
	NoCastle bool
	// Tablebases gives exact results for small endgames. When a position
	// is covered, the score is not an estimate at all.
	Tablebases *TablebaseSet
	// NullReduction is how many plies a null move gives up. 0 means the
	// historical default of 3.
	//
	// Never tuned: it was written as `depth-3` and left there. The one piece
	// of evidence about it is accidental. A bug that left the en passant
	// square set across the null made the null-move search return garbage
	// that pruned harder, and removing it cost 21 +/- 22 Elo. That is a
	// strong hint this engine prunes too little here.
	NullReduction int
	// NullScale adds depth/6 to the reduction, so deep nodes prune harder
	// than shallow ones. Standard practice and absent here.
	NullScale bool
	// KeepNullMoveEP reproduces a bug for measurement only: it leaves the
	// en passant square set across a null move, which is what the engine
	// did until it was found. Never set in play. It exists so the cost of
	// the bug can be measured rather than guessed at.
	KeepNullMoveEP bool
	// DeltaPruning skips captures in quiescence that cannot improve alpha even with a safety margin.
	DeltaPruning bool
	// STM is the side to move at the position being evaluated.
	//
	// The evaluation's `color` argument is the side the score is *for*,
	// which during a search is the root's colour and is fixed for the
	// whole tree. A tablebase index needs the side to move at this node,
	// which is a different thing and changes every ply. Probing with the
	// root's colour indexed the wrong entry on about half of all nodes:
	// the tablebase measured -24 +/- 22 Elo and did not improve rook
	// endgame conversion at all (4 of 6 either way), which is what a probe
	// that is half wrong looks like.
	//
	// Set by evalPosition at every call site inside the search. One Eval
	// belongs to one game searched by one goroutine, so this is per-node
	// state on a per-search object rather than shared mutable state.
	STM board.Color
	// HalfKP is a king-conditioned network. When set it replaces the
	// hand-written evaluation, or blends with it if HalfKPBlend is set.
	HalfKP *HalfKPNet
	// HalfKPBlend is the weight given to the hand-written evaluation when
	// a network is present: 0 uses the network alone, 1 ignores it.
	//
	// A network trained on a few hundred thousand positions is accurate
	// on average and jagged locally. Measured: its score moves 0.97 pawns
	// after a single quiet move, against 0.18 for the hand-written
	// evaluation. The search's pruning is calibrated in pawns (aspiration
	// window 0.5, futility margins 1 to 3), so that jumpiness makes every
	// threshold misfire. Blending keeps the smooth backbone and scales
	// the network's noise by (1 - blend).
	HalfKPBlend float64
	// FiftyClock is the halfmove clock at the node being evaluated. The
	// search sets it per ply; outside the search it stays zero and the
	// game's own clock is used instead.
	FiftyClock int
	// acc is the search's per-ply accumulator stack, accCur the slot for
	// the node being evaluated. Unset outside the search.
	acc      *[accSlots]halfKPAcc
	accCur   *halfKPAcc
	accStats halfKPAccStats
	// Net replaces the hand-written evaluation with a trained network.
	// When set, the material, table and structure terms are not used at
	// all: the network was fitted to the same target they were and is a
	// strictly more expressive function, so summing the two would be
	// double-counting.
	Net *Net
}

func (e *Eval) pstScale() *[6]float64 {
	if e == nil || e.PSTScale == nil {
		return &defaultPSTScale
	}
	return e.PSTScale
}

func (e *Eval) structureWeights() StructureWeights {
	if e == nil || e.StructureW == nil {
		return defaultStructureWeights
	}
	return *e.StructureW
}

func (e *Eval) useSEEPruning() bool { return e != nil && e.SEEPruning }

func (e *Eval) useDeltaPruning() bool { return e != nil && e.DeltaPruning }

func (e *Eval) quiescePly() int {
	if e == nil || e.QuiescePly <= 0 {
		return maxQuiescePly
	}
	return e.QuiescePly
}

func (e *Eval) useNullMove() bool { return e != nil && e.NullMove }

// halfKPScore is the network's score, from the search's per-ply
// accumulator when one is current and valid, else recomputed.
func (e *Eval) halfKPScore(b *board.Board) float64 {
	if e.accCur != nil && e.accCur.valid {
		return e.HalfKP.output(e.accCur)
	}
	return e.HalfKP.Evaluate(b)
}

// setAccPly makes ply's slot current, refreshing it from the parent's.
// A no-op without a stack, so callers outside the search evaluate in
// full as before.
func (e *Eval) setAccPly(b *board.Board, ply int) {
	if e == nil || e.acc == nil || e.HalfKP == nil || ply < 0 || ply >= accSlots {
		return
	}
	self := &e.acc[ply]
	var parent *halfKPAcc
	if ply > 0 {
		parent = &e.acc[ply-1]
	}
	e.HalfKP.refresh(b, self, parent, &e.accStats)
	e.accCur = self
}

func (e *Eval) table() *TranspositionTable {
	if e == nil {
		return nil
	}
	return e.Table
}

func (e *Eval) weightsOrDefault() Weights {
	if e == nil || e.Weights == nil {
		return defaultWeights
	}
	return e.Weights
}

func (e *Eval) usePST() bool { return e != nil && e.UsePST }

func MaterialScore(b *board.Board, color board.Color, weights Weights) float64 {
	if weights == nil {
		weights = defaultWeights
	}
	own, _ := materialAndCentralization(b, color, weights, false)
	enemy, _ := materialAndCentralization(b, color.Other(), weights, false)
	return own - enemy
}

// kingDrivingBonus: endgame technique -- with a winning material edge,
// push the enemy king to the board edge and bring your own king closer
// to help deliver mate.
func kingDrivingBonus(b *board.Board, color board.Color) float64 {
	own := b.KingSquare(color)
	enemy := b.KingSquare(color.Other())
	pushToEdge := centerDistance(enemy)
	df := abs(float64(own.File - enemy.File))
	dr := abs(float64(own.Rank - enemy.Rank))
	kingsDistance := df
	if dr > kingsDistance {
		kingsDistance = dr
	}
	return pushToEdge*0.1 + (7-kingsDistance)*0.05
}

func PositionScore(b *board.Board, color board.Color, weights Weights) float64 {
	return PositionScoreEval(b, color, &Eval{Weights: weights})
}

// positionScoreEvalReference is the original multi-pass evaluation,
// kept only so the fused version can be tested against it. It is not
// used in play.
func positionScoreEvalReference(b *board.Board, color board.Color, ev *Eval) float64 {
	if ev != nil && ev.HalfKP != nil {
		score := ev.halfKPScore(b)
		if color == board.Black {
			score = -score
		}
		if ev.HalfKPBlend > 0 {
			plain := *ev
			plain.HalfKP = nil
			score = ev.HalfKPBlend*PositionScoreEval(b, color, &plain) +
				(1-ev.HalfKPBlend)*score
		}
		// The endgame king-driving term stays: it is the mechanism that
		// converts a won endgame into a mate, not a positional opinion,
		// and the network is trained on positions that are mostly not
		// near mate.
		if score >= 4 {
			score += kingDrivingBonus(b, color)
		} else if score <= -4 {
			score -= kingDrivingBonus(b, color.Other())
		}
		return score
	}
	if ev != nil && ev.Net != nil && !ev.Net.Residual {
		// The network scores from White's point of view; the search wants
		// the score from `color`'s.
		score := ev.Net.Evaluate(b)
		if color == board.Black {
			score = -score
		}
		// The endgame king-driving term stays. It is not a positional
		// opinion but the mechanism that converts a won endgame into an
		// actual mate, and the network was trained on Stockfish scores of
		// positions that were mostly not near mate.
		if score >= 4 {
			score += kingDrivingBonus(b, color)
		} else if score <= -4 {
			score -= kingDrivingBonus(b, color.Other())
		}
		return score
	}
	weights := ev.weightsOrDefault()
	usePST := ev.usePST()
	tapered := ev != nil && ev.Tapered
	phase := 1.0
	if tapered {
		phase = gamePhase(b)
	}
	scale := ev.pstScale()
	ownMaterial, ownCenter := materialAndPositional(b, color, weights, usePST, tapered, phase, scale)
	enemyMaterial, enemyCenter := materialAndPositional(b, color.Other(), weights, usePST, tapered, phase, scale)
	if ev != nil && ev.MaterialOnly {
		ownCenter, enemyCenter = 0, 0
	}
	score := (ownMaterial - enemyMaterial) + (ownCenter - enemyCenter)

	if ev != nil && ev.Structure {
		ownPawns := scanPawns(b, color)
		enemyPawns := scanPawns(b, color.Other())
		sw := ev.structureWeights()
		score += structureScore(b, color, ownPawns, enemyPawns, phase, sw)
		score -= structureScore(b, color.Other(), enemyPawns, ownPawns, phase, sw)
	}

	// A residual network adds what the hand-written terms miss. Material
	// stays where it is known to be right, which is what the first
	// attempt lost by replacing everything at once.
	if ev != nil && ev.Net != nil && ev.Net.Residual {
		correction := ev.Net.Evaluate(b)
		if color == board.Black {
			correction = -correction
		}
		score += correction
	}

	if score >= 4 {
		score += kingDrivingBonus(b, color)
	} else if score <= -4 {
		score -= kingDrivingBonus(b, color.Other())
	}
	return score
}

// PositionScoreEval scores a position from color's point of view.
//
// One walk over the pieces, not seven. The multi-pass version (kept as
// positionScoreEvalReference and tested against) called AppendPiecesOf
// for the phase, for material and tables on each side, for the pawn file
// summaries on each side, and for the structure terms on each side. Each
// of those walks the whole occupied list and decodes every cell, and the
// profile at depth 7 put the evaluation at 33% of all CPU with
// AppendPiecesOf the single largest leaf inside it.
//
// So the board is read once into a stack array and everything else reads
// that array: same arithmetic, same result, a fraction of the memory
// traffic.
func PositionScoreEval(b *board.Board, color board.Color, ev *Eval) float64 {
	// Exact answers first. Where a tablebase covers the position there is
	// nothing for an evaluation to estimate, and the piece-count guard
	// means the probe costs one integer comparison in every position that
	// is not an endgame, which is nearly all of them.
	if ev != nil && ev.Tablebases != nil && b.PieceCount() <= TablebaseMaxPieces {
		if score, ok := ev.Tablebases.Probe(b, ev.STM); ok {
			// Probe answers from White's point of view; this function
			// answers from `color`'s, as every other branch below does.
			// Without this negation the tablebase score was inverted in
			// every game the engine played as Black, which is half of
			// them, and three rounds of tuning the table chased a sign
			// error: -15, -26 and -19 Elo across successive attempts.
			if color == board.Black {
				score = -score
			}
			return score
		}
	}
	if ev != nil && ev.HalfKP != nil {
		score := ev.halfKPScore(b)
		if color == board.Black {
			score = -score
		}
		if ev.HalfKPBlend > 0 {
			plain := *ev
			plain.HalfKP = nil
			score = ev.HalfKPBlend*PositionScoreEval(b, color, &plain) +
				(1-ev.HalfKPBlend)*score
		}
		// The endgame king-driving term stays: it is the mechanism that
		// converts a won endgame into a mate, not a positional opinion,
		// and the network is trained on positions that are mostly not
		// near mate.
		if score >= 4 {
			score += kingDrivingBonus(b, color)
		} else if score <= -4 {
			score -= kingDrivingBonus(b, color.Other())
		}
		return score
	}
	if ev != nil && ev.Net != nil && !ev.Net.Residual {
		score := ev.Net.Evaluate(b)
		if color == board.Black {
			score = -score
		}
		if score >= 4 {
			score += kingDrivingBonus(b, color)
		} else if score <= -4 {
			score -= kingDrivingBonus(b, color.Other())
		}
		return score
	}

	weights := ev.weightsOrDefault()
	usePST := ev.usePST()
	tapered := ev != nil && ev.Tapered
	materialOnly := ev != nil && ev.MaterialOnly
	wantStructure := ev != nil && ev.Structure

	var buf [32]board.ColoredPiece
	pieces := b.AppendAllPieces(buf[:0])

	// Phase first: the tapered tables need it, and it is a property of
	// the whole board rather than of either side.
	phase := 1.0
	if tapered {
		knights := bits.OnesCount64(b.PieceBitboard(board.White, board.Knight) | b.PieceBitboard(board.Black, board.Knight))
		bishops := bits.OnesCount64(b.PieceBitboard(board.White, board.Bishop) | b.PieceBitboard(board.Black, board.Bishop))
		rooks := bits.OnesCount64(b.PieceBitboard(board.White, board.Rook) | b.PieceBitboard(board.Black, board.Rook))
		queens := bits.OnesCount64(b.PieceBitboard(board.White, board.Queen) | b.PieceBitboard(board.Black, board.Queen))
		total := float64(knights + bishops + 2*rooks + 4*queens)
		if total > maxPhase {
			total = maxPhase
		}
		phase = total / maxPhase
	}

	scale := ev.pstScale()
	var material, positional [2]float64
	var bishops [2]int
	var pawns [2]pawnFiles
	for i := range pawns {
		for f := range pawns[i].mostAdv {
			pawns[i].mostAdv[f] = -1
		}
	}

	for _, p := range pieces {
		c := p.Color
		material[c] += weights[p.Type]
		if p.Type == board.Bishop {
			bishops[c]++
		}
		if tapered {
			positional[c] += pstValueTapered(p.Type, p.Sq, c, phase, scale)
		} else if usePST {
			positional[c] += pstValue(p.Type, p.Sq, c, scale)
		} else if bonus := centerBonus[p.Type]; bonus != 0 {
			positional[c] += bonus * (3.5 - centerDistance(p.Sq))
		}
		if p.Type == board.Pawn {
			rank := p.Sq.Rank
			if c == board.Black {
				rank = 7 - rank
			}
			pawns[c].count[p.Sq.File]++
			if rank > pawns[c].mostAdv[p.Sq.File] {
				pawns[c].mostAdv[p.Sq.File] = rank
			}
			pawns[c].anyPawns = true
		}
	}

	for _, c := range [2]board.Color{board.White, board.Black} {
		if (tapered || usePST) && bishops[c] >= 2 {
			positional[c] += 0.3
		}
	}
	if materialOnly {
		positional[board.White], positional[board.Black] = 0, 0
	}

	other := color.Other()
	score := (material[color] - material[other]) + (positional[color] - positional[other])

	if wantStructure {
		sw := ev.structureWeights()
		score += structurePiecesDiff(pieces, color, other, pawns, phase, sw)
	}

	if ev != nil && ev.Shape && wantStructure {
		sw := ev.structureWeights()
		_ = sw
		sh := defaultShape
		if ev.ShapeW != nil {
			sh = shapeWeights{ev.ShapeW.Outpost, ev.ShapeW.Connected,
				ev.ShapeW.Backward, ev.ShapeW.BadBishop}
		}
		score += shapeScore(pieces, color, pawns[color], pawns[other], sh)
		score -= shapeScore(pieces, other, pawns[other], pawns[color], sh)
	}

	if ev != nil && ev.Extras {
		score += extraScore(pieces, color, phase, defaultExtras)
		score -= extraScore(pieces, other, phase, defaultExtras)
		// Tempo: the side to move has a real if small advantage, and
		// nothing else in this evaluation says so.
		if color == board.White {
			score += defaultExtras.Tempo
		} else {
			score -= defaultExtras.Tempo
		}
	}

	if ev != nil && ev.KingSafety != 0 {
		score -= kingSafetyPenalty(b, pieces, color, phase, ev.KingSafety)
		score += kingSafetyPenalty(b, pieces, other, phase, ev.KingSafety)
	}

	if ev != nil && ev.Mobility {
		mw := &defaultMobilityWeights
		if ev.MobilityW != nil {
			mw = ev.MobilityW
		}
		score += mobilityScore(b, pieces, color, mw) - mobilityScore(b, pieces, other, mw)
	}

	// The correction comes before the king-driving bonus, not after: the
	// bonus asks "is this side winning by four pawns", and that question
	// has to be put to the score the engine actually believes, network
	// correction included. Applied the other way round the two evaluations
	// disagreed on 474 of 4000 positions, whenever the correction carried
	// the score across the threshold.
	if ev != nil && ev.Net != nil && ev.Net.Residual {
		correction := ev.Net.Evaluate(b)
		if color == board.Black {
			correction = -correction
		}
		score += correction
	}

	if score >= 4 {
		score += kingDrivingBonus(b, color)
	} else if score <= -4 {
		score -= kingDrivingBonus(b, other)
	}
	return score
}

// evalPosition evaluates a node during a search, recording whose turn it
// is so an endgame tablebase can be indexed correctly.
//
// The evaluation's own `color` argument cannot serve: it is the root's
// colour, constant for the whole tree, while a tablebase index needs the
// side to move at this node.
func evalPosition(g *game.Game, maximizingFor board.Color, ev *Eval) float64 {
	if ev != nil {
		ev.STM = g.Turn
	}
	return fadeForFiftyMove(PositionScoreEval(&g.Board, maximizingFor, ev), g.HalfmoveClock)
}

// fadeForFiftyMove pulls a score toward the draw as the fifty move clock
// runs out, and returns the draw itself once it has.
//
// The search had no idea the clock existed: IsFiftyMoveDraw was written
// and only the training loop ever called it, so a dead drawn position
// evaluated as won and nothing preferred a move that made progress. Two
// real games were thrown away that way, one of them rook and three pawns
// against rook and one with connected passers, where the engine shuffled
// its rook for fifty moves while the clock ran from 41 to 100.
//
// A terminal check alone would not have saved either. At clock 41 the
// draw is fifty-nine plies off, far past any depth this engine reaches,
// so the fade is what makes the difference visible at every depth: a line
// that resets the clock, a pawn move or a capture, keeps its value, and
// one that shuffles loses a little every ply.
//
// Nothing happens until the clock is well under way, so ordinary play is
// untouched: at the fresh clock of almost every position the engine ever
// sees, the score is returned exactly as it came in.
func fadeForFiftyMove(score float64, halfmoveClock int) float64 {
	const (
		limit     = 100 // plies, the fifty move rule
		fadeAfter = 20  // leave early play completely alone
		// The fade stops here rather than reaching zero. Fading all the
		// way creates an incentive to throw material away: a capture
		// resets the clock, so at a fade of 0.3 a side six pawns up scores
		// 1.8, while sacrificing a bishop to reset it scores 3.0. The
		// engine did exactly that, handing a lone king a bishop in the
		// two-bishop mate test, which is how this was caught.
		floor = 0.5
	)
	if halfmoveClock >= limit {
		// The draw has actually happened. This one is not a fade, it is
		// the result.
		return 0
	}
	if halfmoveClock <= fadeAfter {
		return score
	}
	spent := float64(halfmoveClock-fadeAfter) / float64(limit-fadeAfter)
	return score * (1 - (1-floor)*spent)
}

// evalPositionFor is evalPosition told which side is to move, for the
// search, which makes moves on the board without touching g.Turn.
//
// g.Turn is the root's side for the whole tree, so anything that reads
// it (today the tablebase probe) saw the wrong colour on every odd ply.
func evalPositionFor(g *game.Game, sideToMove, maximizingFor board.Color, ev *Eval) float64 {
	clock := g.HalfmoveClock
	if ev != nil {
		ev.STM = sideToMove
		// The search tracks the clock per ply, because it makes its moves
		// on the board and never advances the game's own copy. A caller
		// outside the search leaves FiftyClock at zero, and its game's own
		// clock is the right one to use.
		if ev.FiftyClock > 0 {
			clock = ev.FiftyClock
		}
	}
	score := PositionScoreEval(&g.Board, maximizingFor, ev)
	if ev != nil && ev.DrawScale {
		score = drawScale(&g.Board, score, maximizingFor)
	}
	return fadeForFiftyMove(score, clock)
}

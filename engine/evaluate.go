// Package engine implements position evaluation, alpha-beta search, and
// the self-play training loop.
package engine

import "chess/board"

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
	total := 0.0
	var buf [16]board.PieceAtSquare
	for _, color := range [2]board.Color{board.White, board.Black} {
		for _, ps := range b.AppendPiecesOf(buf[:0], color) {
			total += phaseWeight[ps.Type]
		}
	}
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
	// Mobility adds a bonus per square each piece can reach.
	Mobility bool
	// KingSafety weights the attacker-counting king danger term. 0 is off.
	KingSafety float64
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

func (e *Eval) quiescePly() int {
	if e == nil || e.QuiescePly <= 0 {
		return maxQuiescePly
	}
	return e.QuiescePly
}

func (e *Eval) useNullMove() bool { return e != nil && e.NullMove }

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
		total := 0.0
		for _, p := range pieces {
			total += phaseWeight[p.Type]
		}
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
		score += structurePieces(pieces, color, pawns[color], pawns[other], phase, sw)
		score -= structurePieces(pieces, other, pawns[other], pawns[color], phase, sw)
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

	if score >= 4 {
		score += kingDrivingBonus(b, color)
	} else if score <= -4 {
		score -= kingDrivingBonus(b, other)
	}

	if ev != nil && ev.Net != nil && ev.Net.Residual {
		correction := ev.Net.Evaluate(b)
		if color == board.Black {
			correction = -correction
		}
		score += correction
	}
	return score
}

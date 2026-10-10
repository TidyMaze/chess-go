package engine

import (
	"math"
	"math/bits"
	"math/rand"
	"sync"
	"time"

	"chess/board"
	"chess/game"
	chessmoves "chess/moves"
)

// MCTSConfig configures the Monte Carlo Tree Search.
type MCTSConfig struct {
	Simulations int           // number of iterations (if TimeBudget <= 0)
	TimeBudget  time.Duration // max search duration
	MaxRollout  int           // max plies per simulation rollout
	Exploration float64       // UCB1 exploration constant C
	RNG         *rand.Rand    // random source (forces single-thread determinism if set)
	Threads     int           // search worker threads (0 = auto)
	Eval        *Eval         // leaf evaluation config (nil = default PST+quiescence)
}

type mctsNode struct {
	parent       *mctsNode
	move         game.Move
	turn         board.Color // side to move at this state
	visits       int
	wins         float64     // score from perspective of parent.turn
	children     []*mctsNode
	untriedMoves []game.Move
	isTerminal   bool
	whiteOutcome float64 // 1.0 (White win), 0.5 (draw), 0.0 (Black win)
}

// fastRand is a 64-bit XorShift PRNG with Lemire's fast integer mapping.
type fastRand uint64

func (r *fastRand) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = fastRand(x)
	return x
}

func (r *fastRand) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int((uint64(uint32(r.next())) * uint64(n)) >> 32)
}

// MCTS selects the best move using Pure Monte Carlo Tree Search (defaults to latest MCTSv4).
func MCTS(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	return MCTSv4(g, cfg)
}

// MCTSv2 selects the best move using Pure Monte Carlo Tree Search v2.
func MCTSv2(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	legalMoves := g.AllLegalMoves(g.Turn)
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}
	if len(legalMoves) == 1 {
		return legalMoves[0], true
	}

	if cfg.Exploration <= 0 {
		cfg.Exploration = 1.41421356
	}
	if cfg.MaxRollout <= 0 {
		cfg.MaxRollout = 2
	}

	threads := cfg.Threads
	if threads <= 0 {
		threads = 1
	}

	var baseSeed uint64
	if cfg.RNG != nil {
		baseSeed = uint64(cfg.RNG.Int63())
	} else {
		baseSeed = uint64(time.Now().UnixNano())
	}

	if threads <= 1 {
		root := runMCTSWorker(g, cfg, legalMoves, baseSeed, cfg.Simulations)
		return pickBestRootMove(root, legalMoves)
	}

	workerRoots := make([]*mctsNode, threads)
	var wg sync.WaitGroup
	wg.Add(threads)

	workerSims := 0
	if cfg.Simulations > 0 {
		workerSims = (cfg.Simulations + threads - 1) / threads
	}

	for i := 0; i < threads; i++ {
		go func(workerID int) {
			defer wg.Done()
			seed := baseSeed + uint64(workerID)*0x9e3779b97f4a7c15 + 1
			workerRoots[workerID] = runMCTSWorker(g, cfg, legalMoves, seed, workerSims)
		}(i)
	}
	wg.Wait()

	visits := make(map[game.Move]int, len(legalMoves))
	for _, root := range workerRoots {
		if root != nil {
			for _, child := range root.children {
				visits[child.move] += child.visits
			}
		}
	}

	bestMove := legalMoves[0]
	bestVisits := -1
	for _, m := range legalMoves {
		if v := visits[m]; v > bestVisits {
			bestVisits = v
			bestMove = m
		}
	}
	return bestMove, true
}

func pickBestRootMove(root *mctsNode, legalMoves []game.Move) (game.Move, bool) {
	if len(root.children) == 0 {
		return legalMoves[0], true
	}
	bestChild := root.children[0]
	bestVisits := bestChild.visits
	for _, child := range root.children[1:] {
		if child.visits > bestVisits {
			bestChild = child
			bestVisits = child.visits
		}
	}
	return bestChild.move, true
}

const mctsBlockSize = 2048

type mctsBlock [mctsBlockSize]mctsNode

type mctsTreeArena struct {
	blocks []*mctsBlock
	bIdx   int
	iIdx   int
}

func (a *mctsTreeArena) alloc() *mctsNode {
	if a.bIdx >= len(a.blocks) {
		a.blocks = append(a.blocks, new(mctsBlock))
	}
	n := &a.blocks[a.bIdx][a.iIdx]
	a.iIdx++
	if a.iIdx >= mctsBlockSize {
		a.bIdx++
		a.iIdx = 0
	}
	return n
}

func runMCTSWorker(g *game.Game, cfg MCTSConfig, legalMoves []game.Move, seed uint64, maxSims int) *mctsNode {
	prng := fastRand(seed)
	if prng == 0 {
		prng = 0x853c49e6748fea9b
	}

	arena := mctsTreeArena{
		blocks: make([]*mctsBlock, 0, 8),
	}
	rootMoves := make([]game.Move, len(legalMoves))
	copy(rootMoves, legalMoves)
	root := arena.alloc()
	initMCTSNode(root, nil, game.Move{}, g, rootMoves)

	var deadline time.Time
	hasDeadline := cfg.TimeBudget > 0
	if hasDeadline {
		deadline = time.Now().Add(cfg.TimeBudget)
	}
	if maxSims <= 0 && !hasDeadline {
		maxSims = 1000
	}

	moveBuf := make([]game.Move, 0, 256)
	movesPool := make([]game.Move, 0, 16384)
	pathBuf := make([]game.Move, 0, 64)
	var simGame game.Game

	for sim := 0; ; sim++ {
		if hasDeadline {
			if (sim&63) == 0 && time.Now().After(deadline) {
				break
			}
		} else if sim >= maxSims {
			break
		}

		// 1. Selection: follow tree without cloning game state upfront
		curr := root
		for !curr.isTerminal && len(curr.untriedMoves) == 0 && len(curr.children) > 0 {
			curr = curr.selectChild(cfg.Exploration)
		}

		// Reconstruct game state at curr only when needed (expansion or rollout)
		if !curr.isTerminal {
			pathBuf = pathBuf[:0]
			for n := curr; n != root; n = n.parent {
				pathBuf = append(pathBuf, n.move)
			}
			simGame = *g
			simGame.TrackRepetition = false
			for i := len(pathBuf) - 1; i >= 0; i-- {
				simGame.Apply(pathBuf[i])
			}
		}

		// 2. Expansion
		if !curr.isTerminal && len(curr.untriedMoves) > 0 {
			lastIdx := len(curr.untriedMoves) - 1
			pickIdx := prng.intn(len(curr.untriedMoves))
			m := curr.untriedMoves[pickIdx]
			curr.untriedMoves[pickIdx] = curr.untriedMoves[lastIdx]
			curr.untriedMoves = curr.untriedMoves[:lastIdx]

			simGame.Apply(m)
			if cap(movesPool)-len(movesPool) < 128 {
				movesPool = make([]game.Move, 0, 16384)
			}
			startIdx := len(movesPool)
			movesPool = simGame.AppendLegalMoves(movesPool, simGame.Turn)
			childMoves := movesPool[startIdx:]

			child := arena.alloc()
			initMCTSNode(child, curr, m, &simGame, childMoves)
			curr.children = append(curr.children, child)
			curr = child
		}

		// 3. Simulation (Rollout)
		whiteScore := curr.whiteOutcome
		if !curr.isTerminal {
			whiteScore = rollout(&simGame, cfg.MaxRollout, &prng, moveBuf, curr.untriedMoves)
		}

		// 4. Backpropagation
		for node := curr; node != nil; node = node.parent {
			node.visits++
			if node.parent != nil {
				if node.parent.turn == board.White {
					node.wins += whiteScore
				} else {
					node.wins += (1.0 - whiteScore)
				}
			}
		}
	}
	return root
}

func initMCTSNode(node *mctsNode, parent *mctsNode, m game.Move, g *game.Game, legalMoves []game.Move) {
	initCap := 4
	if len(legalMoves) < initCap {
		initCap = len(legalMoves)
	}
	*node = mctsNode{
		parent:       parent,
		move:         m,
		turn:         g.Turn,
		untriedMoves: legalMoves,
		children:     make([]*mctsNode, 0, initCap),
	}

	if g.KingCaptured {
		node.isTerminal = true
		if g.Turn == board.White {
			node.whiteOutcome = 0.0
		} else {
			node.whiteOutcome = 1.0
		}
		node.untriedMoves = nil
		return
	}

	if len(legalMoves) == 0 {
		node.isTerminal = true
		if chessmoves.IsInCheck(&g.Board, g.Turn) {
			if g.Turn == board.White {
				node.whiteOutcome = 0.0
			} else {
				node.whiteOutcome = 1.0
			}
		} else {
			node.whiteOutcome = 0.5
		}
		node.untriedMoves = nil
		return
	}

	if g.HalfmoveClock >= 100 {
		node.isTerminal = true
		node.whiteOutcome = 0.5
		node.untriedMoves = nil
		return
	}
}

func newMCTSNode(parent *mctsNode, m game.Move, g *game.Game, legalMoves []game.Move) *mctsNode {
	n := new(mctsNode)
	initMCTSNode(n, parent, m, g, legalMoves)
	return n
}

func (n *mctsNode) selectChild(c float64) *mctsNode {
	factor := c * math.Sqrt(math.Log(float64(n.visits)))
	bestScore := -1e9
	var best *mctsNode
	for _, child := range n.children {
		if child.visits == 0 {
			return child
		}
		score := child.wins/float64(child.visits) + factor/math.Sqrt(float64(child.visits))
		if score > bestScore {
			bestScore = score
			best = child
		}
	}
	return best
}

func rollout(g *game.Game, maxPlies int, rng *fastRand, moveBuf []game.Move, firstMoves []game.Move) float64 {
	for ply := 0; ply < maxPlies; ply++ {
		if g.KingCaptured {
			if g.Turn == board.White {
				return 0.0
			}
			return 1.0
		}
		if g.HalfmoveClock >= 100 {
			return 0.5
		}
		var moves []game.Move
		if ply == 0 && len(firstMoves) > 0 {
			moves = firstMoves
		} else {
			moves = g.AppendLegalMoves(moveBuf[:0], g.Turn)
		}
		if len(moves) == 0 {
			if chessmoves.IsInCheck(&g.Board, g.Turn) {
				if g.Turn == board.White {
					return 0.0
				}
				return 1.0
			}
			return 0.5
		}
		m := moves[rng.intn(len(moves))]
		g.Apply(m)
	}

	return materialPayoff(&g.Board)
}

var materialWeights = [6]int{
	board.Pawn: 1, board.Knight: 3, board.Bishop: 3,
	board.Rook: 5, board.Queen: 9, board.King: 0,
}

var sigmoidPayoffTable = func() [256]float64 {
	var tbl [256]float64
	for i := -128; i < 128; i++ {
		tbl[i+128] = 1.0 / (1.0 + math.Exp(-float64(i)/8.0))
	}
	return tbl
}()

func materialPayoff(b *board.Board) float64 {
	diff := 0
	for pt := board.Pawn; pt <= board.Queen; pt++ {
		w := materialWeights[pt] * 2
		diff += w * bits.OnesCount64(b.PieceBitboard(board.White, pt))
		diff -= w * bits.OnesCount64(b.PieceBitboard(board.Black, pt))
	}
	idx := diff + 128
	if idx < 0 {
		return 0.0
	}
	if idx >= 256 {
		return 1.0
	}
	return sigmoidPayoffTable[idx]
}

var mctsPSTEval = &Eval{
	Weights:    defaultWeights,
	UsePST:     true,
	Tapered:    true,
	KingSafety: 0.01,
	Passers:    true,
	Mobility:   true,
}

func applyTacticalPenalty(b *board.Board, m game.Move, mover board.Color, score float64) float64 {
	opponent := mover.Other()

	if m.From != m.To {
		piece, _ := b.PieceAt(m.To)
		pt := piece.Type

		if pt > board.Pawn {
			if isAttackedAfterMove(b, m.To, m.From, mover) {
				w := defaultWeights[pt]
				if mover == board.White {
					score -= w
				} else {
					score += w
				}
			}
		}
	}

	// Penalize hanging major pieces of mover
	qBB := b.PieceBitboard(mover, board.Queen)
	for qBB != 0 {
		sqIdx := bits.TrailingZeros64(qBB)
		qBB &= qBB - 1
		sq := board.Sq{File: int8(sqIdx % 8), Rank: int8(sqIdx / 8)}
		if (b.IsAttackedBy(sq, opponent) && !b.IsAttackedBy(sq, mover)) || b.IsAttackedByLesserThan(sq, opponent, board.Queen) {
			if mover == board.White {
				score -= 8.0
			} else {
				score += 8.0
			}
			break
		}
	}
	rBB := b.PieceBitboard(mover, board.Rook)
	for rBB != 0 {
		sqIdx := bits.TrailingZeros64(rBB)
		rBB &= rBB - 1
		sq := board.Sq{File: int8(sqIdx % 8), Rank: int8(sqIdx / 8)}
		if (b.IsAttackedBy(sq, opponent) && !b.IsAttackedBy(sq, mover)) || b.IsAttackedByLesserThan(sq, opponent, board.Rook) {
			if mover == board.White {
				score -= 4.5
			} else {
				score += 4.5
			}
			break
		}
	}
	return score
}

func tacticalMaterialPayoff(b *board.Board, m game.Move, mover board.Color) float64 {
	wMat, wPST := materialAndPositional(b, board.White, defaultWeights, true, false, 1.0, &defaultPSTScale)
	bMat, bPST := materialAndPositional(b, board.Black, defaultWeights, true, false, 1.0, &defaultPSTScale)
	diff := (wMat + wPST) - (bMat + bPST)

	opponent := mover.Other()
	if b.IsInCheck(opponent) {
		if mover == board.White {
			diff += 0.3
		} else {
			diff -= 0.3
		}
	}

	diff = applyTacticalPenalty(b, m, mover, diff)
	return 1.0 / (1.0 + math.Exp(-diff/2.5))
}

func isAttackedAfterMove(b *board.Board, to board.Sq, from board.Sq, color board.Color) bool {
	piece, ok := b.PieceAt(from)
	if !ok {
		piece, _ = b.PieceAt(to)
	}
	opp := color.Other()
	if b.IsAttackedByExcluding(to, color, from) {
		return b.IsAttackedByLesserThan(to, opp, piece.Type)
	}
	return b.IsAttackedBy(to, opp)
}

func givesDirectCheck(b *board.Board, m game.Move, moving board.Piece, opponent board.Color) bool {
	kSq := b.KingSquare(opponent)
	kIdx := uint8(kSq.Rank*8 + kSq.File)
	toIdx := uint8(m.To.Rank*8 + m.To.File)
	fromIdx := uint8(m.From.Rank*8 + m.From.File)

	switch moving.Type {
	case board.Knight:
		return (board.KnightAttacks[toIdx] & (1 << kIdx)) != 0
	case board.Pawn:
		return (board.PawnAttacksTo[opponent][kIdx] & (1 << toIdx)) != 0
	case board.Bishop:
		occ := (b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)) &^ (1 << fromIdx) | (1 << toIdx)
		return (board.BishopAttacks(toIdx, occ) & (1 << kIdx)) != 0
	case board.Rook:
		occ := (b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)) &^ (1 << fromIdx) | (1 << toIdx)
		return (board.RookAttacks(toIdx, occ) & (1 << kIdx)) != 0
	case board.Queen:
		occ := (b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)) &^ (1 << fromIdx) | (1 << toIdx)
		return ((board.BishopAttacks(toIdx, occ) | board.RookAttacks(toIdx, occ)) & (1 << kIdx)) != 0
	}
	return false
}

func givesDiscoveredCheck(b *board.Board, from, to board.Sq, color board.Color) bool {
	opp := color.Other()
	kSq := b.KingSquare(opp)
	df := int(from.File) - int(kSq.File)
	dr := int(from.Rank) - int(kSq.Rank)

	stepF, stepR := 0, 0
	var t1, t2 board.PieceType

	if df == 0 && dr != 0 {
		stepF, stepR = 0, 1
		if dr < 0 {
			stepR = -1
		}
		t1, t2 = board.Rook, board.Queen
	} else if dr == 0 && df != 0 {
		stepF, stepR = 1, 0
		if df < 0 {
			stepF = -1
		}
		t1, t2 = board.Rook, board.Queen
	} else if df == dr || df == -dr {
		stepF = 1
		if df < 0 {
			stepF = -1
		}
		stepR = 1
		if dr < 0 {
			stepR = -1
		}
		t1, t2 = board.Bishop, board.Queen
	} else {
		return false
	}

	dfTo := int(to.File) - int(kSq.File)
	drTo := int(to.Rank) - int(kSq.Rank)
	if dfTo*stepR == drTo*stepF && (dfTo*stepF > 0 || drTo*stepR > 0) {
		return false
	}

	currF := int(kSq.File) + stepF
	currR := int(kSq.Rank) + stepR
	for currF != int(from.File) || currR != int(from.Rank) {
		if _, occ := b.PieceAt(board.Sq{File: int8(currF), Rank: int8(currR)}); occ {
			return false
		}
		currF += stepF
		currR += stepR
	}
	return b.HitsSlider(from, stepF, stepR, color, t1, t2)
}

func givesCheck(b *board.Board, m game.Move, moving board.Piece, opponent board.Color) bool {
	if givesDirectCheck(b, m, moving, opponent) {
		return true
	}
	return givesDiscoveredCheck(b, m.From, m.To, moving.Color)
}

func orderMovesForMCTS(moves []game.Move, b *board.Board) {
	if len(moves) <= 1 {
		return
	}
	var scoresBuf [128]int16
	var scores []int16
	if len(moves) <= len(scoresBuf) {
		scores = scoresBuf[:len(moves)]
	} else {
		scores = make([]int16, len(moves))
	}
	for i, m := range moves {
		sc := int16(0)
		moving, _ := b.PieceAt(m.From)
		captured, isCap := b.PieceAt(m.To)
		opponent := moving.Color.Other()
		if moving.Type == board.Pawn && ((m.Promo == board.Queen) || (m.From.Rank == 6 && m.To.Rank == 7) || (m.From.Rank == 1 && m.To.Rank == 0)) {
			sc += 500
		}
		chk := givesCheck(b, m, moving, opponent)
		discChk := givesDiscoveredCheck(b, m.From, m.To, moving.Color)
		kSq := b.KingSquare(opponent)
		kingAdj := (m.To.File-kSq.File >= -1 && m.To.File-kSq.File <= 1) &&
			(m.To.Rank-kSq.Rank >= -1 && m.To.Rank-kSq.Rank <= 1)

		seeVal := see(b, m)
		if seeVal < 0 && !chk && !discChk {
			sc -= int16(-seeVal * 300)
		} else if seeVal > 0 {
			sc += int16(seeVal * 200)
		}

		if isCap {
			var isUnsafeCap bool
			if discChk {
				isUnsafeCap = kingAdj && !b.IsAttackedByExcluding(m.To, moving.Color, m.From)
			} else {
				isUnsafeCap = materialWeights[moving.Type] > materialWeights[captured.Type] && isAttackedAfterMove(b, m.To, m.From, moving.Color)
			}
			if discChk {
				sc += 2000 + int16(materialWeights[captured.Type]*10)
			} else if captured.Type >= board.Rook || materialWeights[moving.Type] <= materialWeights[captured.Type] {
				sc += 200 + int16(materialWeights[captured.Type]*10-materialWeights[moving.Type])
			} else {
				if !isUnsafeCap {
					sc += 200 + int16(materialWeights[captured.Type]*10-materialWeights[moving.Type])
				} else {
					sc -= 100
				}
			}
			if chk && !discChk {
				if !isUnsafeCap {
					sc += 300
				} else {
					sc -= 100
				}
			}
		} else {
			var isUnsafe bool
			if discChk {
				isUnsafe = kingAdj && !b.IsAttackedByExcluding(m.To, moving.Color, m.From)
			} else {
				sqIdx := m.To.Rank*8 + m.To.File
				isPawnAttacked := moving.Type > board.Pawn && (b.PieceBitboard(opponent, board.Pawn)&board.PawnAttacksTo[opponent][sqIdx]) != 0
				isUnsafe = isPawnAttacked || (moving.Type > board.Pawn && isAttackedAfterMove(b, m.To, m.From, moving.Color))
				if isPawnAttacked {
					sc -= 300
				} else if isUnsafe {
					sc -= 200
				}
			}
			if discChk {
				if !isUnsafe {
					sc += 1500
				} else {
					sc += 200
				}
			} else if chk {
				if !isUnsafe {
					sc += 300
				} else {
					sc -= 150
				}
			}
		}
		if moving.Type >= board.Knight && b.IsAttackedBy(m.From, opponent) {
			if !b.IsAttackedBy(m.From, moving.Color) || b.IsAttackedByLesserThan(m.From, opponent, moving.Type) {
				if seeVal >= 0 {
					sc += 400 + int16(materialWeights[moving.Type]*10)
				}
			}
		}
		if moving.Type == board.Pawn {
			if (moving.Color == board.White && m.To.Rank == 6) || (moving.Color == board.Black && m.To.Rank == 1) {
				sc += 150
			} else if (moving.Color == board.White && m.To.Rank == 5) || (moving.Color == board.Black && m.To.Rank == 2) {
				sc += 60
			} else if m.To.File >= 2 && m.To.File <= 5 && (m.To.Rank == 3 || m.To.Rank == 4) {
				sc += 30
			}
		} else if moving.Type == board.Knight {
			if m.To.File >= 2 && m.To.File <= 5 && m.To.Rank >= 2 && m.To.Rank <= 5 {
				sc += 20
			}
		} else if moving.Type == board.Bishop {
			if (m.From.Rank == 0 || m.From.Rank == 7) && m.To.Rank > 0 && m.To.Rank < 7 {
				sc += 20
			}
		} else if moving.Type == board.King {
			if m.From.File == 4 && (m.To.File == 6 || m.To.File == 2) {
				sc += 350
			} else if !isCap && !b.IsInCheck(moving.Color) {
				if b.PieceCount() > 10 {
					sc -= 250
				} else {
					kEnemy := b.KingSquare(opponent)
					dfOld := iabs(int(m.From.File - kEnemy.File))
					drOld := iabs(int(m.From.Rank - kEnemy.Rank))
					dfNew := iabs(int(m.To.File - kEnemy.File))
					drNew := iabs(int(m.To.Rank - kEnemy.Rank))
					distOld := dfOld
					if drOld > distOld {
						distOld = drOld
					}
					distNew := dfNew
					if drNew > distNew {
						distNew = drNew
					}
					if distNew < distOld {
						sc += 400
					} else if distNew == distOld {
						sc += 50
					} else {
						sc -= 50
					}
				}
			}
		}
		scores[i] = sc
	}
	for i := 1; i < len(moves); i++ {
		m, sc := moves[i], scores[i]
		j := i
		for j > 0 && scores[j-1] < sc {
			moves[j] = moves[j-1]
			scores[j] = scores[j-1]
			j--
		}
		moves[j] = m
		scores[j] = sc
	}
}


const mctsTableSize = 65536

var (
	invSqrtTable   [mctsTableSize]float64
	invVisitsTable [mctsTableSize]float64
	sqrtLogTable   [mctsTableSize]float64
	rankPriorTable [128]float32
)

func init() {
	for i := 1; i < mctsTableSize; i++ {
		invSqrtTable[i] = 1.0 / math.Sqrt(float64(i))
		invVisitsTable[i] = 1.0 / float64(i)
		sqrtLogTable[i] = math.Sqrt(math.Log(float64(i)))
	}
	for i := 0; i < 128; i++ {
		rankPriorTable[i] = float32(1.0 / math.Pow(float64(i+1), 0.75))
	}
}

func getSqrtLog(visits int32) float64 {
	if visits < mctsTableSize {
		return sqrtLogTable[visits]
	}
	return math.Sqrt(math.Log(float64(visits)))
}

func getInvTables(visits int32) (float64, float64) {
	if visits < mctsTableSize {
		return invVisitsTable[visits], invSqrtTable[visits]
	}
	fv := float64(visits)
	return 1.0 / fv, 1.0 / math.Sqrt(fv)
}

// MCTSv3 implementation: lazy movegen, flat first-child/next-sibling arena, zero-alloc sync.Pool.
type mctsNodeV3 struct {
	parent       *mctsNodeV3
	firstChild   *mctsNodeV3
	nextSibling  *mctsNodeV3
	move         game.Move
	board         board.Board
	visits        int32
	movesStart    int32
	movesUntried  int16
	movesCount    int16
	movesCaptures int16
	turn          board.Color
	isTerminal    bool
	expanded      bool
	whiteOutcome  float64
	wins          float64
}

func partitionCaptures(moves []game.Move, b *board.Board) int16 {
	var numCaps int16
	l, r := 0, len(moves)-1
	for l <= r {
		_, isCap := b.PieceAt(moves[r].To)
		if isCap {
			numCaps++
			r--
		} else {
			_, lCap := b.PieceAt(moves[l].To)
			if lCap {
				moves[l], moves[r] = moves[r], moves[l]
				numCaps++
				r--
			} else {
				l++
			}
		}
	}
	return numCaps
}

func (n *mctsNodeV3) selectChild(c float64) *mctsNodeV3 {
	factor := c * getSqrtLog(n.visits)
	bestScore := -1e9
	var best *mctsNodeV3
	for child := n.firstChild; child != nil; child = child.nextSibling {
		invV, invSqrt := getInvTables(child.visits)
		score := child.wins*invV + factor*invSqrt
		if score > bestScore {
			bestScore = score
			best = child
		}
	}
	return best
}

type mctsArenaV3 struct {
	nodes []mctsNodeV3
	moves []game.Move
}

func (a *mctsArenaV3) alloc() *mctsNodeV3 {
	if len(a.nodes) < cap(a.nodes) {
		a.nodes = a.nodes[:len(a.nodes)+1]
		n := &a.nodes[len(a.nodes)-1]
		*n = mctsNodeV3{}
		return n
	}
	a.nodes = append(a.nodes, mctsNodeV3{})
	return &a.nodes[len(a.nodes)-1]
}

var mctsArenaV3Pool = sync.Pool{
	New: func() any {
		return &mctsArenaV3{
			nodes: make([]mctsNodeV3, 0, 4096),
			moves: make([]game.Move, 0, 16384),
		}
	},
}

func hasAnyLegalMove(g *game.Game, arena *mctsArenaV3) bool {
	start := len(arena.moves)
	arena.moves = g.AppendLegalMoves(arena.moves, g.Turn)
	hasLegal := len(arena.moves) > start
	arena.moves = arena.moves[:start]
	return hasLegal
}

func rolloutV3(g *game.Game, maxPlies int, rng *fastRand) float64 {
	var buf [128]game.Move
	for ply := 0; ply < maxPlies; ply++ {
		if g.KingCaptured {
			if g.Turn == board.White {
				return 0.0
			}
			return 1.0
		}
		if g.HalfmoveClock >= 100 {
			return 0.5
		}
		moves := g.AppendLegalMoves(buf[:0], g.Turn)
		if len(moves) == 0 {
			if chessmoves.IsInCheck(&g.Board, g.Turn) {
				if g.Turn == board.White {
					return 0.0
				}
				return 1.0
			}
			return 0.5
		}
		m := moves[rng.intn(len(moves))]
		g.Apply(m)
	}
	return materialPayoff(&g.Board)
}

func runMCTSv3Worker(g *game.Game, cfg MCTSConfig, legalMoves []game.Move, seed uint64, maxSims int, arena *mctsArenaV3) *mctsNodeV3 {
	prng := fastRand(seed)
	if prng == 0 {
		prng = 0x853c49e6748fea9b
	}

	root := arena.alloc()
	root.board = g.Board
	root.turn = g.Turn
	root.expanded = true
	startIdx := len(arena.moves)
	arena.moves = append(arena.moves, legalMoves...)
	root.movesStart = int32(startIdx)
	root.movesCount = int16(len(legalMoves))
	root.movesUntried = int16(len(legalMoves))
	root.movesCaptures = partitionCaptures(arena.moves[startIdx:], &root.board)

	var deadline time.Time
	hasDeadline := cfg.TimeBudget > 0
	if hasDeadline {
		deadline = time.Now().Add(cfg.TimeBudget)
	}
	if maxSims <= 0 && !hasDeadline {
		maxSims = 1000
	}

	var simGame game.Game
	simGame.TrackRepetition = false

	for sim := 0; ; sim++ {
		if hasDeadline {
			if (sim&63) == 0 && time.Now().After(deadline) {
				break
			}
		} else if sim >= maxSims {
			break
		}

		// 1. Selection
		curr := root
		for !curr.isTerminal && curr.expanded && curr.movesUntried == 0 && curr.firstChild != nil {
			curr = curr.selectChild(cfg.Exploration)
		}

		// 2. Expansion
		if !curr.isTerminal {
			if !curr.expanded {
				mStart := len(arena.moves)
				simGame.Board = curr.board
				simGame.Turn = curr.turn
				arena.moves = simGame.AppendLegalMoves(arena.moves, curr.turn)
				count := int16(len(arena.moves) - mStart)
				curr.movesStart = int32(mStart)
				curr.movesCount = count
				curr.movesUntried = count
				curr.movesCaptures = partitionCaptures(arena.moves[mStart:], &curr.board)
				curr.expanded = true

				if count == 0 {
					curr.isTerminal = true
					if chessmoves.IsInCheck(&curr.board, curr.turn) {
						if curr.turn == board.White {
							curr.whiteOutcome = 0.0
						} else {
							curr.whiteOutcome = 1.0
						}
					} else {
						curr.whiteOutcome = 0.5
					}
				}
			}

			if !curr.isTerminal && curr.movesUntried > 0 {
				lastIdx := int(curr.movesUntried - 1)
				var pickIdx int
				if curr.movesCaptures > 0 {
					pickIdx = lastIdx
					curr.movesCaptures--
				} else {
					pickIdx = prng.intn(int(curr.movesUntried))
				}
				m := arena.moves[int(curr.movesStart)+pickIdx]
				arena.moves[int(curr.movesStart)+pickIdx] = arena.moves[int(curr.movesStart)+lastIdx]
				curr.movesUntried--

				child := arena.alloc()
				child.parent = curr
				child.move = m
				child.turn = curr.turn.Other()
				child.board = curr.board

				// Fast move apply directly on child.board
				movingPiece, _ := curr.board.PieceAt(m.From)
				captured, capturedOk := curr.board.PieceAt(m.To)
				child.board.MakeMove(m.From, m.To)
				if movingPiece.Type == board.Pawn {
					if (movingPiece.Color == board.White && m.To.Rank == 7) ||
						(movingPiece.Color == board.Black && m.To.Rank == 0) {
						promo := board.Queen
						if m.Promo == board.Knight || m.Promo == board.Bishop || m.Promo == board.Rook {
							promo = m.Promo
						}
						child.board.Place(m.To, board.Piece{Color: movingPiece.Color, Type: promo})
					}
				}

				if capturedOk && captured.Type == board.King {
					child.isTerminal = true
					if child.turn == board.White {
						child.whiteOutcome = 0.0
					} else {
						child.whiteOutcome = 1.0
					}
				} else if chessmoves.IsInCheck(&child.board, child.turn) {
					simGame.Board = child.board
					simGame.Turn = child.turn
					if !hasAnyLegalMove(&simGame, arena) {
						child.isTerminal = true
						if child.turn == board.White {
							child.whiteOutcome = 0.0
						} else {
							child.whiteOutcome = 1.0
						}
					}
				}

				child.nextSibling = curr.firstChild
				curr.firstChild = child
				curr = child
			}
		}

		// 3. Evaluation
		whiteScore := curr.whiteOutcome
		if !curr.isTerminal {
			whiteScore = tacticalMaterialPayoff(&curr.board, curr.move, curr.turn.Other())
		}

		// 4. Backpropagation
		for node := curr; node != nil; node = node.parent {
			node.visits++
			if node.parent != nil {
				if node.parent.turn == board.White {
					node.wins += whiteScore
				} else {
					node.wins += (1.0 - whiteScore)
				}
			}
		}
	}

	return root
}

func pickBestRootMoveV3(root *mctsNodeV3, legalMoves []game.Move) (game.Move, bool) {
	if root.firstChild == nil {
		return legalMoves[0], true
	}
	bestChild := root.firstChild
	bestVisits := bestChild.visits
	for child := root.firstChild.nextSibling; child != nil; child = child.nextSibling {
		if child.visits > bestVisits {
			bestChild = child
			bestVisits = child.visits
		}
	}
	return bestChild.move, true
}

// MCTSv3 selects the best move using Pure Monte Carlo Tree Search v3.
func MCTSv3(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	if cfg.Exploration <= 0 {
		cfg.Exploration = 1.41421356
	}

	threads := cfg.Threads
	if threads <= 0 {
		threads = 1
	}

	var baseSeed uint64
	if cfg.RNG != nil {
		baseSeed = uint64(cfg.RNG.Int63())
	} else {
		baseSeed = uint64(time.Now().UnixNano())
	}

	if threads <= 1 {
		arena := mctsArenaV3Pool.Get().(*mctsArenaV3)
		arena.nodes = arena.nodes[:0]
		arena.moves = arena.moves[:0]

		startIdx := len(arena.moves)
		arena.moves = g.AppendLegalMoves(arena.moves, g.Turn)
		legalMoves := arena.moves[startIdx:]
		if len(legalMoves) == 0 {
			mctsArenaV3Pool.Put(arena)
			return game.Move{}, false
		}
		if len(legalMoves) == 1 {
			m := legalMoves[0]
			mctsArenaV3Pool.Put(arena)
			return m, true
		}

		root := runMCTSv3Worker(g, cfg, legalMoves, baseSeed, cfg.Simulations, arena)
		m, ok := pickBestRootMoveV3(root, legalMoves)

		mctsArenaV3Pool.Put(arena)
		return m, ok
	}

	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}
	if len(legalMoves) == 1 {
		return legalMoves[0], true
	}

	workerRoots := make([]*mctsNodeV3, threads)
	workerArenas := make([]*mctsArenaV3, threads)
	var wg sync.WaitGroup
	wg.Add(threads)

	workerSims := 0
	if cfg.Simulations > 0 {
		workerSims = (cfg.Simulations + threads - 1) / threads
	}

	for i := 0; i < threads; i++ {
		arena := mctsArenaV3Pool.Get().(*mctsArenaV3)
		arena.nodes = arena.nodes[:0]
		arena.moves = arena.moves[:0]
		workerArenas[i] = arena

		go func(workerID int, a *mctsArenaV3) {
			defer wg.Done()
			seed := baseSeed + uint64(workerID)*0x9e3779b97f4a7c15 + 1
			workerRoots[workerID] = runMCTSv3Worker(g, cfg, legalMoves, seed, workerSims, a)
		}(i, arena)
	}
	wg.Wait()

	visits := make(map[game.Move]int32, len(legalMoves))
	for _, root := range workerRoots {
		if root != nil {
			for child := root.firstChild; child != nil; child = child.nextSibling {
				visits[child.move] += child.visits
			}
		}
	}

	for _, a := range workerArenas {
		mctsArenaV3Pool.Put(a)
	}

	bestMove := legalMoves[0]
	var bestVisits int32 = -1
	for _, m := range legalMoves {
		if v := visits[m]; v > bestVisits {
			bestVisits = v
			bestMove = m
		}
	}
	return bestMove, true
}

// MCTSv4: Zero-board-copy in-place Undo stack, 56-byte cache-hot nodes, fast bitboard captures, incremental material diff.
type mctsNodeV4 struct {
	parent       *mctsNodeV4
	firstChild   *mctsNodeV4
	nextSibling  *mctsNodeV4
	move         game.Move
	materialDiff int16
	movesUntried int16
	movesCount   int16
	movesCaps    int16
	movesStart   int32
	visits       int32
	turn         board.Color
	isTerminal   bool
	expanded     bool
	pad          byte
	whiteOutcome float32
	prior        float32
	wins         float64
}

func (n *mctsNodeV4) selectChild(c float64) *mctsNodeV4 {
	factor := c * getSqrtLog(n.visits)
	bestScore := -1e9
	var best *mctsNodeV4
	for child := n.firstChild; child != nil; child = child.nextSibling {
		invV, invSqrt := getInvTables(child.visits)
		score := child.wins*invV + factor*invSqrt*float64(child.prior)
		if score > bestScore {
			bestScore = score
			best = child
		}
	}
	return best
}

const mctsV4ChunkSize = 8192

type mctsArenaV4 struct {
	chunks    [][]mctsNodeV4
	currChunk int
	currIndex int
	moves     []game.Move
}

func (a *mctsArenaV4) reset() {
	a.currChunk = 0
	a.currIndex = 0
	a.moves = a.moves[:0]
}

func (a *mctsArenaV4) alloc() *mctsNodeV4 {
	if a.currChunk >= len(a.chunks) {
		a.chunks = append(a.chunks, make([]mctsNodeV4, mctsV4ChunkSize))
	}
	if a.currIndex >= mctsV4ChunkSize {
		a.currChunk++
		a.currIndex = 0
		if a.currChunk >= len(a.chunks) {
			a.chunks = append(a.chunks, make([]mctsNodeV4, mctsV4ChunkSize))
		}
	}
	n := &a.chunks[a.currChunk][a.currIndex]
	a.currIndex++
	*n = mctsNodeV4{}
	return n
}

func partitionCapturesFast(moves []game.Move, enemyBB uint64) int16 {
	var numCaps int16
	l, r := 0, len(moves)-1
	for l <= r {
		rIdx := moves[r].To.Rank*8 + moves[r].To.File
		if (enemyBB & (uint64(1) << rIdx)) != 0 {
			numCaps++
			r--
		} else {
			lIdx := moves[l].To.Rank*8 + moves[l].To.File
			if (enemyBB & (uint64(1) << lIdx)) != 0 {
				moves[l], moves[r] = moves[r], moves[l]
				numCaps++
				r--
			} else {
				l++
			}
		}
	}
	return numCaps
}

var mctsArenaV4Pool = sync.Pool{
	New: func() any {
		return &mctsArenaV4{
			chunks: [][]mctsNodeV4{make([]mctsNodeV4, mctsV4ChunkSize)},
			moves:  make([]game.Move, 0, 131072),
		}
	},
}

func initialMaterialDiff(b *board.Board) int16 {
	diff := 0
	for pt := board.Pawn; pt <= board.Queen; pt++ {
		w := materialWeights[pt]
		diff += w * bits.OnesCount64(b.PieceBitboard(board.White, pt))
		diff -= w * bits.OnesCount64(b.PieceBitboard(board.Black, pt))
	}
	return int16(diff)
}

func runMCTSv4Worker(g *game.Game, cfg MCTSConfig, legalMoves []game.Move, seed uint64, maxSims int, arena *mctsArenaV4) *mctsNodeV4 {
	if cfg.Exploration <= 0 {
		cfg.Exploration = 1.41421356
	}
	prng := fastRand(seed)
	if prng == 0 {
		prng = 0x853c49e6748fea9b
	}

	root := arena.alloc()
	root.turn = g.Turn
	root.expanded = true
	root.materialDiff = initialMaterialDiff(&g.Board)
	startIdx := len(arena.moves)
	arena.moves = append(arena.moves, legalMoves...)
	root.movesStart = int32(startIdx)
	root.movesCount = int16(len(legalMoves))
	root.movesUntried = int16(len(legalMoves))
	orderMovesForMCTS(arena.moves[startIdx:], &g.Board)

	var deadline time.Time
	hasDeadline := cfg.TimeBudget > 0
	if hasDeadline {
		deadline = time.Now().Add(cfg.TimeBudget)
	}
	if maxSims <= 0 && !hasDeadline {
		maxSims = 1000
	}

	var played map[uint64]int
	if g != nil && g.TrackRepetition {
		played = playedKeys(g)
	}

	var simGame game.Game
	simGame.TrackRepetition = false
	currBoard := g.Board
	var undos [256]board.Undo
	var pathKeys [256]uint64
	pathKeys[0] = zobristBoard(&g.Board, g.Turn)

	for sim := 0; ; sim++ {
		if hasDeadline {
			if (sim&63) == 0 && time.Now().After(deadline) {
				break
			}
		} else if sim >= maxSims {
			break
		}

		// 1. Selection
		curr := root
		undoDepth := 0
		for !curr.isTerminal && curr.expanded && curr.firstChild != nil {
			if curr.movesUntried > 0 {
				expandedCount := curr.movesCount - curr.movesUntried
				var allowed int16
				if curr == root {
					allowed = 3 + int16(math.Sqrt(float64(curr.visits)))
				} else {
					allowed = 1 + int16(math.Sqrt(math.Sqrt(float64(curr.visits))))
				}
				if expandedCount < allowed {
					break
				}
			}
			curr = curr.selectChild(cfg.Exploration)
			m := curr.move
			movingPiece, _ := currBoard.PieceAt(m.From)
			undos[undoDepth] = currBoard.MakeMove(m.From, m.To)
			undoDepth++
			if undoDepth < len(pathKeys) {
				pathKeys[undoDepth] = zobristBoard(&currBoard, curr.turn)
			}
			if movingPiece.Type == board.Pawn {
				if (movingPiece.Color == board.White && m.To.Rank == 7) ||
					(movingPiece.Color == board.Black && m.To.Rank == 0) {
					promo := board.Queen
					if m.Promo == board.Knight || m.Promo == board.Bishop || m.Promo == board.Rook {
						promo = m.Promo
					}
					currBoard.SetPiece(m.To, board.Piece{Color: movingPiece.Color, Type: promo})
				}
			}
		}

		// 2. Expansion
		if !curr.isTerminal {
			if !curr.expanded {
				mStart := len(arena.moves)
				simGame.Board = currBoard
				simGame.Turn = curr.turn
				arena.moves = simGame.AppendLegalMoves(arena.moves, curr.turn)
				count := int16(len(arena.moves) - mStart)
				curr.movesStart = int32(mStart)
				curr.movesCount = count
				curr.movesUntried = count
				orderMovesForMCTS(arena.moves[mStart:], &currBoard)
				curr.expanded = true

				if count == 0 {
					curr.isTerminal = true
					if chessmoves.IsInCheck(&currBoard, curr.turn) {
						if curr.turn == board.White {
							curr.whiteOutcome = 0.0
						} else {
							curr.whiteOutcome = 1.0
						}
					} else {
						curr.whiteOutcome = 0.5
					}
				}
			}

			if !curr.isTerminal && curr.movesUntried > 0 {
				pickIdx := int(curr.movesCount - curr.movesUntried)
				m := arena.moves[int(curr.movesStart)+pickIdx]
				curr.movesUntried--

				parentTurn := curr.turn
				parentMatDiff := curr.materialDiff

				child := arena.alloc()
				child.parent = curr
				child.move = m
				child.turn = parentTurn.Other()
				movingPiece, _ := currBoard.PieceAt(m.From)
				captured, capturedOk := currBoard.PieceAt(m.To)
				chk := givesCheck(&currBoard, m, movingPiece, parentTurn.Other())
				discChk := givesDiscoveredCheck(&currBoard, m.From, m.To, parentTurn)
				kSq := currBoard.KingSquare(parentTurn.Other())
				kingAdj := (m.To.File-kSq.File >= -1 && m.To.File-kSq.File <= 1) &&
					(m.To.Rank-kSq.Rank >= -1 && m.To.Rank-kSq.Rank <= 1)

				seeVal := see(&currBoard, m)
				if seeVal < 0 && !currBoard.IsInCheck(parentTurn) && !chk && !discChk {
					child.prior = 0.05
				} else if capturedOk {
					var isUnsafeCap bool
					if discChk {
						isUnsafeCap = kingAdj && !currBoard.IsAttackedByExcluding(m.To, parentTurn, m.From)
					} else {
						isUnsafeCap = materialWeights[movingPiece.Type] > materialWeights[captured.Type] && isAttackedAfterMove(&currBoard, m.To, m.From, parentTurn)
					}
					if currBoard.IsInCheck(parentTurn) {
						child.prior = 1.8
					} else if discChk {
						if !isUnsafeCap {
							child.prior = 2.0
						} else {
							child.prior = 1.4
						}
					} else if chk && !isUnsafeCap {
						child.prior = 1.6
					} else if seeVal > 0 {
						child.prior = 1.6 + float32(seeVal)*0.1
					} else if !isUnsafeCap && (captured.Type >= board.Rook || materialWeights[movingPiece.Type] <= materialWeights[captured.Type]) {
						child.prior = 1.5
					} else if !isUnsafeCap && captured.Type >= board.Knight {
						child.prior = 1.2
					} else if chk {
						child.prior = 1.2
					} else {
						child.prior = 0.1
					}
				} else if currBoard.IsInCheck(parentTurn) {
					if movingPiece.Type == board.King {
						child.prior = 0.05
					} else {
						child.prior = 1.0
					}
				} else {
					var isUnsafe bool
					if discChk {
						isUnsafe = kingAdj && !currBoard.IsAttackedByExcluding(m.To, parentTurn, m.From)
					} else {
						isUnsafe = movingPiece.Type > board.Pawn && isAttackedAfterMove(&currBoard, m.To, m.From, parentTurn)
					}
					if discChk {
						if !isUnsafe {
							child.prior = 1.8
						} else {
							child.prior = 1.0
						}
					} else if chk {
						if !isUnsafe {
							child.prior = 1.6
						} else {
							child.prior = 0.1
						}
					} else if isUnsafe {
						child.prior = 0.1
					} else if movingPiece.Type == board.King {
						if m.From.File == 4 && (m.To.File == 6 || m.To.File == 2) {
							child.prior = 1.6
						} else if currBoard.PieceCount() <= 10 {
							kEnemy := currBoard.KingSquare(parentTurn.Other())
							dfOld := iabs(int(m.From.File - kEnemy.File))
							drOld := iabs(int(m.From.Rank - kEnemy.Rank))
							dfNew := iabs(int(m.To.File - kEnemy.File))
							drNew := iabs(int(m.To.Rank - kEnemy.Rank))
							distOld := dfOld
							if drOld > distOld {
								distOld = drOld
							}
							distNew := dfNew
							if drNew > distNew {
								distNew = drNew
							}
							if distNew < distOld {
								child.prior = 1.6
							} else if distNew == distOld {
								child.prior = 1.1
							} else {
								child.prior = 0.6
							}
						} else {
							child.prior = 0.05
						}
					} else if movingPiece.Type >= board.Knight && currBoard.IsAttackedBy(m.From, parentTurn.Other()) && (!currBoard.IsAttackedBy(m.From, parentTurn) || currBoard.IsAttackedByLesserThan(m.From, parentTurn.Other(), movingPiece.Type)) && seeVal >= 0 {
						child.prior = 1.7
					} else {
						child.prior = 1.0
					}
				}
				undos[undoDepth] = currBoard.MakeMove(m.From, m.To)
				undoDepth++

				matDiff := parentMatDiff
				if capturedOk {
					w := int16(materialWeights[captured.Type])
					if parentTurn == board.White {
						matDiff += w
					} else {
						matDiff -= w
					}
				}

				promo := board.PieceType(0)
				if movingPiece.Type == board.Pawn {
					if (movingPiece.Color == board.White && m.To.Rank == 7) ||
						(movingPiece.Color == board.Black && m.To.Rank == 0) {
						promo = board.Queen
						if m.Promo == board.Knight || m.Promo == board.Bishop || m.Promo == board.Rook {
							promo = m.Promo
						}
						currBoard.SetPiece(m.To, board.Piece{Color: movingPiece.Color, Type: promo})
						pw := int16(materialWeights[promo] - 1)
						if parentTurn == board.White {
							matDiff += pw
						} else {
							matDiff -= pw
						}
					}
				}

				child.materialDiff = matDiff

				childKey := zobristBoard(&currBoard, child.turn)
				if undoDepth < len(pathKeys) {
					pathKeys[undoDepth] = childKey
				}

				isRep := false
				if played != nil && played[childKey] >= 2 {
					isRep = true
				} else {
					for d := 0; d < undoDepth; d++ {
						if pathKeys[d] == childKey {
							isRep = true
							break
						}
					}
				}

				if isRep || (curr == root && g != nil && g.TrackRepetition && g.CountIfPlayed(m.From, m.To) >= 2) {
					child.isTerminal = true
					child.whiteOutcome = 0.5
				} else if capturedOk && captured.Type == board.King {
					child.isTerminal = true
					if child.turn == board.White {
						child.whiteOutcome = 0.0
					} else {
						child.whiteOutcome = 1.0
					}
				} else if chessmoves.IsInCheck(&currBoard, child.turn) {
					simGame.Board = currBoard
					simGame.Turn = child.turn
					mStart := len(arena.moves)
					arena.moves = simGame.AppendLegalMoves(arena.moves, simGame.Turn)
					hasLegal := len(arena.moves) > mStart
					arena.moves = arena.moves[:mStart]
					if !hasLegal {
						child.isTerminal = true
						if child.turn == board.White {
							child.whiteOutcome = 0.0
						} else {
							child.whiteOutcome = 1.0
						}
					} else if child.prior >= 0.5 && child.prior < 1.4 {
						child.prior = 1.4
					}
				}

				child.nextSibling = curr.firstChild
				curr.firstChild = child
				curr = child
			}
		}

		// 3. Evaluation
		whiteScore := float64(curr.whiteOutcome)
		if !curr.isTerminal {
			if cfg.Eval != nil && (cfg.Eval.HalfKP != nil || cfg.Eval.Net != nil) {
				score := PositionScoreEval(&currBoard, board.White, cfg.Eval)
				score = applyTacticalPenalty(&currBoard, curr.move, curr.turn.Other(), score)
				whiteScore = 1.0 / (1.0 + math.Exp(-score/2.5))
			} else {
				whiteScore = tacticalMaterialPayoff(&currBoard, curr.move, curr.turn.Other())
			}

			if currBoard.PieceCount() <= 6 {
				kWhite := currBoard.KingSquare(board.White)
				kBlack := currBoard.KingSquare(board.Black)
				if curr.materialDiff >= 300 {
					edgeDist := min(int(kBlack.File), 7-int(kBlack.File)) + min(int(kBlack.Rank), 7-int(kBlack.Rank))
					df := iabs(int(kWhite.File - kBlack.File))
					dr := iabs(int(kWhite.Rank - kBlack.Rank))
					kDist := df
					if dr > kDist {
						kDist = dr
					}
					matingProgress := float64(6-edgeDist)*0.015 + float64(7-kDist)*0.015
					whiteScore += matingProgress
					if whiteScore > 0.999 {
						whiteScore = 0.999
					}
				} else if curr.materialDiff <= -300 {
					edgeDist := min(int(kWhite.File), 7-int(kWhite.File)) + min(int(kWhite.Rank), 7-int(kWhite.Rank))
					df := iabs(int(kWhite.File - kBlack.File))
					dr := iabs(int(kWhite.Rank - kBlack.Rank))
					kDist := df
					if dr > kDist {
						kDist = dr
					}
					matingProgress := float64(6-edgeDist)*0.015 + float64(7-kDist)*0.015
					whiteScore -= matingProgress
					if whiteScore < 0.001 {
						whiteScore = 0.001
					}
				}
			}
		}

		// 4. Backpropagation
		for node := curr; node != nil; node = node.parent {
			node.visits++
			if node.parent != nil {
				if node.parent.turn == board.White {
					node.wins += whiteScore
				} else {
					node.wins += (1.0 - whiteScore)
				}
			}
		}

		// 5. Unmake moves back to root
		for undoDepth > 0 {
			undoDepth--
			currBoard.UnmakeMove(undos[undoDepth])
		}
	}

	return root
}

func pickBestRootMoveV4(g *game.Game, root *mctsNodeV4, legalMoves []game.Move) (game.Move, bool) {
	if root == nil || root.firstChild == nil {
		return legalMoves[0], true
	}
	var maxVisits int32
	for child := root.firstChild; child != nil; child = child.nextSibling {
		if child.visits > maxVisits {
			maxVisits = child.visits
		}
	}
	threshold := maxVisits / 4
	if threshold < 30 {
		threshold = 30
	}
	if threshold > maxVisits {
		threshold = maxVisits
	}

	bestChild := root.firstChild
	bestScore := -1e9
	for child := root.firstChild; child != nil; child = child.nextSibling {
		if child.visits < threshold && child.visits < maxVisits {
			continue
		}
		n := float64(child.visits)
		if child.isTerminal && child.visits > 0 {
			if child.wins == float64(child.visits) {
				score := 1e7 + float64(child.visits)
				if score > bestScore {
					bestChild = child
					bestScore = score
				}
				continue
			}
		}

		p := (child.wins + 1.0) / (n + 2.0)

		var worstOppRate float64
		allowsMate := false
		for rep := child.firstChild; rep != nil; rep = rep.nextSibling {
			if rep.isTerminal && rep.visits > 0 {
				if rep.wins == float64(rep.visits) {
					allowsMate = true
					break
				}
			}
			if rep.visits >= 15 || rep.visits >= child.visits/8 {
				oppRate := rep.wins / float64(rep.visits)
				if oppRate > worstOppRate {
					worstOppRate = oppRate
				}
			}
		}
		var score float64
		if allowsMate {
			score = -1e7 - float64(child.visits)
		} else {
			if worstOppRate > 0.45 {
				refutedP := 1.0 - worstOppRate
				if worstOppRate > 0.50 {
					refutedP -= (worstOppRate - 0.50) * 2.0
				}
				if refutedP < p {
					p = refutedP
				}
			}

			se := math.Sqrt(p * (1.0 - p) / (n + 2.0))
			score = p - 1.5*se

			if g != nil && g.TrackRepetition {
				rep := g.CountIfPlayed(child.move.From, child.move.To)
				played := playedKeys(g)
				if played != nil {
					var cb board.Board = g.Board
					cb.MakeMove(child.move.From, child.move.To)
					ck := zobristBoard(&cb, g.Turn.Other())
					if played[ck] > rep {
						rep = played[ck]
					}
				}
				if rep >= 2 {
					if p >= 0.45 {
						score -= 1e6
					}
				} else if rep >= 1 {
					if p >= 0.55 {
						score *= 0.1
					}
				}
			}
		}
		if score > bestScore || (score == bestScore && child.visits > bestChild.visits) {
			bestChild = child
			bestScore = score
		}
	}

	if bestScore < -1e6 {
		for child := root.firstChild; child != nil; child = child.nextSibling {
			allowsMate := false
			for rep := child.firstChild; rep != nil; rep = rep.nextSibling {
				if rep.isTerminal && rep.visits > 0 && rep.wins == float64(rep.visits) {
					allowsMate = true
					break
				}
			}
			if !allowsMate {
				n := float64(child.visits)
				p := (child.wins + 1.0) / (n + 2.0)
				if p > bestScore {
					bestScore = p
					bestChild = child
				}
			}
		}
	}

	return bestChild.move, true
}

// MCTSv4 selects the best move using Pure Monte Carlo Tree Search v4 (zero-board-copy Undo stack, incremental material diff, capture-prioritized).
func MCTSv4(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	if cfg.Exploration <= 0 {
		cfg.Exploration = 1.41421356
	}

	threads := cfg.Threads
	if threads <= 0 {
		threads = 1
	}

	var baseSeed uint64
	if cfg.RNG != nil {
		baseSeed = uint64(cfg.RNG.Int63())
	} else {
		baseSeed = uint64(time.Now().UnixNano())
	}

	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}
	if len(legalMoves) == 1 {
		return legalMoves[0], true
	}

	if threads <= 1 {
		arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
		arena.reset()

		root := runMCTSv4Worker(g, cfg, legalMoves, baseSeed, cfg.Simulations, arena)
		m, ok := pickBestRootMoveV4(g, root, legalMoves)
		mctsArenaV4Pool.Put(arena)
		return m, ok
	}

	workerRoots := make([]*mctsNodeV4, threads)
	workerArenas := make([]*mctsArenaV4, threads)
	var wg sync.WaitGroup
	wg.Add(threads)

	workerSims := 0
	if cfg.Simulations > 0 {
		workerSims = (cfg.Simulations + threads - 1) / threads
	}

	for i := 0; i < threads; i++ {
		arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
		arena.reset()
		workerArenas[i] = arena

		go func(workerID int, a *mctsArenaV4) {
			defer wg.Done()
			seed := baseSeed + uint64(workerID)*0x9e3779b97f4a7c15 + 1
			workerRoots[workerID] = runMCTSv4Worker(g, cfg, legalMoves, seed, workerSims, a)
		}(i, arena)
	}
	wg.Wait()

	var visitCounts [128]int32
	var winSums [128]float64
	var allowsMate [128]bool
	var maxVisits int32

	for _, root := range workerRoots {
		if root != nil {
			for child := root.firstChild; child != nil; child = child.nextSibling {
				for idx, m := range legalMoves {
					if m == child.move {
						visitCounts[idx] += child.visits
						winSums[idx] += child.wins
						if visitCounts[idx] > maxVisits {
							maxVisits = visitCounts[idx]
						}
						for rep := child.firstChild; rep != nil; rep = rep.nextSibling {
							if rep.isTerminal && rep.visits > 0 && rep.wins == float64(rep.visits) {
								allowsMate[idx] = true
								break
							}
						}
						break
					}
				}
			}
		}
	}

	for _, a := range workerArenas {
		mctsArenaV4Pool.Put(a)
	}

	hasPositive := false
	for idx := range legalMoves {
		if visitCounts[idx] >= 30 {
			v := float64(visitCounts[idx])
			rate := winSums[idx] / v
			if rate >= 0.50 {
				hasPositive = true
				break
			}
		}
	}

	bestMove := legalMoves[0]
	var bestScore float64 = -1e9
	var bestWin float64 = -1
	for idx, m := range legalMoves {
		if allowsMate[idx] {
			continue
		}
		v := float64(visitCounts[idx])
		w := winSums[idx]
		if v > 0 {
			rate := w / v
			if hasPositive && rate < 0.50 {
				v *= 0.1
			}
		}
		if g != nil && g.TrackRepetition {
			rep := g.CountIfPlayed(m.From, m.To)
			played := playedKeys(g)
			if played != nil {
				var cb board.Board = g.Board
				cb.MakeMove(m.From, m.To)
				ck := zobristBoard(&cb, g.Turn.Other())
				if played[ck] > rep {
					rep = played[ck]
				}
			}
			rate := w / (v + 1)
			if rep >= 2 {
				if rate >= 0.45 {
					v -= 1e6
				}
			} else if rep >= 1 {
				if rate >= 0.55 {
					v *= 0.1
				}
			}
		}
		if v > bestScore || (v == bestScore && w > bestWin) {
			bestScore = v
			bestWin = w
			bestMove = m
		}
	}
	return bestMove, true
}


// MCTSv1 runs the original unoptimized baseline pure MCTS implementation (single-threaded, fresh slices, 30-ply rollout).
func MCTSv1(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	legalMoves := g.AllLegalMoves(g.Turn)
	if len(legalMoves) == 0 {
		return game.Move{}, false
	}
	if len(legalMoves) == 1 {
		return legalMoves[0], true
	}
	if cfg.Exploration <= 0 {
		cfg.Exploration = 1.41421356
	}
	maxRollout := cfg.MaxRollout
	if maxRollout <= 0 {
		maxRollout = 30
	}
	rng := cfg.RNG
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	root := &mctsNode{
		turn:         g.Turn,
		untriedMoves: legalMoves,
	}
	var deadline time.Time
	hasDeadline := cfg.TimeBudget > 0
	if hasDeadline {
		deadline = time.Now().Add(cfg.TimeBudget)
	}
	maxSims := cfg.Simulations
	if maxSims <= 0 && !hasDeadline {
		maxSims = 1000
	}
	for sim := 0; ; sim++ {
		if hasDeadline {
			if (sim&63) == 0 && time.Now().After(deadline) {
				break
			}
		} else if sim >= maxSims {
			break
		}
		curr := root
		simGame := *g
		simGame.Board = g.Board.Clone()
		simGame.TrackRepetition = false

		for !curr.isTerminal && len(curr.untriedMoves) == 0 && len(curr.children) > 0 {
			curr = curr.selectChild(cfg.Exploration)
			simGame.Apply(curr.move)
		}
		if !curr.isTerminal && len(curr.untriedMoves) > 0 {
			lastIdx := len(curr.untriedMoves) - 1
			pickIdx := rng.Intn(len(curr.untriedMoves))
			m := curr.untriedMoves[pickIdx]
			curr.untriedMoves[pickIdx] = curr.untriedMoves[lastIdx]
			curr.untriedMoves = curr.untriedMoves[:lastIdx]

			simGame.Apply(m)
			childMoves := simGame.AllLegalMoves(simGame.Turn)
			child := newMCTSNode(curr, m, &simGame, childMoves)
			curr.children = append(curr.children, child)
			curr = child
		}
		whiteScore := curr.whiteOutcome
		if !curr.isTerminal {
			for ply := 0; ply < maxRollout; ply++ {
				if simGame.KingCaptured {
					if simGame.Turn == board.White {
						whiteScore = 0.0
					} else {
						whiteScore = 1.0
					}
					break
				}
				if simGame.HalfmoveClock >= 100 {
					whiteScore = 0.5
					break
				}
				moves := simGame.AllLegalMoves(simGame.Turn)
				if len(moves) == 0 {
					if chessmoves.IsInCheck(&simGame.Board, simGame.Turn) {
						if simGame.Turn == board.White {
							whiteScore = 0.0
						} else {
							whiteScore = 1.0
						}
					} else {
						whiteScore = 0.5
					}
					break
				}
				m := moves[rng.Intn(len(moves))]
				simGame.Apply(m)
				if ply == maxRollout-1 {
					whiteScore = materialPayoff(&simGame.Board)
				}
			}
		}
		for node := curr; node != nil; node = node.parent {
			node.visits++
			if node.parent != nil {
				if node.parent.turn == board.White {
					node.wins += whiteScore
				} else {
					node.wins += (1.0 - whiteScore)
				}
			}
		}
	}
	return pickBestRootMove(root, legalMoves)
}

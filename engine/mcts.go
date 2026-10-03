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

// MCTS selects the best move using Pure Monte Carlo Tree Search (defaults to latest MCTSv3).
func MCTS(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
	return MCTSv3(g, cfg)
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
		tbl[i+128] = 1.0 / (1.0 + math.Exp(-float64(i)/4.0))
	}
	return tbl
}()

func materialPayoff(b *board.Board) float64 {
	diff := 0
	for pt := board.Pawn; pt <= board.Queen; pt++ {
		w := materialWeights[pt]
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

func tacticalMaterialPayoff(b *board.Board, m game.Move, mover board.Color) float64 {
	diff := 0
	for pt := board.Pawn; pt <= board.Queen; pt++ {
		w := materialWeights[pt]
		diff += w * bits.OnesCount64(b.PieceBitboard(board.White, pt))
		diff -= w * bits.OnesCount64(b.PieceBitboard(board.Black, pt))
	}

	if m.From != m.To {
		opponent := mover.Other()
		sqIdx := m.To.Rank*8 + m.To.File
		piece, _ := b.PieceAt(m.To)
		pt := piece.Type

		if pt > board.Pawn {
			w := materialWeights[pt]
			enemyPawns := b.PieceBitboard(opponent, board.Pawn)
			if (enemyPawns & board.PawnAttacksTo[opponent][sqIdx]) != 0 {
				if mover == board.White {
					diff -= w
				} else {
					diff += w
				}
			} else if pt > board.Knight {
				enemyKnights := b.PieceBitboard(opponent, board.Knight)
				if (enemyKnights & board.KnightAttacks[sqIdx]) != 0 {
					if mover == board.White {
						diff -= (w - 3)
					} else {
						diff += (w - 3)
					}
				}
			}
		}
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

const mctsTableSize = 2048

var (
	invSqrtTable   [mctsTableSize]float64
	invVisitsTable [mctsTableSize]float64
	sqrtLogTable   [mctsTableSize]float64
)

func init() {
	for i := 1; i < mctsTableSize; i++ {
		invSqrtTable[i] = 1.0 / math.Sqrt(float64(i))
		invVisitsTable[i] = 1.0 / float64(i)
		sqrtLogTable[i] = math.Sqrt(math.Log(float64(i)))
	}
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
	factor := c * sqrtLogTable[n.visits&2047]
	bestScore := -1e9
	var best *mctsNodeV3
	for child := n.firstChild; child != nil; child = child.nextSibling {
		v := child.visits & 2047
		score := child.wins*invVisitsTable[v] + factor*invSqrtTable[v]
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

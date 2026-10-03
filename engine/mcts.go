package engine

import (
	"math"
	"math/bits"
	"math/rand"
	"runtime"
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

// MCTS selects the best move using Pure Monte Carlo Tree Search.
func MCTS(g *game.Game, cfg MCTSConfig) (game.Move, bool) {
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
		cfg.MaxRollout = 10
	}

	threads := cfg.Threads
	if threads <= 0 {
		if cfg.RNG != nil || (cfg.Simulations > 0 && cfg.Simulations < 2000) {
			threads = 1
		} else {
			threads = runtime.GOMAXPROCS(0)
			if threads > 8 {
				threads = 8
			}
		}
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

func runMCTSWorker(g *game.Game, cfg MCTSConfig, legalMoves []game.Move, seed uint64, maxSims int) *mctsNode {
	prng := fastRand(seed)
	if prng == 0 {
		prng = 0x853c49e6748fea9b
	}

	root := newMCTSNode(nil, game.Move{}, g, legalMoves)

	var deadline time.Time
	hasDeadline := cfg.TimeBudget > 0
	if hasDeadline {
		deadline = time.Now().Add(cfg.TimeBudget)
	}
	if maxSims <= 0 && !hasDeadline {
		maxSims = 1000
	}

	moveBuf := make([]game.Move, 0, 256)

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
		var simGame game.Game
		simGame = *g
		simGame.Board = g.Board.Clone()
		simGame.TrackRepetition = false

		for !curr.isTerminal && len(curr.untriedMoves) == 0 && len(curr.children) > 0 {
			curr = curr.selectChild(cfg.Exploration)
			simGame.Apply(curr.move)
		}

		// 2. Expansion
		if !curr.isTerminal && len(curr.untriedMoves) > 0 {
			lastIdx := len(curr.untriedMoves) - 1
			pickIdx := prng.intn(len(curr.untriedMoves))
			m := curr.untriedMoves[pickIdx]
			curr.untriedMoves[pickIdx] = curr.untriedMoves[lastIdx]
			curr.untriedMoves = curr.untriedMoves[:lastIdx]

			simGame.Apply(m)
			childMoves := simGame.AllLegalMoves(simGame.Turn)
			child := newMCTSNode(curr, m, &simGame, childMoves)
			curr.children = append(curr.children, child)
			curr = child
		}

		// 3. Simulation (Rollout)
		whiteScore := curr.whiteOutcome
		if !curr.isTerminal {
			whiteScore = rollout(&simGame, cfg.MaxRollout, &prng, moveBuf)
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

func newMCTSNode(parent *mctsNode, m game.Move, g *game.Game, legalMoves []game.Move) *mctsNode {
	node := &mctsNode{
		parent:       parent,
		move:         m,
		turn:         g.Turn,
		untriedMoves: legalMoves,
		children:     make([]*mctsNode, 0, len(legalMoves)),
	}

	if g.KingCaptured {
		node.isTerminal = true
		if g.Turn == board.White {
			node.whiteOutcome = 0.0
		} else {
			node.whiteOutcome = 1.0
		}
		node.untriedMoves = nil
		return node
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
		return node
	}

	if g.HalfmoveClock >= 100 {
		node.isTerminal = true
		node.whiteOutcome = 0.5
		node.untriedMoves = nil
		return node
	}

	return node
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

func rollout(g *game.Game, maxPlies int, rng *fastRand, moveBuf []game.Move) float64 {
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
		moves := g.AppendLegalMoves(moveBuf[:0], g.Turn)
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

func materialPayoff(b *board.Board) float64 {
	weights := [6]float64{
		board.Pawn: 1.0, board.Knight: 3.0, board.Bishop: 3.0,
		board.Rook: 5.0, board.Queen: 9.0, board.King: 0.0,
	}
	diff := 0.0
	for pt := board.Pawn; pt <= board.Queen; pt++ {
		w := weights[pt]
		diff += w * float64(bits.OnesCount64(b.PieceBitboard(board.White, pt)))
		diff -= w * float64(bits.OnesCount64(b.PieceBitboard(board.Black, pt)))
	}
	return 1.0 / (1.0 + math.Exp(-diff/4.0))
}

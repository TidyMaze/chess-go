package engine

import (
	"math"
	"math/rand"
	"time"

	"chess/board"
	"chess/game"
)

// MCTSConfig configures the Monte Carlo Tree Search.
type MCTSConfig struct {
	Simulations int           // number of iterations (if TimeBudget <= 0)
	TimeBudget  time.Duration // max search duration
	MaxRollout  int           // max plies per simulation rollout
	Exploration float64       // UCB1 exploration constant C
	RNG         *rand.Rand    // random source
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
		cfg.MaxRollout = 30
	}
	rng := cfg.RNG
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	root := newMCTSNode(nil, game.Move{}, g, legalMoves)

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

		// 1. Selection
		curr := root
		simGame := cloneGame(g)

		for !curr.isTerminal && len(curr.untriedMoves) == 0 && len(curr.children) > 0 {
			curr = curr.selectChild(cfg.Exploration)
			simGame.Apply(curr.move)
		}

		// 2. Expansion
		if !curr.isTerminal && len(curr.untriedMoves) > 0 {
			// Pick untried move
			lastIdx := len(curr.untriedMoves) - 1
			pickIdx := rng.Intn(len(curr.untriedMoves))
			m := curr.untriedMoves[pickIdx]
			curr.untriedMoves[pickIdx] = curr.untriedMoves[lastIdx]
			curr.untriedMoves = curr.untriedMoves[:lastIdx]

			simGame.Apply(m)
			childMoves := simGame.AllLegalMoves(simGame.Turn)
			child := newMCTSNode(curr, m, simGame, childMoves)
			curr.children = append(curr.children, child)
			curr = child
		}

		// 3. Simulation (Rollout)
		whiteScore := curr.whiteOutcome
		if !curr.isTerminal {
			whiteScore = rollout(simGame, cfg.MaxRollout, rng)
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

	if len(root.children) == 0 {
		return legalMoves[0], true
	}

	// Choose most visited child
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

func newMCTSNode(parent *mctsNode, m game.Move, g *game.Game, legalMoves []game.Move) *mctsNode {
	node := &mctsNode{
		parent:       parent,
		move:         m,
		turn:         g.Turn,
		untriedMoves: legalMoves,
	}

	if g.KingCaptured {
		node.isTerminal = true
		if g.Turn == board.White {
			node.whiteOutcome = 0.0 // White's king captured
		} else {
			node.whiteOutcome = 1.0 // Black's king captured
		}
		node.untriedMoves = nil
		return node
	}

	if len(legalMoves) == 0 {
		node.isTerminal = true
		if g.IsCheckmate(g.Turn) {
			if g.Turn == board.White {
				node.whiteOutcome = 0.0 // White checkmated
			} else {
				node.whiteOutcome = 1.0 // Black checkmated
			}
		} else {
			node.whiteOutcome = 0.5 // Stalemate
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
	logParent := math.Log(float64(n.visits))
	bestScore := -1e9
	var best *mctsNode
	for _, child := range n.children {
		if child.visits == 0 {
			return child
		}
		q := child.wins / float64(child.visits)
		u := c * math.Sqrt(logParent/float64(child.visits))
		score := q + u
		if score > bestScore {
			bestScore = score
			best = child
		}
	}
	return best
}

func rollout(g *game.Game, maxPlies int, rng *rand.Rand) float64 {
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
		moves := g.AllLegalMoves(g.Turn)
		if len(moves) == 0 {
			if g.IsCheckmate(g.Turn) {
				if g.Turn == board.White {
					return 0.0
				}
				return 1.0
			}
			return 0.5
		}
		m := moves[rng.Intn(len(moves))]
		g.Apply(m)
	}

	// Material evaluation payoff if rollout limit reached
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
		diff += w * float64(bitsCount(b.PieceBitboard(board.White, pt)))
		diff -= w * float64(bitsCount(b.PieceBitboard(board.Black, pt)))
	}
	// Sigmoid mapping (-inf, +inf) to (0, 1), 0 diff = 0.5
	return 1.0 / (1.0 + math.Exp(-diff/4.0))
}

func bitsCount(bb uint64) int {
	count := 0
	for bb != 0 {
		count++
		bb &= bb - 1
	}
	return count
}

func cloneGame(g *game.Game) *game.Game {
	cp := *g
	cp.Board = g.Board.Clone()
	cp.TrackRepetition = false
	return &cp
}

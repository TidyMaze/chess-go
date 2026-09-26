package engine

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"chess/game"
)

// ServeUCI speaks enough of the UCI protocol for this engine to sit on the
// far side of a match. The harness races players in one process, so two
// builds of the engine could never meet; behind stdin/stdout a build is
// just another engine to the Stockfish client, and a speedup can be
// measured as Elo instead of as nodes.
//
// Handled: uci, isready, position, go depth N, go movetime N, quit, and
// setoption name Seed value N, which seeds the tie-break before every search.
// Every other command is accepted and ignored.
func ServeUCI(in io.Reader, out io.Writer, p Player) {
	g := game.New()
	seeded, seed := false, int64(0)
	// One table for the whole game, as the in-process harness gives its
	// players; a fresh table per move would handicap the UCI side.
	var tt *TranspositionTable
	if p.TTBits > 0 {
		tt = NewTranspositionTable(p.TTBits)
	}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "uci":
			fmt.Fprintln(out, "id name chess-go")
			fmt.Fprintln(out, "uciok")
		case "isready":
			fmt.Fprintln(out, "readyok")
		case "ucinewgame":
			if tt != nil {
				tt = NewTranspositionTable(p.TTBits)
			}
		case "setoption":
			// setoption name Seed value N
			if len(fields) == 5 && fields[1] == "name" && strings.EqualFold(fields[2], "Seed") && fields[3] == "value" {
				if n, err := strconv.ParseInt(fields[4], 10, 64); err == nil {
					seeded, seed = true, n
				}
			}
		case "position":
			g = uciPosition(fields[1:])
		case "go":
			q := p
			q.TimeBudget = 0
			for i := 1; i+1 < len(fields); i += 2 {
				n, _ := strconv.Atoi(fields[i+1])
				switch fields[i] {
				case "movetime":
					q.TimeBudget = time.Duration(n) * time.Millisecond
				case "depth":
					q.Depth = n
				}
			}
			if seeded {
				SeedRandom(seed)
			}
			ResetNodes()
			started := time.Now()
			m, score, ok := q.pickScored(g, tt)
			elapsed := time.Since(started)
			if !ok {
				fmt.Fprintln(out, "bestmove (none)")
				continue
			}
			// The score travels with the move so the harness on the other
			// side can adjudicate on it; without it, it would see zero.
			if !math.IsNaN(score) {
				// nodes, nps and time are what make a search comparable with
				// another engine's on the same position: depth alone says
				// nothing about how large a tree was spent reaching it.
				nodes := TotalNodes()
				ms := elapsed.Milliseconds()
				nps := int64(0)
				if s := elapsed.Seconds(); s > 0 {
					nps = int64(float64(nodes) / s)
				}
				// Before the depth line: the harness reads the score from the
				// line just above bestmove.
				cutoff := 0
				if LastMoveFromCutOffIteration() {
					cutoff = 1
				}
				fmt.Fprintf(out, "info string cutoff %d\n", cutoff)
				fmt.Fprintf(out, "info depth %d score %s nodes %d nps %d time %d\n",
					LastSearchDepth(), uciScore(score), nodes, nps, ms)
			}
			fmt.Fprintln(out, "bestmove "+m.UCI())
		case "quit":
			return
		}
	}
}

// uciScore formats a score in pawns from the mover's side as UCI wants it:
// "mate N" in moves for a mate (negative when the mover is the one mated),
// "cp N" otherwise. A mate printed as centipawns read as cp 99500, which
// says nothing about how far away it is. The harness and the loss audit
// already parse both forms.
func uciScore(score float64) string {
	if math.Abs(score) >= mateBound {
		plies := int(math.Round(mateScore - math.Abs(score)))
		moves := (plies + 1) / 2
		if score < 0 {
			moves = -moves
		}
		return fmt.Sprintf("mate %d", moves)
	}
	return fmt.Sprintf("cp %d", int(math.Round(score*100)))
}

// uciPosition parses the arguments of a position command: startpos or a
// six-field FEN, then optionally "moves" and a list of UCI moves. A bad
// FEN leaves the start position; a bad move is skipped. A promotion
// suffix is accepted and the pawn becomes a queen, the only promotion
// this engine plays itself.
func uciPosition(fields []string) *game.Game {
	g := game.New()
	i := 0
	if len(fields) > 0 && fields[0] == "fen" {
		end := 1
		for end < len(fields) && fields[end] != "moves" {
			end++
		}
		if parsed, err := game.ParseFEN(strings.Join(fields[1:end], " ")); err == nil {
			g = parsed
		}
		i = end
	} else if len(fields) > 0 && fields[0] == "startpos" {
		i = 1
	}
	if i < len(fields) && fields[i] == "moves" {
		for _, s := range fields[i+1:] {
			if m, ok := game.MoveFromUCI(s); ok {
				g.ApplyMove(m.From, m.To)
			}
		}
	}
	return g
}

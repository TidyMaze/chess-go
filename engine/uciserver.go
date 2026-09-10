package engine

import (
	"bufio"
	"fmt"
	"io"
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
// Handled: uci, isready, position, go depth N, go movetime N, quit. Every
// other command, setoption included, is accepted and ignored.
func ServeUCI(in io.Reader, out io.Writer, p Player) {
	g := game.New()
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
			m, _, ok := q.pickScored(g, tt)
			if !ok {
				fmt.Fprintln(out, "bestmove (none)")
				continue
			}
			fmt.Fprintln(out, "bestmove "+m.UCI())
		case "quit":
			return
		}
	}
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

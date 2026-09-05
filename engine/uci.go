package engine

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"chess/game"
)

// UCIEngine drives an external UCI engine (Stockfish) as an opponent, so
// this engine's rating can be pinned to a real, externally-calibrated
// reference instead of only to its own weaker ancestors. A ladder
// anchored at "random mover = 0" is internally consistent but says
// nothing about where the engine sits in absolute terms.
type UCIEngine struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

// NewStockfish starts Stockfish limited to a given skill level and
// search depth. Skill level 0 is its weakest setting.
func NewStockfish(path string, skill, elo int) (*UCIEngine, error) {
	cmd := exec.Command(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	e := &UCIEngine{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}

	e.send("uci")
	e.waitFor("uciok")
	e.send(fmt.Sprintf("setoption name Skill Level value %d", skill))
	if elo > 0 {
		e.send("setoption name UCI_LimitStrength value true")
		e.send(fmt.Sprintf("setoption name UCI_Elo value %d", elo))
	}
	e.send("isready")
	e.waitFor("readyok")
	return e, nil
}

func (e *UCIEngine) send(cmd string) {
	fmt.Fprintf(e.stdin, "%s\n", cmd)
}

func (e *UCIEngine) waitFor(token string) string {
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return ""
		}
		if strings.HasPrefix(strings.TrimSpace(line), token) {
			return strings.TrimSpace(line)
		}
	}
}

// BestMove asks the external engine for its move in the given position.
func (e *UCIEngine) BestMove(g *game.Game, depth int) (game.Move, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.send("position fen " + g.FEN())
	e.send(fmt.Sprintf("go depth %d", depth))
	line := e.waitFor("bestmove")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] == "(none)" {
		return game.Move{}, false
	}
	m, ok := game.MoveFromUCI(fields[1])
	if !ok {
		return game.Move{}, false
	}
	// The external engine may return a move that is legal in real chess
	// but not in this engine's rule subset (castling, en passant). Reject
	// anything not in our own legal list rather than corrupting the board.
	for _, legal := range g.AllLegalMoves(g.Turn) {
		if legal == m {
			return m, true
		}
	}
	return game.Move{}, false
}

// LegalMoves asks the external engine to enumerate the legal moves in a
// position ("go perft 1" lists each move). Used to validate this
// engine's own move generation against a reference implementation.
//
// This engine implements a rule subset: no castling, no en passant, and
// promotion always to a queen. FEN() therefore always reports castling
// rights and en-passant as unavailable, so the reference engine is asked
// about exactly the same position -- and the only expected difference is
// that it lists under-promotions (=r/b/n) which this engine never makes.
func (e *UCIEngine) LegalMoves(g *game.Game) (map[string]bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.send("position fen " + g.FEN())
	e.send("go perft 1")

	out := map[string]bool{}
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Nodes searched") {
			return out, nil
		}
		if i := strings.Index(line, ":"); i > 0 {
			mv := strings.TrimSpace(line[:i])
			// Only accept things shaped like a UCI move: the engine also
			// prints "info string ..." lines that contain colons.
			if !looksLikeUCIMove(mv) {
				continue
			}
			// Collapse under-promotions onto the queen promotion this
			// engine would play, so the sets are comparable.
			if len(mv) == 5 {
				mv = mv[:4] + "q"
			}
			out[mv] = true
		}
	}
}

func looksLikeUCIMove(s string) bool {
	if len(s) != 4 && len(s) != 5 {
		return false
	}
	if s[0] < 'a' || s[0] > 'h' || s[2] < 'a' || s[2] > 'h' {
		return false
	}
	if s[1] < '1' || s[1] > '8' || s[3] < '1' || s[3] > '8' {
		return false
	}
	if len(s) == 5 {
		switch s[4] {
		case 'q', 'r', 'b', 'n':
		default:
			return false
		}
	}
	return true
}

func (e *UCIEngine) Close() {
	e.send("quit")
	_ = e.cmd.Wait()
}

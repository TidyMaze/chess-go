package engine

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"chess/game"
)

// UCIEngine drives an external UCI engine (Stockfish) as an opponent, so
// this engine's rating can be pinned to a real, externally-calibrated
// reference instead of only to its own weaker ancestors. A ladder
// anchored at "random mover = 0" is internally consistent but says
// nothing about where the engine sits in absolute terms.
//
// One UCIEngine is handed to every match worker, so it is a pool of
// processes rather than one process: a caller takes an idle process or
// spawns another, and gives it back once its command has answered. With
// one process behind a mutex, ten workers played one move at a time and a
// half-hour race was on course for five hours.
type UCIEngine struct {
	path       string
	skill, elo int
	mu         sync.Mutex
	idle, all  []*uciProc
	closed     bool
}

type uciProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// NewStockfish starts Stockfish limited to a given skill level and
// search depth. Skill level 0 is its weakest setting.
func NewStockfish(path string, skill, elo int) (*UCIEngine, error) {
	e := &UCIEngine{path: path, skill: skill, elo: elo}
	p, err := e.spawn()
	if err != nil {
		return nil, err
	}
	e.release(p)
	return e, nil
}

func (e *UCIEngine) spawn() (*uciProc, error) {
	cmd := exec.Command(e.path)
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
	p := &uciProc{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	p.send("uci")
	p.waitFor("uciok")
	p.send(fmt.Sprintf("setoption name Skill Level value %d", e.skill))
	if e.elo > 0 {
		p.send("setoption name UCI_LimitStrength value true")
		p.send(fmt.Sprintf("setoption name UCI_Elo value %d", e.elo))
	}
	p.send("isready")
	p.waitFor("readyok")
	e.mu.Lock()
	e.all = append(e.all, p)
	e.mu.Unlock()
	return p, nil
}

// acquire hands the caller an idle process, or a new one when every
// process is busy answering someone else.
func (e *UCIEngine) acquire() (*uciProc, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, fmt.Errorf("%s: engine closed", e.path)
	}
	if n := len(e.idle); n > 0 {
		p := e.idle[n-1]
		e.idle = e.idle[:n-1]
		e.mu.Unlock()
		return p, nil
	}
	e.mu.Unlock()
	return e.spawn()
}

func (e *UCIEngine) release(p *uciProc) {
	e.mu.Lock()
	e.idle = append(e.idle, p)
	e.mu.Unlock()
}

func (p *uciProc) send(cmd string) {
	fmt.Fprintf(p.stdin, "%s\n", cmd)
}

// waitFor reads lines until one starts with token, and returns it.
func (p *uciProc) waitFor(token string) string {
	for {
		line, err := p.stdout.ReadString('\n')
		if err != nil {
			return ""
		}
		if strings.HasPrefix(strings.TrimSpace(line), token) {
			return strings.TrimSpace(line)
		}
	}
}

// BestMove asks the external engine for its move in the given position.
//
// moveTimeMS, when positive, gives the engine a clock instead of a depth.
// A timed calibration has to hand both sides a clock: our engine under a
// budget against Stockfish at a fixed depth measures the budget, not the
// engine.
// BestMove is BestMoveScored without the score.
func (e *UCIEngine) BestMove(g *game.Game, depth, moveTimeMS int) (game.Move, bool) {
	m, _, ok := e.BestMoveScored(g, depth, moveTimeMS)
	return m, ok
}

// BestMoveScored asks for a move and returns with it the engine's own score
// for the position, in pawns from the side to move, taken from the last
// "info ... score" line before bestmove. NaN when the engine sent none: the
// adjudicator must see "no opinion" there, not a level position. A mate
// score is reported as 100 pawns either way.
func (e *UCIEngine) BestMoveScored(g *game.Game, depth, moveTimeMS int) (game.Move, float64, bool) {
	p, err := e.acquire()
	if err != nil {
		return game.Move{}, math.NaN(), false
	}
	defer e.release(p)

	p.send("position fen " + g.FEN())
	if moveTimeMS > 0 {
		p.send(fmt.Sprintf("go movetime %d", moveTimeMS))
	} else {
		p.send(fmt.Sprintf("go depth %d", depth))
	}
	// Everything up to bestmove is read here rather than skipped, so the
	// engine's own score for the position comes back with the move.
	score := math.NaN()
	var line string
	for {
		l, err := p.stdout.ReadString('\n')
		if err != nil {
			return game.Move{}, score, false
		}
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "bestmove") {
			line = l
			break
		}
		if s, ok := uciScorePawns(l); ok {
			score = s
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] == "(none)" {
		return game.Move{}, score, false
	}
	m, ok := game.MoveFromUCI(fields[1])
	if !ok {
		return game.Move{}, score, false
	}
	// The external engine may return a move that is legal in real chess
	// but not in this engine's rule subset (castling, en passant). Reject
	// anything not in our own legal list rather than corrupting the board.
	for _, legal := range g.AllLegalMoves(g.Turn) {
		if legal == m {
			return m, score, true
		}
	}
	return game.Move{}, score, false
}

// uciScorePawns reads the score out of an info line: "score cp N" in
// centipawns, "score mate N" as 100 pawns for whoever is mating.
func uciScorePawns(line string) (float64, bool) {
	i := strings.Index(line, " score ")
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(line[i+7:])
	if len(fields) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	switch fields[0] {
	case "cp":
		return float64(n) / 100, true
	case "mate":
		if n < 0 {
			return -100, true
		}
		return 100, true
	}
	return 0, false
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
	p, err := e.acquire()
	if err != nil {
		return nil, err
	}
	defer e.release(p)

	p.send("position fen " + g.FEN())
	p.send("go perft 1")

	out := map[string]bool{}
	for {
		line, err := p.stdout.ReadString('\n')
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

// Close quits every process the pool has spawned.
func (e *UCIEngine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.all {
		p.send("quit")
		_ = p.cmd.Wait()
	}
	e.all, e.idle, e.closed = nil, nil, true
}

// Evaluate asks the external engine what it thinks a position is worth,
// in pawns from the side-to-move's point of view, and reports whether it
// saw a forced mate.
//
// This is the teacher signal for distillation. Tuning against self-play
// results failed to produce Elo (-16 +/- 48 over 200 games), and the
// fitted values said why: between engines around 1900, the outcome of a
// game is a very noisy statement about a position, so terms that matter
// get scaled toward zero. A strong engine's evaluation of the same
// position is a far sharper label and costs one search per position
// instead of one whole game.
func (e *UCIEngine) Evaluate(g *game.Game, depth int) (pawns float64, mate bool, ok bool) {
	p, err := e.acquire()
	if err != nil {
		return 0, false, false
	}
	defer e.release(p)

	p.send("position fen " + g.FEN())
	p.send(fmt.Sprintf("go depth %d", depth))

	// Keep the score from the last "info" line before bestmove: that is
	// the deepest completed iteration.
	var lastCP int
	var sawCP, sawMate bool
	for {
		line, err := p.stdout.ReadString('\n')
		if err != nil {
			return 0, false, false
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bestmove") {
			break
		}
		i := strings.Index(line, " score ")
		if i < 0 {
			continue
		}
		fields := strings.Fields(line[i+7:])
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "cp":
			if n, err := strconv.Atoi(fields[1]); err == nil {
				lastCP, sawCP, sawMate = n, true, false
			}
		case "mate":
			sawMate, sawCP = true, false
		}
	}
	if sawMate {
		return 0, true, true
	}
	if !sawCP {
		return 0, false, false
	}
	return float64(lastCP) / 100, false, true
}

// StaticEval asks the external engine for its evaluation of a position
// with no search at all, in pawns from White's point of view.
//
// This is the right target for training a static evaluation. Fitting one
// to a depth-12 search score scored -338 Elo: a searched score contains
// tactics, and asking a function of piece placement to predict tactics
// leaves a residual it cannot learn (RMS 2.39 pawns, enough to swamp the
// evaluation it was correcting). A static score is a fair question to
// ask a static function, and it is far cheaper, needing no search.
func (e *UCIEngine) StaticEval(g *game.Game) (pawns float64, ok bool) {
	p, err := e.acquire()
	if err != nil {
		return 0, false
	}
	defer e.release(p)

	p.send("position fen " + g.FEN())
	p.send("eval")
	// "eval" has no terminator of its own, so a following isready gives
	// one: readyok cannot arrive before eval's output is written.
	p.send("isready")

	for {
		line, err := p.stdout.ReadString('\n')
		if err != nil {
			return 0, false
		}
		line = strings.TrimSpace(line)
		if line == "readyok" {
			return 0, false
		}
		if !strings.HasPrefix(line, "Final evaluation") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		v, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			// "none (in check)" and similar.
			continue
		}
		// Drain to readyok so the next command starts from a clean state.
		for {
			l, err := p.stdout.ReadString('\n')
			if err != nil || strings.TrimSpace(l) == "readyok" {
				break
			}
		}
		return v, true
	}
}

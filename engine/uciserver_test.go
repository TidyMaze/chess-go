package engine

import (
	"bytes"
	"strings"
	"testing"

	"chess/game"
)

// A second build of this engine is the one opponent the harness could not
// race: both players live in one process. Speaking UCI puts a build behind
// stdin/stdout, where the existing Stockfish client already knows how to
// talk to it, so a speedup can be measured as Elo instead of nodes.
func TestServeUCIPlaysAGameOverStdio(t *testing.T) {
	script := strings.Join([]string{
		"uci",
		"setoption name Skill Level value 20",
		"isready",
		"ucinewgame",
		"",
		"nonsense command",
		"position startpos moves e2e4",
		"go depth 2",
		"position fen 6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1",
		"go movetime 200",
		"position fen rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3",
		"go depth 1",
		"quit",
		"go depth 1",
	}, "\n")
	var out bytes.Buffer
	p := Strong(2)
	p.TTBits = 16
	ServeUCI(strings.NewReader(script), &out, p)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var bestmoves []string
	for _, l := range lines {
		if strings.HasPrefix(l, "bestmove ") {
			bestmoves = append(bestmoves, strings.TrimPrefix(l, "bestmove "))
		}
	}
	for _, want := range []string{"id name chess-go", "uciok", "readyok"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in the output:\n%s", want, out.String())
		}
	}
	if len(bestmoves) != 3 {
		t.Fatalf("three go commands before quit, %d bestmoves: %v", len(bestmoves), bestmoves)
	}
	infos := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "info depth ") && strings.Contains(l, " score cp ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "bestmove ") {
			infos++
		}
	}
	if infos != 2 {
		t.Errorf("each searched move must be preceded by an info line with its score; found %d of 2:\n%s", infos, out.String())
	}
	g := game.New()
	g.ApplyMove(game.Move{}.From, game.Move{}.To)
	after, _ := game.MoveFromUCI("e2e4")
	g = game.New()
	g.ApplyMove(after.From, after.To)
	first, ok := game.MoveFromUCI(bestmoves[0])
	legal := false
	for _, m := range g.AllLegalMoves(g.Turn) {
		legal = legal || (ok && m == first)
	}
	if !legal {
		t.Errorf("bestmove %q is not a legal black reply to e2e4", bestmoves[0])
	}
	if bestmoves[1] != "a1a8" {
		t.Errorf("mate in one is a1a8, got %q", bestmoves[1])
	}
	if bestmoves[2] != "(none)" {
		t.Errorf("a mated side has no move, got %q", bestmoves[2])
	}
}

// position: startpos or a FEN, optionally followed by moves; a promotion
// suffix is accepted and the pawn becomes a queen, which is the only
// promotion this engine ever plays itself.
func TestUCIPositionParsing(t *testing.T) {
	g := uciPosition(strings.Fields("fen 8/P6k/8/8/8/8/8/K7 w - - 0 1 moves a7a8q"))
	if got := g.FEN(); !strings.HasPrefix(got, "Q7/7k/8/8/8/8/8/K7 b") {
		t.Errorf("promotion not applied: %s", got)
	}
	if got := uciPosition(strings.Fields("fen this is not a fen")).FEN(); got != game.New().FEN() {
		t.Errorf("a bad FEN should leave the start position, got %s", got)
	}
	if got := uciPosition(strings.Fields("startpos moves e2e4 zz99")).FEN(); !strings.HasPrefix(got, "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b") {
		t.Errorf("startpos with one legal and one garbage move: %s", got)
	}
	if got := uciPosition(nil).FEN(); got != game.New().FEN() {
		t.Errorf("bare position should be the start position, got %s", got)
	}
}

// A player with no score to give (a non-iterative search) sends its move
// without an info line, rather than a made-up score.
func TestServeUCIOmitsTheScoreItDoesNotHave(t *testing.T) {
	var out bytes.Buffer
	ServeUCI(strings.NewReader("position startpos\ngo depth 1\nquit\n"), &out, Player{Depth: 1, UsePST: true})
	if strings.Contains(out.String(), "info ") || !strings.Contains(out.String(), "bestmove ") {
		t.Errorf("expected a bare bestmove, got:\n%s", out.String())
	}
}

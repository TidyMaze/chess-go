// Command play serves the browser UI and lets a human play the current
// best engine, while training and measurement runs carry on in other
// processes.
//
// It also replaces the static file server the UI was being viewed
// through, so there is one thing to start rather than two.
//
// The board state lives in the browser and arrives with every request as
// a FEN. That keeps the server stateless: several people (or several
// tabs) can play at once, reloading the page cannot desynchronise a game
// from the server's idea of it, and a restart mid-game costs nothing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/moves"
)

// champions hands out the strongest configuration measured so far,
// reloading it when a campaign adopts something new. The UI must play the
// current best without a restart, which is why this is a watcher rather
// than a value read once at startup.
var champions *engine.ChampionWatcher

func best() engine.Player {
	_, p := champions.Current()
	return p
}

// status describes how a game ended, or that it has not.
func status(g *game.Game) string {
	switch {
	case g.IsCheckmate(g.Turn):
		if g.Turn == board.White {
			return "checkmate: Black wins"
		}
		return "checkmate: White wins"
	case g.IsStalemate(g.Turn):
		return "draw: stalemate"
	case g.IsFiftyMoveDraw():
		return "draw: fifty-move rule"
	case g.IsThreefoldRepetition():
		return "draw: threefold repetition"
	case moves.IsInCheck(&g.Board, g.Turn):
		return "check"
	}
	return ""
}

func over(s string) bool {
	return len(s) >= 9 && (s[:9] == "checkmate" || s[:4] == "draw")
}

type moveRequest struct {
	FEN  string `json:"fen"`
	From string `json:"from"`
	To   string `json:"to"`
}

type moveResponse struct {
	OK         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
	FEN        string   `json:"fen"`
	EngineMove string   `json:"engine_move,omitempty"`
	Status     string   `json:"status"`
	Legal      []string `json:"legal"`
	ThinkMS    int64    `json:"think_ms,omitempty"`
	Score      float64  `json:"score"`
	Depth      int      `json:"depth"`
	Nodes      int      `json:"nodes"`
	KNPS       float64  `json:"knps"`
}

// legalUCI lists every legal move for the side to move, so the browser
// can highlight destinations and reject illegal drags without needing a
// second copy of the rules in JavaScript.
func legalUCI(g *game.Game) []string {
	out := []string{}
	for _, m := range g.AllLegalMoves(g.Turn) {
		out = append(out, m.UCI())
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func handleNew(w http.ResponseWriter, r *http.Request) {
	g := game.New()
	score := engine.PlayerStaticEval(best(), &g.Board)
	writeJSON(w, moveResponse{
		OK: true, FEN: g.FEN(), Legal: legalUCI(g),
		Score: score,
	})
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, moveResponse{Error: "bad request"})
		return
	}
	g, err := game.ParseFEN(req.FEN)
	if err != nil {
		writeJSON(w, moveResponse{Error: "bad position: " + err.Error()})
		return
	}
	g.EnableRepetitionTracking()

	from, ok1 := game.MoveFromUCI(req.From + req.To)
	if !ok1 {
		writeJSON(w, moveResponse{Error: "bad move"})
		return
	}
	// Trust nothing from the browser: the move has to be in this
	// position's legal list, not merely well-formed.
	legal := false
	for _, m := range g.AllLegalMoves(g.Turn) {
		if m.From == from.From && m.To == from.To {
			legal = true
			break
		}
	}
	if !legal {
		writeJSON(w, moveResponse{Error: "illegal move", FEN: g.FEN(), Legal: legalUCI(g)})
		return
	}
	g.ApplyMove(from.From, from.To)

	if s := status(g); over(s) {
		writeJSON(w, moveResponse{OK: true, FEN: g.FEN(), Status: s, Legal: legalUCI(g)})
		return
	}

	engine.ResetNodes()
	t0 := time.Now()
	reply, score, ok := engine.PlayerPickScored(best(), g)
	elapsed := time.Since(t0)
	if !ok {
		writeJSON(w, moveResponse{OK: true, FEN: g.FEN(), Status: status(g), Legal: legalUCI(g)})
		return
	}
	g.ApplyMove(reply.From, reply.To)

	nodes := engine.TotalNodes()
	depth := engine.LastSearchDepth()
	var knps float64
	if elapsed.Seconds() > 0 {
		knps = float64(nodes) / elapsed.Seconds() / 1000
	}

	writeJSON(w, moveResponse{
		OK: true, FEN: g.FEN(), EngineMove: reply.UCI(),
		Status: status(g), Legal: legalUCI(g),
		ThinkMS: elapsed.Milliseconds(),
		Score:   score,
		Depth:   depth,
		Nodes:   nodes,
		KNPS:    knps,
	})
}

// handleHint answers with the move the engine would play, so a human can
// ask what it would do without handing over the game.
func handleHint(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, moveResponse{Error: "bad request"})
		return
	}
	g, err := game.ParseFEN(req.FEN)
	if err != nil {
		writeJSON(w, moveResponse{Error: "bad position"})
		return
	}
	engine.ResetNodes()
	t0 := time.Now()
	m, score, ok := engine.PlayerPickScored(best(), g)
	elapsed := time.Since(t0)
	if !ok {
		writeJSON(w, moveResponse{Error: "no move"})
		return
	}
	nodes := engine.TotalNodes()
	depth := engine.LastSearchDepth()
	var knps float64
	if elapsed.Seconds() > 0 {
		knps = float64(nodes) / elapsed.Seconds() / 1000
	}
	writeJSON(w, moveResponse{
		OK: true, FEN: req.FEN, EngineMove: m.UCI(), Legal: legalUCI(g),
		ThinkMS: elapsed.Milliseconds(),
		Score:   score,
		Depth:   depth,
		Nodes:   nodes,
		KNPS:    knps,
	})
}

func handleEval(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, moveResponse{Error: "bad request"})
		return
	}
	g, err := game.ParseFEN(req.FEN)
	if err != nil {
		writeJSON(w, moveResponse{Error: "bad position"})
		return
	}
	engine.ResetNodes()
	t0 := time.Now()
	p := best()
	p.Depth = 5
	p.TimeBudget = 100 * time.Millisecond
	_, score, ok := engine.PlayerPickScored(p, g)
	elapsed := time.Since(t0)
	if !ok {
		score = engine.PlayerStaticEval(best(), &g.Board)
	}
	nodes := engine.TotalNodes()
	depth := engine.LastSearchDepth()
	var knps float64
	if elapsed.Seconds() > 0 {
		knps = float64(nodes) / elapsed.Seconds() / 1000
	}
	writeJSON(w, moveResponse{
		OK: true, FEN: req.FEN, Legal: legalUCI(g),
		ThinkMS: elapsed.Milliseconds(),
		Score:   score,
		Depth:   depth,
		Nodes:   nodes,
		KNPS:    knps,
	})
}

func main() {
	port := flag.Int("port", 8765, "port to serve on")
	champFile := flag.String("champion", "champion.json", "descriptor of the strongest configuration so far")
	dir := flag.String("dir", ".", "directory to serve files from")
	flag.Parse()

	champions = engine.NewChampionWatcher(*champFile)

	http.HandleFunc("/api/new", handleNew)
	http.HandleFunc("/api/move", handleMove)
	http.HandleFunc("/api/hint", handleHint)
	http.HandleFunc("/api/eval", handleEval)
	http.HandleFunc("/api/engine", func(w http.ResponseWriter, r *http.Request) {
		c, p := champions.Current()
		eval := "hand-written evaluation"
		if p.HalfKP != nil {
			eval = fmt.Sprintf("HalfKP network (%d hidden), %.0f%% hand evaluation",
				p.HalfKP.H, 100*c.HandBlend)
		}
		if n := p.Book.Len(); n > 0 {
			eval += fmt.Sprintf(", opening book of %d positions", n)
		}
		writeJSON(w, map[string]any{
			"depth": p.Depth, "cores": runtime.NumCPU(),
			"champion": c.Label, "elo": c.Elo, "margin": c.Margin,
			"adopted": c.Adopted, "eval": eval,
			"description": fmt.Sprintf("depth %d, iterative deepening + PVS + killers + history + LMR, null-move, futility, quiescence with SEE, tapered PST, pawn structure; %s", p.Depth, eval),
		})
	})
	http.Handle("/", http.FileServer(http.Dir(*dir)))

	c, _ := champions.Current()
	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("serving %s on http://localhost%s/gui.html\nchampion: %s (depth %d, ~%.0f Elo)\n",
		*dir, addr, c.Label, c.Depth, c.Elo)
	log.Fatal(http.ListenAndServe(addr, nil))
}

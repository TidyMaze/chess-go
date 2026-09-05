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

var best engine.Player

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
	writeJSON(w, moveResponse{OK: true, FEN: g.FEN(), Legal: legalUCI(g)})
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

	t0 := time.Now()
	reply, ok := engine.PlayerPick(best, g)
	if !ok {
		writeJSON(w, moveResponse{OK: true, FEN: g.FEN(), Status: status(g), Legal: legalUCI(g)})
		return
	}
	g.ApplyMove(reply.From, reply.To)

	writeJSON(w, moveResponse{
		OK: true, FEN: g.FEN(), EngineMove: reply.UCI(),
		Status: status(g), Legal: legalUCI(g),
		ThinkMS: time.Since(t0).Milliseconds(),
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
	m, ok := engine.PlayerPick(best, g)
	if !ok {
		writeJSON(w, moveResponse{Error: "no move"})
		return
	}
	writeJSON(w, moveResponse{OK: true, FEN: req.FEN, EngineMove: m.UCI(), Legal: legalUCI(g)})
}

func main() {
	port := flag.Int("port", 8765, "port to serve on")
	depth := flag.Int("depth", 5, "engine search depth")
	tuned := flag.Bool("tuned", false, "use the Texel-tuned evaluation")
	dir := flag.String("dir", ".", "directory to serve files from")
	flag.Parse()

	best = engine.Player{
		Name: "best", Depth: *depth, UsePST: true, Quiescence: true,
		TTBits: 20, NullMove: true, Tapered: true, Iterative: true,
		Extensions: true, Aspiration: true, SEEPruning: true,
		Structure: true, Futility: true, Tuned: *tuned,
	}

	http.HandleFunc("/api/new", handleNew)
	http.HandleFunc("/api/move", handleMove)
	http.HandleFunc("/api/hint", handleHint)
	http.HandleFunc("/api/engine", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"depth": *depth, "tuned": *tuned, "cores": runtime.NumCPU(),
			"description": fmt.Sprintf("depth %d, iterative deepening + PVS + killers + history + LMR, null-move, futility, quiescence with SEE, tapered PST, pawn structure", *depth),
		})
	})
	http.Handle("/", http.FileServer(http.Dir(*dir)))

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("serving %s on http://localhost%s/gui.html (engine: depth %d, tuned=%v)\n", *dir, addr, *depth, *tuned)
	log.Fatal(http.ListenAndServe(addr, nil))
}

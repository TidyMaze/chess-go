// Command lossaudit reads the games a gauntlet recorded with -games-out
// and says how the challenger lost or drew them: where in the game the
// evaluation collapsed, on what kind of move, and whether the challenger
// saw it coming. The judge is an external engine at fixed depth, which is
// a ruler here, never a teacher.
package main

import (
	"math"
	"sort"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// plyEval is the judge's opinion after one ply, from the challenger's
// point of view, in pawns.
type plyEval struct {
	ply      int
	ours     bool // the challenger made this move
	eval     float64
	mate     bool
	move     string
	capture  bool
	promo    bool
	material int // non-pawn men on the board after the move, both sides
	ownScore float64
	fen      string
	before   string // FEN before the move, for probing what should have been played
}

// worstDrop is the challenger move after which the judge's evaluation fell
// most, compared to the position before it. A drop below threshold means
// no single move lost the game: it bled away.
type worstDrop struct {
	found bool
	ply   int
	drop  float64
	at    plyEval
	fromE float64
}

func findWorstDrop(evals []plyEval) worstDrop {
	var w worstDrop
	prev := math.NaN()
	for _, e := range evals {
		if e.ours && !math.IsNaN(prev) && !e.mate {
			if d := prev - e.eval; d > w.drop {
				w = worstDrop{found: true, ply: e.ply, drop: d, at: e, fromE: prev}
			}
		}
		if e.mate {
			// A mate score has no pawn value; keep the last real one.
			continue
		}
		prev = e.eval
	}
	return w
}

// phase names the position by non-pawn men on both sides.
func phase(material int) string {
	switch {
	case material >= 12:
		return "opening"
	case material >= 6:
		return "middlegame"
	default:
		return "endgame"
	}
}

func nonPawnMen(b *board.Board) int {
	var buf [32]board.ColoredPiece
	n := 0
	for _, p := range b.AppendAllPieces(buf[:0]) {
		if p.Type != board.Pawn && p.Type != board.King {
			n++
		}
	}
	return n
}

// challengerIsWhite reads the colour from the names the gauntlet wrote.
func challengerIsWhite(r engine.GameRecord) bool {
	return len(r.White) >= 10 && r.White[:10] == "challenger"
}

// outcome is the challenger's result: 1 win, 0.5 draw, 0 loss.
func outcome(r engine.GameRecord) float64 {
	if !r.Decisive {
		return 0.5
	}
	if (r.Winner == board.White) == challengerIsWhite(r) {
		return 1
	}
	return 0
}

// replay walks the game and asks the judge about every position.
func replay(r engine.GameRecord, judge func(*game.Game) (float64, bool, bool), depth int) ([]plyEval, error) {
	g, err := game.ParseFEN(r.StartFEN)
	if err != nil {
		return nil, err
	}
	white := challengerIsWhite(r)
	var out []plyEval
	for i, uci := range r.Moves {
		m, ok := game.MoveFromUCI(uci)
		if !ok {
			break
		}
		_, capture := g.Board.PieceAt(m.To)
		mover := g.Turn
		before := g.FEN()
		g.Apply(m)
		e, mate, ok := judge(g)
		if !ok {
			continue
		}
		// The judge speaks for the side to move, which is now the opponent
		// of the mover; flip to the challenger's view.
		toMove := g.Turn
		if (toMove == board.White) != white {
			e = -e
		}
		p, _ := g.Board.PieceAt(m.To)
		out = append(out, plyEval{
			ply: i + 1, ours: (mover == board.White) == white, eval: e, mate: mate, move: uci,
			capture: capture, promo: p.Type == board.Queen && len(uci) == 5,
			material: nonPawnMen(&g.Board), ownScore: r.Scores[i], fen: g.FEN(), before: before,
		})
	}
	return out, nil
}

type bucketCount map[string]int

func (b bucketCount) sortedKeys() []string {
	ks := make([]string, 0, len(b))
	for k := range b {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

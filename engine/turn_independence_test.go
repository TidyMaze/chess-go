package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// The search makes its moves on the board and never updates g.Turn, which
// stays the root's side for the whole tree. Every node has to take the side
// to move from its colour argument, so the same search must give the same
// score over the same tree whatever g.Turn says.
//
// It did not: the table move was screened with g.IsLegalMove, which answers
// for g.Turn, so at every node of the other side the shortcut turned the
// move down and the node generated its whole list first. And the singular
// extension, which only ran behind that shortcut, searched its alternatives
// for g.Turn. The champion leaves singular off, so it is run both ways.
func TestSearchIgnoresGameTurn(t *testing.T) {
	c := ReadChampion("../champion.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		t.Fatalf("champion: %v", err)
	}
	type result struct {
		score float64
		nodes int
	}
	run := func(fen string, turn board.Color, singular bool) result {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		color := g.Turn
		g.Turn = turn
		ev := evalForPlayer(p)
		ev.Table = NewTranspositionTable(18)
		ev.Singular = singular
		ctx := &searchCtx{}
		ctx.reset()
		ctx.ev, ctx.quiescence, ctx.extensions = ev, p.Quiescence, ev.Extensions
		ctx.ev.acc = &ctx.acc
		ctx.acc[0].valid = false
		SeedRandom(1)
		var score float64
		for depth := 1; depth <= 6; depth++ {
			score = ctx.search(g, color, color, depth, 0, negInf, posInf)
		}
		return result{score, ctx.nodes}
	}
	for _, singular := range []bool{false, true} {
		for _, fen := range championBenchFENs {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			asRoot, flipped := run(fen, g.Turn, singular), run(fen, g.Turn.Other(), singular)
			if asRoot.nodes == 0 {
				t.Fatalf("%s: searched no nodes", fen)
			}
			if asRoot != flipped {
				t.Errorf("singular %v, %s: g.Turn = side to move gives %.4f in %d nodes, the other side %.4f in %d",
					singular, fen, asRoot.score, asRoot.nodes, flipped.score, flipped.nodes)
			}
		}
	}
}

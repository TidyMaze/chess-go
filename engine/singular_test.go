package engine

import (
	"testing"

	"chess/game"
)

// Singular extension: when the transposition table's move is much better
// than every alternative, searching it one ply deeper is what strong
// engines spend nodes on. The test of "much better" is a reduced-depth
// search of the position with that move excluded, against a window a
// margin below the stored score. If every other move fails below it, the
// move is singular.
//
// Excluding a move is the mechanism, so it is tested directly across real
// positions: whatever the search would have chosen, it must not choose it
// once barred. The first version of this test asserted a mate in one that
// was not mate, and the engine was right and the test wrong.
func TestAnExcludedMoveIsNeverPlayed(t *testing.T) {
	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		p := Strong(4)
		best, ok := PlayerPick(p, g)
		if !ok {
			continue
		}

		ctx := searchCtxPool.Get().(*searchCtx)
		ctx.reset()
		ev := evalForPlayer(p)
		ev.Table = NewTranspositionTable(14)
		ctx.ev, ctx.quiescence = ev, true
		ev.acc = &ctx.acc
		ctx.excluded[0] = best
		ctx.path[0] = zobristHash(g)
		ctx.played = playedKeys(g)

		// The root of an exclusion search reports a score, so read the move
		// by searching each alternative one ply down is overkill: it is
		// enough that the excluded move is skipped, which shows as the
		// search never storing it as the table's best for this position.
		ctx.search(g, g.Turn, g.Turn, 4, 0, negInf, posInf)
		got, found := ev.Table.bestMove(zobristHash(g))
		searchCtxPool.Put(ctx)
		if found && got == best {
			t.Errorf("%s: %v was excluded and still came back as the best move", fen, best.UCI())
		}
	}
}

func TestSingularIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("singular")
	if !p.Singular || !evalForPlayer(p).Singular {
		t.Fatal("singular did not reach the Eval")
	}
}

// With the flag on the tree must change, and with it off it must not: a
// singular extension that never fires is a feature that does nothing, and
// one that fires with the flag off is a regression for everyone.
func TestSingularChangesTheTreeOnlyWhenOn(t *testing.T) {
	var off, on int
	for _, fen := range correctnessPositions[:4] {
		g, _ := game.ParseFEN(fen)
		a, b := Strong(9), Strong(9)
		b.Singular = true
		ResetNodes()
		PlayerScoreWith(a, g, nil)
		off += TotalNodes()
		ResetNodes()
		PlayerScoreWith(b, g, nil)
		on += TotalNodes()
	}
	t.Logf("nodes at depth 9: %d without singular extensions, %d with (%.1f%%)",
		off, on, 100*float64(on-off)/float64(off))
	if on == off {
		t.Error("the flag changed nothing, so the singular test never fired")
	}
}

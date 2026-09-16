package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Quiescence stores at depth 0 and is roughly half the nodes searched, so
// with always-replace every deep entry is one collision away from being
// overwritten by a leaf. Depth-preferred keeps the expensive entry; aging
// is what stops the table silting up, since without it a full table would
// never accept anything new again.
func TestDeepEntriesSurviveShallowCollisions(t *testing.T) {
	tt := NewTranspositionTable(12)
	const deepKey, shallowKey = uint64(0x1234567800000001), uint64(0x9876543200000001)
	if deepKey&tt.mask != shallowKey&tt.mask {
		t.Fatal("the two keys must collide for this test to mean anything")
	}

	tt.store(deepKey, 1.5, 10, ttExact, board.White)
	tt.store(shallowKey, -0.5, 0, ttExact, board.White)
	if _, ok := tt.probe(deepKey, 10, board.White, -10, 10); !ok {
		t.Error("a depth-10 entry was evicted by a depth-0 quiescence store")
	}

	// Same depth still replaces: the newer entry is the more relevant one.
	tt.store(shallowKey, -0.5, 10, ttExact, board.White)
	if _, ok := tt.probe(deepKey, 10, board.White, -10, 10); ok {
		t.Error("an equally deep entry should have taken the slot")
	}

	// A new search ages the table, and then depth no longer protects.
	tt.store(deepKey, 1.5, 10, ttExact, board.White)
	tt.NewSearch()
	tt.store(shallowKey, -0.5, 0, ttExact, board.White)
	if _, ok := tt.probe(deepKey, 10, board.White, -10, 10); ok {
		t.Error("an entry from the previous search must be replaceable")
	}
}

// The continuation table is 196 KB and was cleared on every search even
// with the feature off, which showed up as memclr in the profile.
func TestContinuationTableIsOnlyClearedWhenUsed(t *testing.T) {
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	// Writing through contSlot is what marks the table dirty, which is how
	// the search reaches it.
	g := game.New()
	ctx.ev = &Eval{ContHist: true}
	ctx.prevMove = game.Move{From: board.Sq{File: 4, Rank: 6}, To: board.Sq{File: 4, Rank: 4}}
	slot := ctx.contSlot(board.White, g, game.Move{From: board.Sq{File: 6, Rank: 0}, To: board.Sq{File: 5, Rank: 2}})
	if slot == nil {
		t.Fatal("no continuation slot with the feature on")
	}
	*slot = 42
	ctx.reset()
	if *slot != 0 {
		t.Error("a used continuation table must be cleared between searches")
	}

	// Never written, so never cleared: 196 KB of memclr per search.
	ctx.ev = &Eval{}
	ctx.cont[0][1][2][3] = 42
	ctx.reset()
	if ctx.cont[0][1][2][3] != 42 {
		t.Error("an unused continuation table should not be cleared, it is 196 KB per search")
	}
}

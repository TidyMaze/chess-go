package engine

import "testing"

// The quiescence cap has to live in the champion file for a champion A/B to
// vary it: -qply only configures the plain challenger, which -champion replaces.
func TestTheChampionFileSetsTheQuiescenceCap(t *testing.T) {
	p, err := Champion{Depth: 4, QuiescePly: 8}.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	if p.QuiescePly != 8 {
		t.Errorf("qply 8 in the champion file gave QuiescePly %d", p.QuiescePly)
	}
	if p, _ := (Champion{Depth: 4}).PlayerOrError(); p.QuiescePly != Strong(4).QuiescePly {
		t.Errorf("no qply must keep the engine default %d, got %d", Strong(4).QuiescePly, p.QuiescePly)
	}
}

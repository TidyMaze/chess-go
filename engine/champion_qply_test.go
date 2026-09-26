package engine

import "testing"

// The quiescence cap has to live in the champion file for a champion A/B to
// vary it: -qply only configures the plain challenger, which -champion replaces.
// Tuned search margins have to travel with the champion, like qply, or every
// harness would need a -tune flag to play the adopted engine.
func TestTheChampionFileCarriesItsSearchTune(t *testing.T) {
	p, err := Champion{Depth: 4, Tune: "LMRDiv=2.3,NullBase=3.4"}.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	if p.Tune == nil || p.Tune.LMRDiv != 2.3 || p.Tune.NullBase != 3.4 {
		t.Errorf("tune in the champion file gave %+v", p.Tune)
	}
	if p, _ := (Champion{Depth: 4}).PlayerOrError(); p.Tune != nil {
		t.Errorf("no tune must keep the defaults, got %+v", p.Tune)
	}
	if _, err := (Champion{Depth: 4, Tune: "Nonsense=1"}).PlayerOrError(); err == nil {
		t.Error("an unknown tune name in the champion file was accepted")
	}
}

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

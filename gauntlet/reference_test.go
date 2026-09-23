package main

import (
	"reflect"
	"testing"
	"time"

	"chess/engine"
)

// Applying the reference switches with the values the flags default to must
// leave the engine exactly as it is.
//
// That is the invariant the whole harness rests on: when nobody configures
// anything, the two sides of a match are the same player and an equal pair
// must score 0.500. It was broken once on the challenger side, where
// -mobility defaulted to false while engine.Strong sets it true, and the
// same network on both sides then read -18 +/- 22 over 1000 games. Seven
// ladder rungs were measured through that.
func TestDefaultReferenceSwitchesLeaveTheEngineAlone(t *testing.T) {
	base := engine.Strong(4)
	got, err := referenceSwitches{
		futility:       strongDefaults.Futility,
		noCastle:       false,
		noRepetition:   false,
		keepNullMoveEP: false,
		features:       "",
	}.applyTo(base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Errorf("default switches changed the reference:\n before %+v\n after  %+v", base, got)
	}
}

// The switches have to be applied after -ref-champion, which replaces the
// reference wholesale. Assigned before it, they were silently discarded,
// so -ref-no-castle, -ref-no-repetition and -ref-nullmove-ep-bug did
// nothing whenever the reference was a champion, which is the only mode the
// ladder ever runs in.
func TestReferenceSwitchesSurviveAChampionReplacement(t *testing.T) {
	// What PlayerOrError hands back for a champion: a fresh Strong, so
	// anything set on the player it replaced is gone.
	champion := engine.Strong(4)
	champion.Name = "reference: some champion"

	got, err := referenceSwitches{
		futility:       false,
		noCastle:       true,
		noRepetition:   true,
		keepNullMoveEP: true,
		features:       "lmp,see, iir ",
	}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		got  bool
	}{
		{"-futility=false", !got.Futility},
		{"-ref-no-castle", got.NoCastle},
		{"-ref-no-repetition", got.NoRepetition},
		{"-ref-nullmove-ep-bug", got.KeepNullMoveEP},
		{"-ref-features lmp", got.LMP},
		{"-ref-features see", got.MainSEE},
		{"-ref-features iir", got.IIR},
	} {
		if !c.got {
			t.Errorf("%s did not reach the reference", c.name)
		}
	}
	// Untouched features stay off, so a feature list cannot switch on more
	// than it names.
	if got.DeepRFP || got.NullGate || got.Countermoves || got.ScaledLMR || got.LMRTwoStep || got.HistoryAging {
		t.Error("a feature nobody asked for was switched on")
	}
}

// SPSA races two tunings of the same champion, so each side's search margins
// must come from its own flag and survive the champion-file replacement.
func TestEachSideTakesItsOwnSearchTune(t *testing.T) {
	p, err := (referenceSwitches{tune: "LMRDiv=2.3"}).applyTo(engine.Strong(4))
	if err != nil {
		t.Fatal(err)
	}
	if p.Tune == nil || p.Tune.LMRDiv != 2.3 {
		t.Errorf("tune LMRDiv=2.3 gave %+v", p.Tune)
	}
	if p, _ := (referenceSwitches{}).applyTo(engine.Strong(4)); p.Tune != nil {
		t.Errorf("no tune flag must leave the defaults, got %+v", p.Tune)
	}
	if _, err := (referenceSwitches{tune: "Nonsense=1"}).applyTo(engine.Strong(4)); err == nil {
		t.Error("an unknown tune name was accepted")
	}
}

func TestUnknownReferenceFeatureIsRejected(t *testing.T) {
	if _, err := (referenceSwitches{features: "lmp,nonsense"}).applyTo(engine.Strong(4)); err == nil {
		t.Error("an unknown feature was accepted, so a typo would silently race the wrong reference")
	}
}

// The harness owns the time control on both sides.
//
// champion.json carries time_ms now that the champion is deployed on a
// clock, and PlayerOrError honours it, so a reference built from that file
// arrives with a one second budget. Left alone, it plays a second a move
// against a challenger on a fixed depth: the same network on both sides
// read -552 +/- 149 over 200 games, which is the clock's worth, not the
// network's.
func TestTheReferenceTakesItsClockFromTheHarnessNotTheChampionFile(t *testing.T) {
	champion := engine.Strong(4)
	champion.TimeBudget = 1234 * time.Millisecond // as champion.json would give it

	fixedDepth, err := referenceSwitches{timeMS: 0, futility: strongDefaults.Futility}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	if fixedDepth.TimeBudget != 0 {
		t.Errorf("a fixed-depth match left the reference on a %v clock", fixedDepth.TimeBudget)
	}

	clocked, err := referenceSwitches{timeMS: 1000, futility: strongDefaults.Futility}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	if clocked.TimeBudget != time.Second {
		t.Errorf("-time-ms 1000 gave the reference %v", clocked.TimeBudget)
	}
}

// Threads come from the harness as well. A champion file saying four would
// otherwise give the reference four threads in each of ten workers, forty
// threads on ten cores against a single-threaded challenger.
func TestTheReferenceTakesItsThreadsFromTheHarnessNotTheChampionFile(t *testing.T) {
	champion := engine.Strong(4)
	champion.Threads = 4 // as a champion.json with "threads": 4 would give it

	single, err := referenceSwitches{threads: 0, futility: strongDefaults.Futility}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	if single.Threads != 0 {
		t.Errorf("a match nobody gave -threads left the reference on %d threads", single.Threads)
	}
	two, err := referenceSwitches{threads: 2, futility: strongDefaults.Futility}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	if two.Threads != 2 {
		t.Errorf("-threads 2 gave the reference %d", two.Threads)
	}
}

// The challenger can now be a champion file too, which is the only way to
// put the identical player on both sides and let a single -features flag be
// the whole difference. Without it the challenger is engine.Strong plus
// flags, which is not the champion: a null control of -halfkp against
// -ref-champion read -10 +/- 48 on one run and -40 +/- 34 on another, a
// baseline that moves by more than most effects being looked for. With both
// sides on champion.json it reads -9 +/- 34 over 400 games.
//
// The switches have to survive the replacement, exactly as they do on the
// reference side, because a replacement discards everything set before it.
func TestChallengerSwitchesSurviveAChampionReplacement(t *testing.T) {
	champion := engine.Strong(4)
	champion.Name = "challenger: some champion"
	champion.TimeBudget = 1000 * 1000 * 1000 // a champion file's own clock
	champion.Threads = 8

	got, err := referenceSwitches{
		timeMS:   200,
		threads:  1,
		futility: true,
		features: "historyaging",
	}.applyTo(champion)
	if err != nil {
		t.Fatal(err)
	}
	// The harness owns the clock and the threads on this side too, or a
	// champion file saying 8 threads and one second would race a
	// single-threaded challenger at 200 ms and win on nothing but that.
	if got.TimeBudget != 200*1000*1000 {
		t.Errorf("time budget %v, want the harness's 200ms and not the champion file's", got.TimeBudget)
	}
	if got.Threads != 1 {
		t.Errorf("threads %d, want the harness's 1", got.Threads)
	}
	if !got.HistoryAging {
		t.Error("-features did not reach the challenger after the replacement")
	}
	if !got.Futility {
		t.Error("-futility did not reach the challenger after the replacement")
	}
	// And the feature under test must be the only thing switched on.
	if got.NullGate || got.IIR || got.DeepRFP {
		t.Error("a feature nobody asked for was switched on")
	}
}

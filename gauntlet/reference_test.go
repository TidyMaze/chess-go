package main

import (
	"reflect"
	"testing"

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
	if got.DeepRFP || got.NullGate || got.Countermoves || got.ScaledLMR {
		t.Error("a feature nobody asked for was switched on")
	}
}

func TestUnknownReferenceFeatureIsRejected(t *testing.T) {
	if _, err := (referenceSwitches{features: "lmp,nonsense"}).applyTo(engine.Strong(4)); err == nil {
		t.Error("an unknown feature was accepted, so a typo would silently race the wrong reference")
	}
}

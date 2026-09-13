package lichessbot

import (
	"chess/game"
	"testing"
	"time"
)

// The accept policy must never say yes to a variant: this engine has
// never played one and a variant board (atomic captures, crazyhouse
// drops) would silently corrupt the champion's own board state, which
// only knows standard rules.
func TestShouldAcceptChallengeRejectsVariants(t *testing.T) {
	for _, variant := range []string{"chess960", "atomic", "crazyhouse", "kingOfTheHill"} {
		c := Challenge{Variant: variant}
		if shouldAcceptChallenge(c) {
			t.Errorf("variant %q was accepted; the engine only plays standard chess", variant)
		}
	}
}

func TestShouldAcceptChallengeAcceptsStandardAtAnySpeedAboveUltraBullet(t *testing.T) {
	for _, speed := range []string{"bullet", "blitz", "rapid", "classical"} {
		c := Challenge{Variant: "standard", SpeedTC: speed, Rated: true}
		if !shouldAcceptChallenge(c) {
			t.Errorf("standard %s was rejected", speed)
		}
	}
}

func TestShouldAcceptChallengeRejectsUltraBullet(t *testing.T) {
	c := Challenge{Variant: "standard", SpeedTC: "ultraBullet"}
	if shouldAcceptChallenge(c) {
		t.Error("ultraBullet was accepted; a round trip to lichess cannot answer inside that clock")
	}
}

// An empty variant field means standard on lichess's own wire format, so
// it must not be rejected as if it were a named variant.
func TestShouldAcceptChallengeTreatsEmptyVariantAsStandard(t *testing.T) {
	if !shouldAcceptChallenge(Challenge{Variant: "", Rated: true}) {
		t.Error("an empty variant field was rejected; lichess omits it for standard chess")
	}
}

// Replaying the move list is the one piece of this package that can
// silently diverge from the real position: get it wrong and the bot
// plays a move that was legal three plies ago.
func TestApplyMovesStringReplaysFromTheStartPosition(t *testing.T) {
	g, err := applyMovesString("startpos", "e2e4 e7e5 g1f3")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(g.AllLegalMoves(g.Turn)); got == 0 {
		t.Fatal("no legal moves after three plies from the start position")
	}
	// Black to move after White's third-move knight development.
	if !isOurTurn(g, "black") {
		t.Error("after e4 e5 Nf3 it is black to move")
	}
}

func TestApplyMovesStringHandlesAnEmptyMoveList(t *testing.T) {
	g, err := applyMovesString("startpos", "")
	if err != nil {
		t.Fatal(err)
	}
	if !isOurTurn(g, "white") {
		t.Error("the start position has white to move")
	}
}

func TestApplyMovesStringStartsFromAGivenFEN(t *testing.T) {
	// One move from the black side of a well known middlegame FEN.
	fen := "r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R b KQkq - 2 2"
	g, err := applyMovesString(fen, "f8c5")
	if err != nil {
		t.Fatal(err)
	}
	if !isOurTurn(g, "white") {
		t.Error("after black's bishop move it should be white to move")
	}
}

func TestApplyMovesStringRejectsABadFEN(t *testing.T) {
	if _, err := applyMovesString("not a fen", ""); err == nil {
		t.Error("a malformed FEN was accepted silently")
	}
}

// A move the parser cannot read (garbage from a stream glitch) must stop
// the replay rather than desync the board by skipping it and continuing.
func TestApplyMovesStringStopsAtAnUnparseableMove(t *testing.T) {
	g, err := applyMovesString("startpos", "e2e4 not-a-move g1f3")
	if err != nil {
		t.Fatal(err)
	}
	// Only e2e4 applied: it is black to move, not white.
	if isOurTurn(g, "white") {
		t.Error("the replay continued past an unparseable move instead of stopping")
	}
}

func TestIsOurTurn(t *testing.T) {
	g, err := applyMovesString("startpos", "")
	if err != nil {
		t.Fatal(err)
	}
	if !isOurTurn(g, "white") {
		t.Error("start position: white to move")
	}
	if isOurTurn(g, "black") {
		t.Error("start position: it is not black to move")
	}
}

// A pawn reaching the last rank must send an explicit promotion letter:
// this engine always queens internally, but lichess reads a bare
// four-character move as an illegal pawn advance, not a queening.
func TestMoveUCIForLichessAddsQOnAPromotingPawn(t *testing.T) {
	g, err := applyMovesString("8/P7/8/8/8/8/8/k1K5 w - - 0 1", "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := game.MoveFromUCI("a7a8")
	if !ok {
		t.Fatal("could not parse a7a8")
	}
	if got := moveUCIForLichess(g, m); got != "a7a8q" {
		t.Errorf("got %q, want a7a8q", got)
	}
}

func TestMoveUCIForLichessLeavesAnOrdinaryPawnPushAlone(t *testing.T) {
	g, err := applyMovesString("startpos", "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := game.MoveFromUCI("e2e4")
	if !ok {
		t.Fatal("could not parse e2e4")
	}
	if got := moveUCIForLichess(g, m); got != "e2e4" {
		t.Errorf("got %q, want e2e4 with no promotion suffix", got)
	}
}

func TestMoveUCIForLichessLeavesANonPawnMoveAlone(t *testing.T) {
	g, err := applyMovesString("startpos", "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := game.MoveFromUCI("g1f3")
	if !ok {
		t.Fatal("could not parse g1f3")
	}
	if got := moveUCIForLichess(g, m); got != "g1f3" {
		t.Errorf("got %q, want g1f3", got)
	}
}

// A move budget must scale with the clock: more time left means more
// thinking time, so the same engine plays deeper in a slow game and
// shallower in a fast one, instead of the fixed budget champion.json
// carries for every measurement race.
func TestMoveTimeBudgetScalesWithRemainingTime(t *testing.T) {
	rapid := moveTimeBudget("white", gameState{WhiteTimeMS: 600000, WhiteIncMS: 5000})
	bullet := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 1000})
	if rapid <= bullet {
		t.Errorf("a 10 minute clock (%v) did not think longer than a 1 minute clock (%v)", rapid, bullet)
	}
}

// The increment must count too: a move played purely on increment (no
// time pressure at all) should still get some budget from it, not zero.
func TestMoveTimeBudgetCountsTheIncrement(t *testing.T) {
	noInc := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 0})
	withInc := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 10000})
	if withInc <= noInc {
		t.Errorf("a 10s increment (%v) did not add thinking time over no increment (%v)", withInc, noInc)
	}
}

// The budget must read the color it is asked for, not always white's
// clock: playing black in a game where white has plenty of time and
// black is nearly flagging must use black's own numbers.
func TestMoveTimeBudgetReadsOurOwnColor(t *testing.T) {
	st := gameState{WhiteTimeMS: 600000, WhiteIncMS: 5000, BlackTimeMS: 3000, BlackIncMS: 0}
	white := moveTimeBudget("white", st)
	black := moveTimeBudget("black", st)
	if white <= black {
		t.Errorf("white with plenty of time (%v) was not given more than black near flagging (%v)", white, black)
	}
}

// A budget must never exceed what is actually left on the clock: a move
// that takes longer than the remaining time loses the game on time,
// which is a worse outcome than any depth the extra thinking could buy.
func TestMoveTimeBudgetNeverExceedsWhatIsLeft(t *testing.T) {
	// A large increment pushes the raw formula (remaining/30 + 80% of
	// increment) well past the 1 second actually left, so this only
	// stays safe if the ceiling clamp is doing its job.
	st := gameState{WhiteTimeMS: 1000, WhiteIncMS: 20000}
	got := moveTimeBudget("white", st)
	if got >= time.Duration(st.WhiteTimeMS)*time.Millisecond {
		t.Errorf("budget %v does not leave any safety margin on a %dms clock", got, st.WhiteTimeMS)
	}
	if got <= 0 {
		t.Error("a nearly flagging clock still needs a move to play, budget must stay positive")
	}
}

// A very slow time control must not make the engine think forever for a
// gain nothing here has ever measured: mean depth only grows from 10.6 to
// 11.8 plies going from one to four threads at 1s, so there is no reason
// to trust minutes of thinking on one move.
func TestMoveTimeBudgetIsCappedOnASlowClock(t *testing.T) {
	got := moveTimeBudget("white", gameState{WhiteTimeMS: 3600000, WhiteIncMS: 60000})
	if got > 15*time.Second {
		t.Errorf("budget %v was not capped on a one hour clock", got)
	}
}

// No clock at all (correspondence games send no wtime/btime) must signal
// the caller to fall back to a fixed budget, not silently think for 0ms
// and play whatever move happens to be ready first.
func TestMoveTimeBudgetIsZeroWithNoClock(t *testing.T) {
	if got := moveTimeBudget("white", gameState{}); got != 0 {
		t.Errorf("got %v with no clock data at all, want 0 so the caller falls back", got)
	}
}

// With almost no time left the ceiling itself would go negative; the
// budget must still be a positive floor so the engine plays something
// rather than nothing.
func TestMoveTimeBudgetFloorsWhenAlmostOutOfTime(t *testing.T) {
	got := moveTimeBudget("white", gameState{WhiteTimeMS: 100, WhiteIncMS: 0})
	if got <= 0 {
		t.Errorf("got %v, want a positive floor even at 100ms remaining", got)
	}
	if got > 100*time.Millisecond {
		t.Errorf("got %v, more than what is actually left (100ms)", got)
	}
}

// A game rebuilt from an explicit FEN must track repetition, exactly as
// one rebuilt from the start position does. ParseFEN deliberately leaves
// tracking off, because the search builds hundreds of thousands of
// throwaway positions per move and none of them should pay for a map,
// but a game actually being played is not one of those. The move
// picker's only anti-shuffle rule is to prefer a move that does not
// return to a position it has already stood in, and that rule is dead
// without tracking: the bot would happily repeat its way into a draw
// from a winning position.
func TestApplyMovesStringTracksRepetitionFromAnyStart(t *testing.T) {
	fromStart, err := applyMovesString("startpos", "e2e4 e7e5")
	if err != nil {
		t.Fatal(err)
	}
	if !fromStart.TrackRepetition {
		t.Error("a game from the start position does not track repetition")
	}
	fromFEN, err := applyMovesString("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R b KQkq - 2 2", "f8c5")
	if err != nil {
		t.Fatal(err)
	}
	if !fromFEN.TrackRepetition {
		t.Error("a game rebuilt from a FEN does not track repetition, so the anti-shuffle rule is dead in it")
	}
}

// The clock has to be spent, not hoarded: real games used to end with a
// sixth to a quarter of it untouched, from 10.4 s of a 60 s clock to 84.5 s
// of a 180 s one, and unspent clock is unsearched depth. That is a property
// of a whole game rather than of one move, so it is checked over 120 moves
// against the ceilings in clocksim_test.go, next to the floors that stop
// the rule spending too much. Pinning a single move against the previous
// rule's arithmetic, which is what this test used to do, only measured how
// front loaded the rule was.

// It must not spend into the reserve while the reserve is intact: that is
// what keeps a long game from flagging. Simulated over 120 moves of
// bullet with no increment, the aggressive rules without a reserve reach
// zero and flag, and this one does not.
func TestMoveTimeBudgetLeavesTheReserveAlone(t *testing.T) {
	st := gameState{WhiteTimeMS: 60000, WhiteIncMS: 0}
	got := moveTimeBudget("white", st)
	reserve := 8 * time.Second
	if got > time.Duration(st.WhiteTimeMS)*time.Millisecond-reserve {
		t.Errorf("budget %v eats into the reserve on a %dms clock", got, st.WhiteTimeMS)
	}
}

// Once the reserve is gone the engine still has to move, so the budget
// stays positive and small rather than refusing or overrunning.
func TestMoveTimeBudgetStillMovesBelowTheReserve(t *testing.T) {
	st := gameState{WhiteTimeMS: 3000, WhiteIncMS: 0}
	got := moveTimeBudget("white", st)
	if got <= 0 {
		t.Error("budget must stay positive below the reserve")
	}
	if got >= 3000*time.Millisecond {
		t.Errorf("budget %v exceeds the whole remaining clock", got)
	}
}

// Without an increment there is nothing handing time back, so a long
// game drains the clock: simulated over 120 moves of 1+0, spending a
// twentieth of what is left each move exhausts it exactly and flags,
// while a thirtieth ends with half a second in hand. The share must
// therefore depend on whether there is an increment to lean on.
func TestMoveTimeBudgetIsMoreCautiousWithoutAnIncrement(t *testing.T) {
	withInc := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 1000})
	without := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 0})
	if without >= withInc {
		t.Errorf("no increment gave %v and an increment gave %v; the no-increment case must be the cautious one", without, withInc)
	}
	// It must also be at least as careful as the rule this replaced, which
	// spent a thirtieth of the clock. Pinning the exact constant is what
	// this test used to do, and that turned a safer rule into a failure.
	atMost := time.Duration(60000/30) * time.Millisecond
	if without > atMost {
		t.Errorf("got %v with no increment, want no more than the %v the old rule spent", without, atMost)
	}
}

// A reserve of ten increments is right on a 2+1 clock and absurd on a
// 1+10 one, where it would swallow the whole clock and make the engine
// think less with an increment than without. It is capped as a share of
// what is left.
func TestMoveTimeBudgetReserveCannotSwallowTheClock(t *testing.T) {
	big := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 10000})
	none := moveTimeBudget("white", gameState{WhiteTimeMS: 60000, WhiteIncMS: 0})
	if big <= none {
		t.Errorf("a 10s increment gave %v, less than no increment at %v", big, none)
	}
}

// Once the reserve is spent the engine is in the scramble and runs on a
// small share of what is left, and once even that is under the floor it
// runs on the floor. Both branches have to produce a move: an engine
// that returns nothing here loses on time for certain.
func TestMoveTimeBudgetScrambleBranches(t *testing.T) {
	// Below the reserve but well above the floor.
	if got := moveTimeBudget("white", gameState{WhiteTimeMS: 4000, WhiteIncMS: 0}); got <= 0 || got >= 4000*time.Millisecond {
		t.Errorf("scramble budget %v is not a sane slice of a 4s clock", got)
	}
	// Almost nothing left: the floor, and never more than the clock.
	got := moveTimeBudget("white", gameState{WhiteTimeMS: 120, WhiteIncMS: 0})
	if got <= 0 {
		t.Error("a nearly flagged clock still has to produce a move")
	}
	if got > 120*time.Millisecond {
		t.Errorf("budget %v exceeds the 120ms actually left", got)
	}
}

// A long clock must not make one move think for minutes: the cap applies
// before anything else, and 10.6 plies in a second already only buys
// about a ply per further second.
func TestMoveTimeBudgetCapAppliesOnAVeryLongClock(t *testing.T) {
	got := moveTimeBudget("white", gameState{WhiteTimeMS: 3600000, WhiteIncMS: 60000})
	if got != 15*time.Second {
		t.Errorf("got %v, want the 15s cap on a one hour clock", got)
	}
}

// A large increment on a nearly empty clock is the one case where the
// reserve still leaves more than the safety margin allows, so the final
// ceiling has to clamp it. Without that clamp the engine would spend
// past the flag.
func TestMoveTimeBudgetCeilingClampsABigIncrementOnATinyClock(t *testing.T) {
	st := gameState{WhiteTimeMS: 600, WhiteIncMS: 10000}
	got := moveTimeBudget("white", st)
	if got >= time.Duration(st.WhiteTimeMS)*time.Millisecond {
		t.Errorf("budget %v does not leave the safety margin on a 600ms clock", got)
	}
	if got <= 0 {
		t.Error("budget must stay positive")
	}
}

// With no clock reported this reports nothing and leaves the decision to
// the caller, which knows the game's speed. Deciding "unlimited" here,
// from a missing field alone, would hand a bullet game whose event simply
// omitted the clock a fifteen second think and flag it.
func TestNoClockDefersToTheCaller(t *testing.T) {
	if got := moveTimeBudget("white", gameState{}); got != 0 {
		t.Errorf("got %v with no clock, want 0 so the caller decides on the game's speed", got)
	}
}

// Correspondence is declined: it moves none of the four ratings being
// chased, holds a game slot for days, and its fifteen second searches run
// on the same cores as every real time game. One of those searches is what
// pushed blitz game hTmspQs0 a second a move over its budget until it
// flagged.
func TestCorrespondenceChallengesAreDeclined(t *testing.T) {
	if shouldAcceptChallenge(Challenge{Variant: "standard", SpeedTC: "correspondence"}) {
		t.Error("a correspondence challenge was accepted; its search starves the real time games")
	}
	for _, speed := range []string{"bullet", "blitz", "rapid", "classical"} {
		if !shouldAcceptChallenge(Challenge{Variant: "standard", SpeedTC: speed, Rated: true}) {
			t.Errorf("%s was declined; it is one of the modes being chased", speed)
		}
	}
}

// The exported wrapper has to stay the same rule as the one the bot plays
// by. It exists so clockaudit measures real games against this function
// rather than a copy, and a wrapper that drifted would quietly defeat that.
func TestExportedMoveTimeBudgetMatchesTheRuleTheBotPlaysBy(t *testing.T) {
	for _, c := range []struct{ remain, inc int64 }{
		{300000, 3000}, {120000, 1000}, {60000, 0}, {15000, 3000}, {500, 1000},
	} {
		want := moveTimeBudget("white", gameState{WhiteTimeMS: c.remain, WhiteIncMS: c.inc})
		if got := MoveTimeBudget(c.remain, c.inc); got != want {
			t.Errorf("MoveTimeBudget(%d, %d) = %v, want %v", c.remain, c.inc, got, want)
		}
	}
}

// Rated only, testing included: an unrated game costs the same cores and
// the same wall clock as a rated one and moves none of the four ratings.
func TestUnratedChallengesAreDeclined(t *testing.T) {
	for _, speed := range []string{"bullet", "blitz", "rapid", "classical"} {
		if shouldAcceptChallenge(Challenge{Variant: "standard", SpeedTC: speed, Rated: false}) {
			t.Errorf("an unrated %s challenge was accepted", speed)
		}
		if !shouldAcceptChallenge(Challenge{Variant: "standard", SpeedTC: speed, Rated: true}) {
			t.Errorf("a rated %s challenge was declined", speed)
		}
	}
}

package lichessbot

import (
	"chess/game"
	"testing"
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
	for _, speed := range []string{"bullet", "blitz", "rapid", "classical", "correspondence"} {
		c := Challenge{Variant: "standard", SpeedTC: speed}
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
	if !shouldAcceptChallenge(Challenge{Variant: ""}) {
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

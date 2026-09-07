package engine

import (
	"math"
	"reflect"
	"testing"

	"chess/game"
)

// The two entry points into the search must configure it identically.
//
// PlayerPick and PlayerScoreWith each built their Eval by hand, listing the
// fields they cared about, and the two lists had drifted apart.
// PlayerScoreWith is what labels every training position, so a field missing
// from its list meant the teacher that produced the labels was not the
// player that was raced afterwards.
//
// Checking by reflection rather than by naming fields is the point: a test
// that lists fields drifts in exactly the way the constructors did.
func TestEveryPlayerSettingReachesTheEvaluation(t *testing.T) {
	p := Player{
		Weights: &[6]float64{1, 3, 3, 5, 9, 0}, UsePST: true, Tapered: true,
		Structure: true, Mobility: true, KingSafety: 0.02, MaterialOnly: false,
		NullMove: true, Futility: true, Extensions: true, Aspiration: true,
		SEEPruning: true, QuiescePly: 4, HalfKPBlend: 0.45,
		NoCastle: true, NoLMR: true, ScaledLMR: true, NoRepetition: true,
		KeepNullMoveEP: true, NullReduction: 3, NullScale: true,
		Extras: true, Shape: true, ShapeW: &ShapeWeights{},
		PSTScale:   &[6]float64{1.1, 0.9, 1.2, 1.0, 1.3, 1.0},
		MobilityW:  &[6]float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6},
		StructureW: &StructureWeights{},
	}
	ev := evalForPlayer(p)

	pv, evv := reflect.ValueOf(p), reflect.ValueOf(*ev)
	pt := pv.Type()
	for i := 0; i < pt.NumField(); i++ {
		name := pt.Field(i).Name
		dst := evv.FieldByName(name)
		// Only fields the Eval actually has, with a matching type. The rest
		// (Depth, Name, TTBits) belong to the caller, not to the evaluation.
		if !dst.IsValid() || dst.Type() != pv.Field(i).Type() {
			continue
		}
		want, got := pv.Field(i).Interface(), dst.Interface()
		if !reflect.DeepEqual(want, got) {
			t.Errorf("Player.%s is %v but the Eval used for scoring has %v: "+
				"this setting never reaches the search", name, want, got)
		}
	}
}

// The behavioural half of the same thing: a tuned piece-square scale must
// change what the scoring path returns. It did not, because that field was
// only ever copied on the move-picking path.
func TestScoringHonoursTunedWeights(t *testing.T) {
	g, err := game.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3")
	if err != nil {
		t.Fatal(err)
	}
	base := exactPlayer(2)
	tuned := exactPlayer(2)
	tuned.PSTScale = &[6]float64{3, 3, 3, 3, 3, 1}

	a, ok1 := PlayerScoreWith(base, g, nil)
	b, ok2 := PlayerScoreWith(tuned, g, nil)
	if !ok1 || !ok2 {
		t.Fatal("no legal moves")
	}
	if math.Abs(a-b) < 1e-9 {
		t.Errorf("tripling every piece-square table changed nothing: both scored %.6f, "+
			"so PlayerScoreWith is ignoring the player's tuned weights", a)
	}
}

// The blend between the network and the hand evaluation must reach the
// scoring path too. This is the field whose loss did the real damage: the
// champion plays at 0.45 and labelled its training data at 0.
func TestScoringHonoursTheNetworkBlend(t *testing.T) {
	if got := evalForPlayer(Player{HalfKPBlend: 0.45}).HalfKPBlend; got != 0.45 {
		t.Errorf("HalfKPBlend arrives as %v, not 0.45: every position labelled by a "+
			"champion with a blended evaluation was labelled by a different player "+
			"than the one that plays", got)
	}
}

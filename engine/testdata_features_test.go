package engine

import (
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"chess/board"
	"chess/game"
)

// featureFixtureSlots are the king granularities written side by side, so a
// tool deriving coarse schemes from the 64-slot features can be checked
// against what the engine itself computes.
var featureFixtureSlots = []int{halfKPRawKingSquares, halfKPKingBuckets, halfKPKingSquares}

type featureFixturePerspectives struct {
	White []int32 `json:"white"`
	Black []int32 `json:"black"`
}

type featureFixturePosition struct {
	FEN      string                                `json:"fen"`
	Features map[string]featureFixturePerspectives `json:"features"`
}

// TestWriteFeatureFixture writes the fixture tools/features checks against;
// it only runs when CHESS_WRITE_FEATURE_FIXTURE names the output file.
func TestWriteFeatureFixture(t *testing.T) {
	path := os.Getenv("CHESS_WRITE_FEATURE_FIXTURE")
	if path == "" {
		t.Skip("set CHESS_WRITE_FEATURE_FIXTURE=<path> to write the feature fixture")
	}
	var positions []featureFixturePosition
	for _, g := range featureFixtureGames(t) {
		p := featureFixturePosition{FEN: g.FEN(), Features: map[string]featureFixturePerspectives{}}
		for _, slots := range featureFixtureSlots {
			p.Features[strconv.Itoa(slots)] = featureFixturePerspectives{
				White: AppendHalfKPFeaturesN([]int32{}, &g.Board, board.White, slots, false),
				Black: AppendHalfKPFeaturesN([]int32{}, &g.Board, board.Black, slots, false),
			}
		}
		positions = append(positions, p)
	}
	b, err := json.MarshalIndent(map[string][]featureFixturePosition{"positions": positions}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d positions to %s", len(positions), path)
}

// featureFixtureKingsides put a White king on the kingside of the upper
// half, which no correctness position or walk from one reaches.
var featureFixtureKingsides = []string{
	"6k1/8/6K1/8/8/8/8/R7 w - - 0 1",
	"3q4/1p3pk1/5N2/7K/8/8/P7/8 w - - 0 1",
}

// featureFixtureGames is every correctness position plus snapshots of a
// seeded random walk from each, which moves the kings off their home files.
func featureFixtureGames(t *testing.T) []*game.Game {
	rng := rand.New(rand.NewSource(25))
	snapshots := map[int]bool{6: true, 14: true, 24: true}
	var games []*game.Game
	fens := append(append([]string{}, correctnessPositions...), featureFixtureKingsides...)
	for _, fen := range fens {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		games = append(games, g)
		walk, _ := game.ParseFEN(fen)
		for ply := 1; ply <= 24; ply++ {
			legal := walk.AllLegalMoves(walk.Turn)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			walk.ApplyMove(m.From, m.To)
			if snapshots[ply] {
				snap, err := game.ParseFEN(walk.FEN())
				if err != nil {
					t.Fatalf("%s: %v", walk.FEN(), err)
				}
				games = append(games, snap)
			}
		}
	}
	return games
}

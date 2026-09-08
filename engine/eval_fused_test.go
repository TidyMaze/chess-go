package engine

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"

	"chess/board"
	"chess/game"
)

// The fused evaluation is a pure speed change: it must return exactly
// what the multi-pass version returned, on every configuration, or it is
// not an optimisation but a silent evaluation change that would show up
// later as an unexplained Elo movement.
func TestFusedEvaluationMatchesReference(t *testing.T) {
	positions := loadTestPositions(t, 4000)
	if len(positions) < 50 {
		t.Skipf("only %d positions available", len(positions))
	}

	configs := []struct {
		name string
		ev   *Eval
	}{
		{"material only", &Eval{Weights: DefaultWeights(), MaterialOnly: true}},
		{"centre bonus, no PST", &Eval{Weights: DefaultWeights()}},
		{"PST", &Eval{Weights: DefaultWeights(), UsePST: true}},
		{"tapered", &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true}},
		{"tapered + structure", &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true, Structure: true}},
		{"tuned", func() *Eval {
			scale, sw := TunedPSTScale(), TunedStructure()
			return &Eval{Weights: TunedWeights(), UsePST: true, Tapered: true,
				Structure: true, PSTScale: &scale, StructureW: &sw}
		}()},
		// The paths that replace or correct the hand score: the two forms
		// of the legacy network, and the king-conditioned one with a blend.
		{"replacing net", &Eval{Weights: DefaultWeights(), UsePST: true, Net: constantNet(0.75, false)}},
		{"residual net", &Eval{Weights: DefaultWeights(), UsePST: true, Net: constantNet(0.75, true)}},
		{"halfkp blended", &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true,
			HalfKP: tinyHalfKP(4, 8, false), HalfKPBlend: 0.45}},
		{"halfkp alone", &Eval{Weights: DefaultWeights(), UsePST: true, HalfKP: tinyHalfKP(4, 8, false)}},
		// Mobility, king safety, extras and shape are deliberately absent:
		// the reference predates them and never implemented them, so a
		// comparison on those settings measures nothing.
		{"material only", &Eval{Weights: DefaultWeights(), MaterialOnly: true}},
	}

	for _, c := range configs {
		mismatches := 0
		for _, g := range positions {
			for _, side := range [2]board.Color{board.White, board.Black} {
				want := positionScoreEvalReference(&g.Board, side, c.ev)
				got := PositionScoreEval(&g.Board, side, c.ev)
				if math.Abs(want-got) > 1e-9 {
					if mismatches < 3 {
						t.Errorf("%s: %s (%v to move): fused %.9f, reference %.9f",
							c.name, g.FEN(), side, got, want)
					}
					mismatches++
				}
			}
		}
		if mismatches > 0 {
			t.Errorf("%s: %d mismatches over %d positions", c.name, mismatches, len(positions))
		}
	}
}

func loadTestPositions(t *testing.T, limit int) []*game.Game {
	t.Helper()
	f, err := os.Open("../tuning_data.jsonl")
	if err != nil {
		// Fall back to self-play so the test still runs on a clean
		// checkout, where the generated data file is absent.
		var out []*game.Game
		p := Player{Depth: 2, UsePST: true, Quiescence: true, Tapered: true}
		for len(out) < 200 {
			g := game.New()
			for ply := 0; ply < 60 && len(out) < 200; ply++ {
				m, ok := PlayerPick(p, g)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				out = append(out, game.From(g.Board.Clone(), g.Turn))
			}
		}
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<22), 1<<22)
	var out []*game.Game
	for sc.Scan() && len(out) < limit {
		var batch []struct {
			FEN string `json:"fen"`
		}
		if json.Unmarshal(sc.Bytes(), &batch) != nil {
			continue
		}
		for _, s := range batch {
			g, err := game.ParseFEN(s.FEN)
			if err != nil {
				continue
			}
			out = append(out, g)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// constantNet is a legacy network whose every position evaluates to the
// same number, so the fused and reference evaluations can be compared on
// the paths a network takes without depending on trained weights.
func constantNet(out float32, residual bool) *Net {
	const hidden = 4
	return &Net{W1: make([]float32, nnueInputs*hidden), B1: make([]float32, hidden),
		W2: make([]float32, hidden), B2: out, Scale: 1, Residual: residual}
}

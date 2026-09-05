package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sync"

	"chess/board"
	"chess/game"
)

var pieceNames = map[board.PieceType]string{
	board.Pawn: "PAWN", board.Knight: "KNIGHT", board.Bishop: "BISHOP",
	board.Rook: "ROOK", board.Queen: "QUEEN", board.King: "KING",
}

var namePieces = func() map[string]board.PieceType {
	out := map[string]board.PieceType{}
	for pt, name := range pieceNames {
		out[name] = pt
	}
	return out
}()

func MutateWeights(w Weights, rate float64) Weights {
	out := CloneWeights(w)
	for pt := range out {
		if pt == board.King {
			continue
		}
		delta := (rand.Float64()*2 - 1) * rate * out[pt]
		out[pt] = math.Max(0.1, out[pt]+delta)
	}
	return out
}

func ResultScore(winner board.Color, decisive bool, perspective board.Color) float64 {
	if !decisive {
		return 0.5
	}
	if winner == perspective {
		return 1.0
	}
	return 0.0
}

// EloFromWinRate is the standard Elo win-probability inversion, anchored
// at 0 for an even score. Clamped, so a 100% score maps to a ceiling
// rather than infinity -- which also means this saturates and stops
// resolving further improvement once an opponent is beaten every time.
func EloFromWinRate(winRate float64) int {
	const clamp = 0.01
	winRate = math.Min(math.Max(winRate, clamp), 1-clamp)
	return int(math.Round(-400 * math.Log10(1/winRate-1)))
}

type GameResult struct {
	Winner   board.Color
	Decisive bool
	Plies    int
	Reason   string
}

// LiveHook is called after each move of a game, for the UI to show the
// game in progress. Nil for the games nobody is watching.
type LiveHook func(g *game.Game, plies int, from, to board.Sq)

func PlayGame(white, black Weights, depth, maxMoves int, live LiveHook) GameResult {
	g := game.New()
	plies := 0
	for plies < maxMoves && !g.IsOver() {
		weights := white
		if g.Turn == board.Black {
			weights = black
		}
		move, ok := ChooseMove(g, g.Turn, depth, weights)
		if !ok {
			break
		}
		g.ApplyMove(move.From, move.To)
		plies++
		if live != nil {
			live(g, plies, move.From, move.To)
		}
	}

	switch {
	case g.IsCheckmate(g.Turn):
		return GameResult{g.Turn.Other(), true, plies, "checkmate"}
	case g.KingCaptured:
		return GameResult{g.Turn.Other(), true, plies, "king captured"}
	case g.IsStalemate(g.Turn):
		return GameResult{board.White, false, plies, "stalemate"}
	case g.IsThreefoldRepetition():
		return GameResult{board.White, false, plies, "threefold repetition"}
	case g.IsFiftyMoveDraw():
		return GameResult{board.White, false, plies, "fifty-move rule"}
	default:
		return GameResult{board.White, false, plies, "move limit"}
	}
}

type championFile struct {
	Elo     int                `json:"vs_fixed_bot_elo"`
	Weights map[string]float64 `json:"weights"`
}

func SaveChampion(w Weights, elo int, path string) error {
	out := championFile{Elo: elo, Weights: map[string]float64{}}
	for pt, v := range w {
		out.Weights[pieceNames[pt]] = v
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadChampion returns the saved weights and the Elo they actually
// cleared. Resuming must restore that Elo exactly rather than
// re-measuring a fresh (noisy, small-sample) baseline, or a strong
// checkpoint can resume against a weak bar and be overwritten by
// something no better.
func LoadChampion(path string) (Weights, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var parsed championFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, 0, err
	}
	w := Weights{}
	for name, v := range parsed.Weights {
		w[namePieces[name]] = v
	}
	return w, parsed.Elo, nil
}

// playMatch runs `games` games between two weight sets concurrently --
// one goroutine per game, capped at GOMAXPROCS. Goroutines share memory
// directly, so unlike the Python port there's no process pool, no
// pickling of boards, and no pool-lifecycle bookkeeping.
func playMatch(a, b Weights, games, depth, maxMoves int, live LiveHook) []GameResult {
	results := make([]GameResult, games)
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup

	for i := 0; i < games; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var hook LiveHook
			if i == 0 {
				hook = live
			}
			if i%2 == 0 {
				results[i] = PlayGame(a, b, depth, maxMoves, hook)
			} else {
				r := PlayGame(b, a, depth, maxMoves, hook)
				results[i] = r
			}
		}(i)
	}
	wg.Wait()
	return results
}

// scoreMatch converts match results into a's score share, accounting for
// the colour swap on odd-numbered games.
func scoreMatch(results []GameResult) float64 {
	score := 0.0
	for i, r := range results {
		perspective := board.White
		if i%2 == 1 {
			perspective = board.Black
		}
		score += ResultScore(r.Winner, r.Decisive, perspective)
	}
	return score / float64(len(results))
}

func EvaluateVsFixedBot(w Weights, engineDepth, opponentDepth, games, maxMoves int) (float64, int) {
	fixed := DefaultWeights()
	results := playMatch(w, fixed, games, engineDepth, maxMoves, nil)
	winRate := scoreMatch(results)
	return winRate, EloFromWinRate(winRate)
}

type GenerationRecord struct {
	Generation   int      `json:"generation"`
	WinRate      float64  `json:"win_rate"`
	Promoted     bool     `json:"promoted"`
	Outcomes     []string `json:"outcomes"`
	VsFixedBot   int      `json:"vs_fixed_bot_elo"`
	CandidateElo *int     `json:"candidate_elo,omitempty"`
}

type Config struct {
	Generations    int
	GamesPerGen    int
	PopulationSize int
	Depth          int
	MaxMoves       int
	BenchmarkGames int
	RatchetGames   int
	Patience       int
	ChampionPath   string
	Live           LiveHook
	OnGeneration   func(GenerationRecord)
}

func Train(cfg Config) (Weights, []GenerationRecord) {
	if cfg.RatchetGames == 0 {
		cfg.RatchetGames = maxInt(cfg.BenchmarkGames*4, 24)
	}

	champion := DefaultWeights()
	bestElo := 0
	if loaded, elo, err := LoadChampion(cfg.ChampionPath); err == nil {
		champion, bestElo = loaded, elo
		fmt.Printf("Resuming from checkpoint: %s (vs fixed bot: elo %+d)\n", cfg.ChampionPath, bestElo)
	} else {
		_, bestElo = EvaluateVsFixedBot(champion, cfg.Depth, cfg.Depth, cfg.RatchetGames, cfg.MaxMoves)
		fmt.Printf("Baseline (default weights) vs fixed bot: elo %+d\n", bestElo)
	}

	history := []GenerationRecord{}
	stagnant := 0

	for gen := 1; gen <= cfg.Generations; gen++ {
		// Spread mutation rates across the population: one fixed rate for
		// every candidate explores too narrowly once the easy gains are
		// used up.
		bestWinRate, bestCandidate, bestOutcomes := -1.0, Weights(nil), []string(nil)
		for c := 0; c < cfg.PopulationSize; c++ {
			rate := 0.08 + 0.22*float64(c)/math.Max(1, float64(cfg.PopulationSize-1))
			candidate := MutateWeights(champion, rate)
			var hook LiveHook
			if c == 0 {
				hook = cfg.Live
			}
			results := playMatch(candidate, champion, cfg.GamesPerGen, cfg.Depth, cfg.MaxMoves, hook)
			winRate := scoreMatch(results)
			if winRate > bestWinRate {
				bestWinRate = winRate
				bestCandidate = candidate
				bestOutcomes = outcomesOf(results)
			}
		}

		record := GenerationRecord{Generation: gen, WinRate: bestWinRate, Outcomes: bestOutcomes, VsFixedBot: bestElo}

		// Elitism ratchet: winning its self-play match only qualifies a
		// candidate for testing. It's promoted only if it also doesn't
		// regress against the fixed external bot -- self-play win rate is
		// relative (a challenger can beat a champion that has itself
		// drifted weaker), so without this the tracked Elo just wanders.
		if bestWinRate > 0.5 {
			_, candidateElo := EvaluateVsFixedBot(bestCandidate, cfg.Depth, cfg.Depth, cfg.RatchetGames, cfg.MaxMoves)
			record.CandidateElo = &candidateElo
			if candidateElo >= bestElo {
				champion = bestCandidate
				bestElo = candidateElo
				record.Promoted = true
				record.VsFixedBot = bestElo
				if cfg.ChampionPath != "" {
					_ = SaveChampion(champion, bestElo, cfg.ChampionPath)
				}
			}
		}

		history = append(history, record)
		if cfg.OnGeneration != nil {
			cfg.OnGeneration(record)
		}

		status := "rejected"
		if record.Promoted {
			status = "promoted"
		} else if bestWinRate > 0.5 {
			status = "beat champion, rejected: regressed vs fixed bot"
		}
		fmt.Printf("Gen %d: challenger win rate %.0f%% (%s) | champion's best-ever vs fixed bot: elo %+d\n",
			gen, bestWinRate*100, status, bestElo)

		if record.Promoted {
			stagnant = 0
		} else {
			stagnant++
			if cfg.Patience > 0 && stagnant >= cfg.Patience {
				fmt.Printf("\nNo improvement in best-ever Elo for %d generations (stuck at %+d) -- stopping early.\n",
					cfg.Patience, bestElo)
				return champion, history
			}
		}
	}
	return champion, history
}

func outcomesOf(results []GameResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Reason
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

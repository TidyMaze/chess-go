package engine

import "testing"

// Ten games against Stockfish put ten of our searches and ten Stockfish
// processes on ten cores: ours got about 37% of a core a game and Stockfish
// about 50%, a timed match decided partly by the scheduler.
func TestAMatchAgainstAnExternalEngineRunsHalfAsManyGames(t *testing.T) {
	ours := Player{Depth: 4}
	external := Player{UCI: &UCIEngine{}}
	cases := []struct {
		name  string
		a, b  Player
		procs int
		want  int
	}{
		{"both in process", ours, ours, 10, 10},
		{"external reference", ours, external, 10, 5},
		{"external challenger", external, ours, 10, 5},
		{"never below one", ours, external, 1, 1},
	}
	for _, c := range cases {
		if got := matchWorkers(c.a, c.b, c.procs); got != c.want {
			t.Errorf("%s with %d procs: %d workers, want %d", c.name, c.procs, got, c.want)
		}
	}
}

package engine

import "fmt"

// LadderRung is one engine on the Elo ladder, with its rating derived by
// chaining match results up from the anchor.
type LadderRung struct {
	Player Player
	Elo    int
	Margin int
	Record MatchResult
}

// MeasureLadder rates a chain of engines, each measured against the one
// below it, with the first pinned at 0 Elo.
//
// Measuring everything directly against a single weak anchor does not
// work: once an engine wins ~95% of games the score saturates, the Elo
// estimate's confidence interval explodes, and further real improvement
// is invisible (a trap this project hit repeatedly). Chaining keeps every
// individual match near even, where the measurement is actually sharp,
// and sums the differences.
func MeasureLadder(players []Player, gamesPerRung, maxMoves int) []LadderRung {
	rungs := make([]LadderRung, len(players))
	rungs[0] = LadderRung{Player: players[0], Elo: 0}

	for i := 1; i < len(players); i++ {
		res := PlayMatch(players[i], players[i-1], gamesPerRung, maxMoves)
		rungs[i] = LadderRung{
			Player: players[i],
			Elo:    rungs[i-1].Elo + res.Elo(),
			Margin: res.EloMargin(),
			Record: res,
		}
	}
	return rungs
}

func (r LadderRung) String() string {
	if r.Record.Games() == 0 {
		return fmt.Sprintf("%-40s  ELO %+5d  (anchor)", r.Player.Name, r.Elo)
	}
	return fmt.Sprintf("%-40s  ELO %+5d  (+%d vs previous, W-D-L %d-%d-%d)",
		r.Player.Name, r.Elo, r.Record.Elo(), r.Record.Wins, r.Record.Draws, r.Record.Losses)
}

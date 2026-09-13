// Command clockaudit replays the clocks of real lichess games through the
// engine's own time budget rule and reports what the bot actually spent
// against what the rule told it to spend.
//
// It exists because that comparison had to be done by hand twice, and both
// times by reimplementing the budget formula in awk, which is the one thing
// this check must never do: a hand written copy of the rule is what once
// reported a game ending safely while the real function flagged. This calls
// lichessbot.MoveTimeBudget, the same function the bot plays by.
//
// Usage:
//
//	clockaudit -user tidymazebot games.pgn
//	curl -s -H "Authorization: Bearer $TOKEN" \
//	  "https://lichess.org/api/games/user/tidymazebot?max=20&clocks=true" |
//	  clockaudit -user tidymazebot
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"chess/lichessbot"
)

// game is one parsed PGN: the headers the audit needs and our own clock
// readings in move order.
type game struct {
	id          string
	timeControl string
	speed       string
	termination string
	result      string
	weAreWhite  bool
	clocksMS    []int64 // every clock reading, both sides, in move order
}

// baseAndIncrement splits a PGN TimeControl such as "300+3" into its two
// halves, in milliseconds. Anything it cannot read reports ok false rather
// than guessing, since a wrong increment silently corrupts every spend.
func baseAndIncrement(tc string) (baseMS, incMS int64, ok bool) {
	parts := strings.Split(strings.TrimSpace(tc), "+")
	if len(parts) != 2 {
		return 0, 0, false
	}
	base, err1 := strconv.ParseInt(parts[0], 10, 64)
	inc, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || base <= 0 {
		return 0, 0, false
	}
	return base * 1000, inc * 1000, true
}

var clockRe = regexp.MustCompile(`\[%clk (\d+):(\d+):(\d+)\]`)

// parseClocks pulls every [%clk h:mm:ss] out of a movetext line, in order.
// Lichess writes one after every move, so the readings alternate white,
// black, white, and the caller picks its own side out of them.
func parseClocks(movetext string) []int64 {
	var out []int64
	for _, m := range clockRe.FindAllStringSubmatch(movetext, -1) {
		h, _ := strconv.ParseInt(m[1], 10, 64)
		min, _ := strconv.ParseInt(m[2], 10, 64)
		s, _ := strconv.ParseInt(m[3], 10, 64)
		out = append(out, (h*3600+min*60+s)*1000)
	}
	return out
}

func headerValue(line string) string {
	i, j := strings.Index(line, `"`), strings.LastIndex(line, `"`)
	if i < 0 || j <= i {
		return ""
	}
	return line[i+1 : j]
}

// parsePGN reads a PGN stream into games. Only games carrying clocks are
// useful here, so games without them are returned too and skipped by the
// caller, which keeps "no clocks in this export" distinguishable from "no
// games".
func parsePGN(r io.Reader, user string) []game {
	var games []game
	var cur game
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[Site "):
			v := headerValue(line)
			cur.id = v[strings.LastIndex(v, "/")+1:]
		case strings.HasPrefix(line, "[White "):
			cur.weAreWhite = strings.EqualFold(headerValue(line), user)
		case strings.HasPrefix(line, "[TimeControl "):
			cur.timeControl = headerValue(line)
		case strings.HasPrefix(line, "[Event "):
			cur.speed = headerValue(line)
		case strings.HasPrefix(line, "[Termination "):
			cur.termination = headerValue(line)
		case strings.HasPrefix(line, "[Result "):
			cur.result = headerValue(line)
		case strings.HasPrefix(line, "1."):
			cur.clocksMS = parseClocks(line)
			games = append(games, cur)
			cur = game{}
		}
	}
	return games
}

// audit is what one game's clock says about the rule.
type audit struct {
	game
	moves                     int
	lowestMS                  int64
	meanBudgetMS, meanSpentMS int64
	// medianOverMS is the middle value of spent minus allowed across the
	// game, which estimates what a move costs beyond its search: network,
	// event handling, and whatever else the budget cannot control. The mean
	// hides it, because one 15 s think that finished early cancels out ten
	// moves that each paid half a second too much.
	medianOverMS                int64
	worstOverspendMS            int64
	worstOverspendAtRemainingMS int64
	flagged                     bool
}

// auditGame walks our own clock readings and compares each move's real cost
// against what the rule would have allowed at that moment. The cost of a
// move is the drop in our clock plus the increment we were handed back for
// playing it.
func auditGame(g game) (audit, bool) {
	_, incMS, ok := baseAndIncrement(g.timeControl)
	if !ok || len(g.clocksMS) < 4 {
		return audit{}, false
	}
	start := 0
	if !g.weAreWhite {
		start = 1
	}
	var ours []int64
	for i := start; i < len(g.clocksMS); i += 2 {
		ours = append(ours, g.clocksMS[i])
	}
	if len(ours) < 2 {
		return audit{}, false
	}
	a := audit{game: g, lowestMS: ours[0]}
	var sumBudget, sumSpent int64
	var overs []int64
	for i := 1; i < len(ours); i++ {
		remaining := ours[i-1]
		spent := remaining - ours[i] + incMS
		budget := lichessbot.MoveTimeBudget(remaining, incMS).Milliseconds()
		if over := spent - budget; over > a.worstOverspendMS {
			a.worstOverspendMS = over
			a.worstOverspendAtRemainingMS = remaining
		}
		overs = append(overs, spent-budget)
		sumBudget += budget
		sumSpent += spent
		a.moves++
		if ours[i] < a.lowestMS {
			a.lowestMS = ours[i]
		}
	}
	if a.moves > 0 {
		a.meanBudgetMS = sumBudget / int64(a.moves)
		a.meanSpentMS = sumSpent / int64(a.moves)
		sort.Slice(overs, func(i, j int) bool { return overs[i] < overs[j] })
		a.medianOverMS = overs[len(overs)/2]
	}
	a.flagged = strings.Contains(strings.ToLower(g.termination), "time forfeit")
	return a, true
}

func secs(ms int64) string { return fmt.Sprintf("%.1fs", float64(ms)/1000) }

func main() {
	user := flag.String("user", "tidymazebot", "whose clock to audit, as lichess spells it")
	floorMS := flag.Int64("floor-ms", 5000, "report a game whose clock ever fell below this")
	flag.Parse()

	in := io.Reader(os.Stdin)
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "clockaudit:", err)
			os.Exit(1)
		}
		defer f.Close()
		in = f
	}

	games := parsePGN(in, *user)
	if len(games) == 0 {
		fmt.Fprintln(os.Stderr, "clockaudit: no games found")
		os.Exit(1)
	}

	audited, low, flagged := 0, 0, 0
	fmt.Printf("%-10s %-9s %5s %9s %9s %9s %10s %12s\n",
		"game", "control", "moves", "lowest", "budget", "spent", "overhead", "worst over")
	for _, g := range games {
		a, ok := auditGame(g)
		if !ok {
			continue
		}
		audited++
		note := ""
		if a.lowestMS < *floorMS {
			note += "  LOW"
			low++
		}
		if a.flagged {
			note += "  FLAGGED"
			flagged++
		}
		fmt.Printf("%-10s %-9s %5d %9s %9s %9s %10s %12s%s\n",
			a.id, a.timeControl, a.moves, secs(a.lowestMS), secs(a.meanBudgetMS),
			secs(a.meanSpentMS), secs(a.medianOverMS), secs(a.worstOverspendMS), note)
	}
	if audited == 0 {
		fmt.Fprintln(os.Stderr, "clockaudit: no game carried clocks; export with clocks=true")
		os.Exit(1)
	}
	fmt.Printf("\n%d games with clocks, %d ever below %s, %d lost on time\n",
		audited, low, secs(*floorMS), flagged)
	if flagged > 0 {
		os.Exit(1)
	}
}

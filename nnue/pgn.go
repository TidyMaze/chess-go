package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// Learning from a games database, which is the honest source.
//
// The rule, from the user: a match history database is fair game, a dump of
// positions with scores already computed is not. Games give the engine what
// a human learns from, the moves people played and who won. Evaluations
// hand it another engine's judgement, which is the one thing this engine
// exists to produce for itself.
//
// So this reads PGN and takes exactly two things from it: the moves, and
// the result. Every score in the file is discarded, and stripComments is
// what does the discarding. Lichess ships `[%eval ...]` inside movetext
// comments for analysed games, so "PGN instead of the eval dump" is not by
// itself enough; the comments have to go.
//
// Labels are then computed here, by this engine's own search, exactly as
// the self-play path does. The only thing that changes is where the
// positions come from.

// pgnGame is one game reduced to what may be used.
type pgnGame struct {
	moves  []string
	result float64 // 1 White won, 0 Black won, 0.5 drawn
	// whiteElo and blackElo are 0 when the game did not say. The opening
	// book uses them to leave out weak games, whose most played move is
	// whatever was popular rather than whatever was good.
	whiteElo, blackElo int
}

// stripComments removes PGN comments and variations.
//
// This is the anti-cheating guard, not a formatting nicety. Everything a
// stronger engine knows about a position travels in these braces:
// `{ [%eval 2.35] [%clk 0:02:31] }`. A parser that merely ignores tokens it
// does not recognise would still read the moves correctly and would leave
// the door open for anything that later decides to parse them.
func stripComments(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '(':
			depth++
		case '}', ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(s[i])
			}
		}
	}
	return b.String()
}

// resultOf maps a PGN result tag to a score for White.
func resultOf(tag string) (float64, bool) {
	switch strings.TrimSpace(tag) {
	case "1-0":
		return 1, true
	case "0-1":
		return 0, true
	case "1/2-1/2":
		return 0.5, true
	}
	return 0, false // "*" is an unfinished game and says nothing
}

// moveToken strips the scaffolding SAN shares its line with and returns
// the move, or false if the token is not one.
//
// The move number is not reliably separated from the move: PGN in the wild
// writes both `1. e4` and `1.e4`, and the second form arrives as a single
// token. Rejecting anything starting with a digit therefore drops every
// move of a compactly written game, which is exactly what it did.
func moveToken(t string) (string, bool) {
	if t == "" || t == "*" || strings.HasPrefix(t, "$") {
		return "", false
	}
	if _, ok := resultOf(t); ok {
		return "", false
	}
	// Strip a leading move number and its dots: "12." or "12..." or "1.e4".
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i > 0 {
		for i < len(t) && t[i] == '.' {
			i++
		}
		if i == 0 || i >= len(t) {
			return "", false // a bare move number
		}
		t = t[i:]
	}
	if t == "" || t == "*" {
		return "", false
	}
	return t, true
}

// eloHeader reads a rating out of a PGN header such as [WhiteElo "2314"].
// Lichess writes "?" for an unrated player, which reads as no rating
// rather than as zero, and both end up excluded by any positive minimum.
func eloHeader(line, prefix string) (int, bool) {
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	i, j := strings.Index(line, "\""), strings.LastIndex(line, "\"")
	if i < 0 || j <= i {
		return 0, false
	}
	n, err := strconv.Atoi(line[i+1 : j])
	if err != nil {
		return 0, false
	}
	return n, true
}

// readPGN streams games. A game is its header block followed by its
// movetext, separated from the next by a blank line.
func readPGN(r io.Reader, out chan<- pgnGame) {
	defer close(out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)

	var moveText strings.Builder
	result, haveResult := 0.0, false
	whiteElo, blackElo := 0, 0
	flush := func() {
		text := stripComments(moveText.String())
		moveText.Reset()
		if !haveResult {
			return
		}
		var moves []string
		for _, t := range strings.Fields(text) {
			if mv, ok := moveToken(t); ok {
				moves = append(moves, mv)
			}
		}
		haveResult = false
		w, b := whiteElo, blackElo
		whiteElo, blackElo = 0, 0
		if len(moves) >= 10 {
			out <- pgnGame{moves: moves, result: result, whiteElo: w, blackElo: b}
		}
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			// A header line starting a new game means the previous one is
			// complete. PGN has no end-of-game marker other than this.
			if moveText.Len() > 0 {
				flush()
			}
			if n, ok := eloHeader(line, "[WhiteElo "); ok {
				whiteElo = n
			}
			if n, ok := eloHeader(line, "[BlackElo "); ok {
				blackElo = n
			}
			if strings.HasPrefix(line, "[Result ") {
				if i, j := strings.Index(line, "\""), strings.LastIndex(line, "\""); i >= 0 && j > i {
					result, haveResult = 0, false
					if r, ok := resultOf(line[i+1 : j]); ok {
						result, haveResult = r, true
					}
				}
			}
			continue
		}
		if line != "" {
			moveText.WriteString(line)
			moveText.WriteByte(' ')
		}
	}
	flush()
}

// ImportPGN replays games from a PGN stream and writes training samples,
// labelling every kept position with this engine's own search.
//
// labelDepth is how deep that search goes; lambda weights it against the
// game's actual result, the same blend the self-play path uses.
// ImportPGN's labeller is the current champion, not a fixed configuration.
//
// That is what makes the bootstrap ladder a ladder. Rung 0 labels with the
// hand-written evaluation, and a network fitted to those labels imitates
// it and plays like it: measured +0 +/- 22 over 1000 games, from two
// independent trainers. The only way the next network can be stronger is
// for its labels to come from a stronger player, which means the labeller
// has to pick up whatever was adopted last.
func ImportPGN(r io.Reader, poolPath string, maxKeep, labelDepth, skipPlies int,
	lambda, quietTol float64, progressEvery time.Duration, resume bool,
	labeller engine.Player, blendK float64) error {

	// Resume marker, in games consumed.
	//
	// Without it a run interrupted after an hour restarts from the first
	// game and appends everything a second time, so the pool silently gains
	// duplicates and the run costs twice. Games are the unit because they
	// are what the reader counts and what a restart can cheaply skip.
	progressPath := poolPath + ".progress"
	skipGames := int64(0)
	if resume {
		if data, err := os.ReadFile(progressPath); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
				skipGames = n
			}
		}
	}
	if skipGames > 0 {
		fmt.Printf("%s  resuming: skipping %d games already imported\n",
			time.Now().Format("15:04:05"), skipGames)
	}

	raw := make(chan pgnGame, 256)
	go readPGN(r, raw)

	// Drop the already-imported prefix before the workers see it, so the
	// skip costs a parse and not a labelling search.
	games := make(chan pgnGame, 256)
	go func() {
		defer close(games)
		seen := int64(0)
		for g := range raw {
			seen++
			if seen <= skipGames {
				continue
			}
			games <- g
		}
	}()

	out := make(chan []sample, runtime.NumCPU())
	var wg sync.WaitGroup
	var nGames, nKept, nBadReplay int64

	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A copy per worker: Player holds a transposition table
			// pointer that must not be shared across goroutines.
			labeler := labeller
			labeler.Depth = labelDepth
			// One table per worker for the whole run rather than one per
			// position: at 2^16 entries it stays in cache, and allocating
			// one per labelled position dominated everything else when the
			// self-play path did that.
			tt := engine.NewTranspositionTable(16)
			batch := make([]sample, 0, 256)

			for pg := range games {
				gi := atomic.AddInt64(&nGames, 1)
				g := game.New()
				kept := batch[:0:0]
				bad := false
				for ply, san := range pg.moves {
					m, ok := game.MoveFromSAN(g, san)
					if !ok {
						// A game that will not replay is skipped whole. It
						// is cheaper than importing positions from a line
						// that was never played.
						bad = true
						break
					}
					g.ApplyMove(m.From, m.To)
					if ply < skipPlies {
						continue
					}
					if moveIsNoisy(labeler, g, quietTol) {
						continue
					}
					score, ok := engine.PlayerScoreWith(labeler, g, tt)
					if !ok {
						continue
					}
					if g.Turn == board.Black {
						score = -score
					}
					if score > 12 {
						score = 12
					} else if score < -12 {
						score = -12
					}
					static := engine.PlayerStaticEval(labeler, &g.Board)
					var own, opp []int32
					own = engine.AppendHalfKPFeatures(own, &g.Board, board.White)
					opp = engine.AppendHalfKPFeatures(opp, &g.Board, board.Black)
					kept = append(kept, sample{
						own: own, opp: opp, static: static,
						// blendK > 0 blends in probability space, where a game
						// outcome belongs; 0 keeps the pawn-space blend.
						target: blendTarget(score, pg.result, lambda, blendK),
						game:   int32(gi),
					})
				}
				if bad {
					atomic.AddInt64(&nBadReplay, 1)
					continue
				}
				batch = append(batch, kept...)
				if len(batch) >= 256 {
					out <- batch
					batch = make([]sample, 0, 256)
				}
			}
			if len(batch) > 0 {
				out <- batch
			}
		}()
	}
	go func() { wg.Wait(); close(out) }()

	t0 := time.Now()
	last := time.Now()
	pending := make([]sample, 0, 8192)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := appendPool(poolPath, pending); err != nil {
			return err
		}
		// Written only after the samples are on disk, so a crash between
		// the two costs a re-read of some games, never a silent gap.
		_ = os.WriteFile(progressPath,
			[]byte(strconv.FormatInt(skipGames+atomic.LoadInt64(&nGames), 10)), 0644)
		pending = pending[:0]
		return nil
	}

	for b := range out {
		pending = append(pending, b...)
		atomic.AddInt64(&nKept, int64(len(b)))
		if len(pending) >= 8192 {
			if err := flush(); err != nil {
				return err
			}
		}
		if progressEvery > 0 && time.Since(last) > progressEvery {
			last = time.Now()
			k, gms := atomic.LoadInt64(&nKept), atomic.LoadInt64(&nGames)
			el := time.Since(t0)
			fmt.Printf("%s  %d games, %d positions kept, %.0f pos/s\n",
				time.Now().Format("15:04:05"), gms, k, float64(k)/el.Seconds())
		}
		if maxKeep > 0 && atomic.LoadInt64(&nKept) >= int64(maxKeep) {
			break
		}
	}
	if err := flush(); err != nil {
		return err
	}
	fmt.Printf("%s  imported %d positions from %d games in %s (%d games would not replay)\n",
		time.Now().Format("15:04:05"), nKept, nGames, time.Since(t0).Truncate(time.Second), nBadReplay)
	return nil
}

// moveIsNoisy reports whether a capture or tactic is pending, in which case
// the gap between a static score and a searched one is the tactic and no
// evaluation term can be blamed for it.
func moveIsNoisy(p engine.Player, g *game.Game, tol float64) bool {
	if len(g.AllLegalMoves(g.Turn)) == 0 {
		return true
	}
	static := engine.PlayerStaticEval(p, &g.Board)
	stm := static
	if g.Turn == board.Black {
		stm = -static
	}
	return math.Abs(engine.QuiescenceScore(p, g)-stm) > tol
}

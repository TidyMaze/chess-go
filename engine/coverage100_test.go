package engine

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/board"
	"chess/game"
)

// A fake UCI engine (a shell script speaking just enough of the protocol)
// so pickScored's default-depth branch can be exercised without a real
// Stockfish binary. It logs every line it reads to seen, then always
// answers with a fixed legal move.
func fakeUCIEngine(t *testing.T, seen string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-uci.sh")
	src := "#!/bin/sh\nwhile read -r line; do\n  echo \"$line\" >> " + seen + "\n  case \"$line\" in\n    uci*) echo uciok;;\n    isready) echo readyok;;\n    go*) echo \"bestmove e2e4\";;\n    quit) exit 0;;\n  esac\ndone\n"
	if err := os.WriteFile(script, []byte(src), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

// pickScored must default a non-positive UCIDepth to 1: the field exists
// so a caller can leave it unset, and a search depth of 0 sent to the
// external engine would not be "unlimited", it would be a malformed
// command. If the default were removed, the engine would be asked for
// "go depth 0" instead.
func TestPickScoredDefaultsUCIDepthToOne(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "cmds")
	if err := os.WriteFile(seen, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := NewStockfish(fakeUCIEngine(t, seen), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	p := Player{UCI: e, UCIDepth: 0}
	g := game.New()
	if _, _, ok := p.pickScored(g, nil); !ok {
		t.Fatal("pickScored with a fake UCI engine did not return a move")
	}
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "go depth 1") {
		t.Errorf("UCIDepth<=0 must default to depth 1, engine saw:\n%s", got)
	}
}

// PlayMatchSerial must classify a decisive game as a win or a loss for
// "a" depending on which colour a played, not just report draws. A book
// forcing Fool's Mate (1.f3 e5 2.g4 Qh4#) for both players makes every
// game end the same way regardless of random openings: white is always
// mated. With two games, a plays white once and black once, so the match
// must record exactly one win and one loss, never two draws.
func TestPlayMatchSerialRecordsWinsAndLosses(t *testing.T) {
	uciMoves := []string{"f2f3", "e7e5", "g2g4", "d8h4"}
	b := &Book{moves: map[string]string{}}
	g := game.New()
	for _, want := range uciMoves {
		wm, ok := game.MoveFromUCI(want)
		if !ok {
			t.Fatalf("bad UCI move literal %q", want)
		}
		var mv game.Move
		found := false
		for _, m := range g.AllLegalMoves(g.Turn) {
			if m.From == wm.From && m.To == wm.To {
				mv, found = m, true
				break
			}
		}
		if !found {
			t.Fatalf("move %s illegal in the forced mate sequence", want)
		}
		b.moves[BookKey(g.FEN())] = mv.UCI()
		g.ApplyMove(mv.From, mv.To)
	}
	if !g.IsCheckmate(g.Turn) {
		t.Fatal("the forced sequence must end in checkmate, test setup is wrong")
	}

	oldPlies := OpeningPlies
	OpeningPlies = 0
	defer func() { OpeningPlies = oldPlies }()

	a := Player{Name: "a", Book: b}
	bp := Player{Name: "b", Book: b}
	res := PlayMatchSerial(a, bp, 2, 10)
	if res.Wins != 1 || res.Losses != 1 || res.Draws != 0 {
		t.Errorf("forced fool's mate as both colours: wins=%d draws=%d losses=%d, want 1/0/1",
			res.Wins, res.Draws, res.Losses)
	}
}

// kingSafetyPenalty counts attacking pieces and indexes kingDangerScale
// (length 8) with that count, clamped at the last entry. Eight black
// queens each threatening a square next to the white king push the raw
// count to exactly 8, one past the last valid index (7): without the
// clamp, indexing kingDangerScale[8] panics with "index out of range".
// Confirmed by removing the clamp locally and re-running this test: it
// panicked at evalterms.go with that exact error.
func TestKingSafetyPenaltyClampsAttackersAtTableSize(t *testing.T) {
	g, err := game.ParseFEN("3qqq2/6k1/8/q1q4q/8/8/8/q3K2q w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	pieces := g.Board.AppendAllPieces(nil)
	got := kingSafetyPenalty(&g.Board, pieces, board.White, 1.0, 1.0)
	if got <= 0 {
		t.Fatalf("8 attacking queens next to the king must produce a positive penalty, got %v", got)
	}
	maxScale := kingDangerScale[len(kingDangerScale)-1]
	// weight=1, phase=1, so the result is exactly maxScale*weightSum. A
	// queen's attacker weight is 5 and each of the 8 queens hits at
	// least one square next to the king, so weightSum is at least 40.
	if got < maxScale*40 {
		t.Errorf("penalty %v is too small for 8 clamped attackers (want at least %v)", got, maxScale*40)
	}
}

// newShapeFiles builds a pawnFiles with a pawn on each listed file at the
// given owner-perspective rank, and -1 (no pawn) everywhere else, the way
// scanPawns initialises it.
func newShapeFiles(entries map[int]int) pawnFiles {
	pf := pawnFiles{}
	for i := range pf.mostAdv {
		pf.mostAdv[i] = -1
	}
	for f, r := range entries {
		pf.count[f] = 1
		pf.mostAdv[f] = r
		pf.anyPawns = true
	}
	return pf
}

// A pawn is backward when both neighbouring files have a friendly pawn
// strictly ahead of it: it can never again be defended by a pawn push.
// The d-pawn here has c- and e-pawns both two ranks ahead, so it must
// take the backward penalty. If the backward check were removed or
// inverted, this score would come back as just the connected bonus.
func TestShapeScoreBackwardPawnTakesThePenalty(t *testing.T) {
	own := newShapeFiles(map[int]int{2: 3, 3: 1, 4: 3}) // c, d, e files
	enemy := newShapeFiles(nil)
	pieces := []board.ColoredPiece{
		{Sq: board.Sq{File: 3, Rank: 1}, Type: board.Pawn, Color: board.White},
	}
	got := shapeScore(pieces, board.White, own, enemy, defaultShape)
	want := defaultShape.Connected - defaultShape.Backward
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("backward d-pawn scored %v, want %v (connected %v minus backward %v)",
			got, want, defaultShape.Connected, defaultShape.Backward)
	}
}

// A knight outpost needs a defending pawn and no enemy pawn ever able to
// evict it. Three knights, one on each combination the code branches on:
// d5 is defended (by the c-pawn) and never challenged, so it earns the
// outpost bonus; g5 is defended (by the f-pawn) but the enemy h-pawn can
// still reach past it, so it earns nothing; a5 has no defending pawn at
// all and must be skipped before the challenge check even runs. Only the
// first contributes to the score.
func TestShapeScoreKnightOutpostDefenceAndChallenge(t *testing.T) {
	own := newShapeFiles(map[int]int{2: 3, 5: 3}) // c and f pawns, three ranks up
	enemy := newShapeFiles(map[int]int{7: 1})     // h pawn, far enough to challenge g5
	pieces := []board.ColoredPiece{
		{Sq: board.Sq{File: 3, Rank: 4}, Type: board.Knight, Color: board.White}, // d5: outpost
		{Sq: board.Sq{File: 6, Rank: 4}, Type: board.Knight, Color: board.White}, // g5: challenged
		{Sq: board.Sq{File: 0, Rank: 4}, Type: board.Knight, Color: board.White}, // a5: undefended
	}
	got := shapeScore(pieces, board.White, own, enemy, defaultShape)
	want := defaultShape.Outpost
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("three knights (outpost, challenged, undefended) scored %v, want exactly the one outpost bonus %v",
			got, want)
	}
}

// bestMove on a nil table must report no move rather than dereferencing
// t.mask, which a nil receiver does not have. Move ordering can legally
// be asked about a position on a table that was never allocated (TTBits
// 0), and that must come back empty, not panic.
func TestBestMoveOnNilTableReturnsNoMove(t *testing.T) {
	var tt *TranspositionTable
	if m, ok := tt.bestMove(0xdeadbeef); ok || m != (game.Move{}) {
		t.Errorf("bestMove on a nil table returned move=%v ok=%v, want zero move, false", m, ok)
	}
}

// A table whose stored Pieces list disagrees in length with its own key
// cannot happen from real generation, but Probe and probeDTM must still
// come back empty rather than indexing garbage: encodePosition fails the
// length check before either ever touches tb.dtm.
func TestProbeAndProbeDTMRejectAMismatchedStoredPieceList(t *testing.T) {
	mismatched := &TablebaseSet{byKey: map[string]*Tablebase{
		"KQvK": {
			Pieces: []board.ColoredPiece{
				{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Queen},
				{Color: board.White, Type: board.Queen}, {Color: board.Black, Type: board.King},
			},
			dtm: map[uint32]int16{},
		},
	}}
	g, err := game.ParseFEN("4k3/8/8/8/8/8/8/4K2Q w - - 0 1") // real material: KQvK, 3 pieces
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mismatched.Probe(&g.Board, g.Turn); ok {
		t.Error("Probe answered from a table whose stored piece list does not match its own key")
	}
	if _, covered := mismatched.probeDTM(&g.Board, g.Turn); covered {
		t.Error("probeDTM reported coverage from a table whose stored piece list does not match its own key")
	}
}

// A position with legal material but too many pieces for any table
// (materialKey refuses more than 5) must miss cleanly through probeDTM
// too, not just through Probe.
func TestProbeDTMMissesOverLimitMaterial(t *testing.T) {
	set := setOf(t, krvk())
	if _, covered := set.probeDTM(&game.New().Board, board.White); covered {
		t.Error("probeDTM reported coverage for the 32-piece starting position")
	}
}

// King and rook against king with the kings adjacent is an illegal
// position (the side that just moved would be leaving its own king next
// to the enemy king, which the generator's buildPosition rejects), so it
// is never added to the table even though the material matches exactly.
// Probe must report this as a miss (dtm lookup fails), not as an
// (incorrect) draw score of exactly 0.
func TestProbeMissesAnIllegalPositionWithCoveredMaterial(t *testing.T) {
	set := setOf(t, krvk())
	g, err := game.ParseFEN("8/8/8/8/8/8/4k3/R3K3 w - - 0 1") // white king e1, black king e2: adjacent
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Probe(&g.Board, g.Turn); ok {
		t.Error("Probe answered for an illegal (adjacent kings) position the generator never visited")
	}
}

// StandardTablebases must start from bare kings (everything else is
// generated against it, in dependency order) and every configuration
// must carry exactly one king per side, or GenerateTablebase's own
// bookkeeping (which assumes exactly that) silently produces garbage.
func TestStandardTablebasesStartsFromBareKingsWithOneKingPerSide(t *testing.T) {
	sets := StandardTablebases()
	if len(sets) == 0 {
		t.Fatal("no configurations at all")
	}
	if len(sets[0]) != 2 || sets[0][0].Type != board.King || sets[0][1].Type != board.King ||
		sets[0][0].Color == sets[0][1].Color {
		t.Fatalf("first configuration %v, want bare kings first (everything else depends on it)", sets[0])
	}
	for _, cfg := range sets {
		whiteKings, blackKings := 0, 0
		for _, p := range cfg {
			if p.Type == board.King {
				if p.Color == board.White {
					whiteKings++
				} else {
					blackKings++
				}
			}
		}
		if whiteKings != 1 || blackKings != 1 {
			t.Errorf("configuration %v has %d white kings and %d black kings, want exactly one each",
				cfg, whiteKings, blackKings)
		}
	}
}

// BuildTablebases must call progress with the key and the entry count it
// just generated, so a long generation run can report where it is.
func TestBuildTablebasesReportsProgressPerSet(t *testing.T) {
	var gotKeys []string
	gotCounts := map[string]int{}
	BuildTablebases([][]board.ColoredPiece{
		{{Color: board.White, Type: board.King}, {Color: board.Black, Type: board.King}},
	}, func(key string, entries int) {
		gotKeys = append(gotKeys, key)
		gotCounts[key] = entries
	})
	if len(gotKeys) != 1 || gotKeys[0] != "KvK" {
		t.Fatalf("progress callback keys %v, want exactly [\"KvK\"]", gotKeys)
	}
	if gotCounts["KvK"] == 0 {
		t.Error("progress reported 0 entries for bare kings, which has drawn and stalemate positions")
	}
}

// A tablebase file cut off in the middle of a set's entry count must be
// rejected with a clear error, not read past the end of the buffer.
func TestLoadTablebasesRejectsATruncatedEntryCount(t *testing.T) {
	tb := &Tablebase{
		Pieces: []board.ColoredPiece{
			{Color: board.White, Type: board.King}, {Color: board.Black, Type: board.King},
		},
		dtm: map[uint32]int16{},
	}
	set := &TablebaseSet{byKey: map[string]*Tablebase{"KvK": tb}}
	var buf bytes.Buffer
	if err := set.writeSet(&buf); err != nil {
		t.Fatal(err)
	}
	full := buf.Bytes()
	truncated := full[:len(full)-2] // cut two bytes off the 4-byte entry count
	path := filepath.Join(t.TempDir(), "trunc.bin")
	if err := os.WriteFile(path, truncated, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTablebases(path); err == nil || !strings.Contains(err.Error(), "truncated entry count") {
		t.Errorf("loading a file cut off mid entry-count: err=%v, want an error mentioning \"truncated entry count\"", err)
	}
}

// Adjacent kings is an illegal position, but this engine's rule subset
// does not forbid it: from White's move the king simply captures its
// neighbour. playGameFrom must report that as a decisive king capture,
// not fall through to the checkmate or move-limit case.
func TestPlayGameFromReportsKingCaptured(t *testing.T) {
	w := Weights(&[6]float64{1, 3, 3, 5, 9, 0})
	res := playFromFEN(t, "8/8/8/8/8/8/1k6/K7 w - - 0 1", w)
	if res.Reason != "king captured" || !res.Decisive || res.Winner != board.White {
		t.Errorf("adjacent kings, white to move: %+v, want a decisive king capture for White", res)
	}
}

// Threefold repetition must be reported even when it is already true
// before playGameFrom's own loop runs a single ply. A knight shuffled
// out and back three times reaches the starting position a third time
// without a capture or a pawn move, so the position is over on entry and
// the switch at the end of playGameFrom must still catch it.
func TestPlayGameFromReportsThreefoldRepetition(t *testing.T) {
	g := game.New()
	for i := 0; i < 3; i++ {
		for _, uci := range []string{"g1f3", "g8f6", "f3g1", "f6g8"} {
			m, ok := game.MoveFromUCI(uci)
			if !ok {
				t.Fatalf("bad move literal %s", uci)
			}
			g.ApplyMove(m.From, m.To)
		}
	}
	if !g.IsThreefoldRepetition() {
		t.Fatal("the knight shuffle did not reach a threefold repetition, test setup is wrong")
	}
	w := Weights(&[6]float64{1, 3, 3, 5, 9, 0})
	res := playGameFrom(g, w, w, 1, 10, nil)
	if res.Reason != "threefold repetition" || res.Decisive {
		t.Errorf("already-threefold position: %+v, want a non-decisive threefold repetition", res)
	}
}

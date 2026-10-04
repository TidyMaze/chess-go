package engine

import (
	"math/rand"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

func TestMCTSMateInOneWhite(t *testing.T) {
	// Scholar's mate: Qxf7# is immediate mate in 1.
	g, err := game.ParseFEN("r1bqkb1r/pppp1ppp/2n5/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{
		Simulations: 400,
		MaxRollout:  20,
		Exploration: 1.414,
	}
	m, ok := MCTS(g, cfg)
	if !ok {
		t.Fatal("MCTS failed to return a move")
	}
	wantFrom := board.Sq{File: 7, Rank: 4} // h5
	wantTo := board.Sq{File: 5, Rank: 6}   // f7
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTS picked %v, want Qxf7# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSMateInOneBlack(t *testing.T) {
	// Fool's mate: Qh4# is immediate mate in 1 for Black (d8 -> h4).
	g, err := game.ParseFEN("rnbqkbnr/pppp1ppp/8/4p3/6P1/5P2/PPPPP2P/RNBQKBNR b KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{
		Simulations: 400,
		MaxRollout:  20,
		Exploration: 1.414,
	}
	m, ok := MCTS(g, cfg)
	if !ok {
		t.Fatal("MCTS failed to return a move")
	}
	wantFrom := board.Sq{File: 3, Rank: 7} // d8
	wantTo := board.Sq{File: 7, Rank: 3}   // h4
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTS picked %v, want Qh4# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSTakesFreeQueen(t *testing.T) {
	// White rook on e1, Black queen on e5 with no defenders. White must play Rxe5.
	g, err := game.ParseFEN("4k3/8/8/4q3/8/8/8/4R1K1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{
		Simulations: 600,
		MaxRollout:  20,
		Exploration: 1.414,
	}
	m, ok := MCTS(g, cfg)
	if !ok {
		t.Fatal("MCTS failed to return a move")
	}
	wantFrom := board.Sq{File: 4, Rank: 0} // e1
	wantTo := board.Sq{File: 4, Rank: 4}   // e5
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTS picked %v, want Rxe5 (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSTimeBudget(t *testing.T) {
	g := game.New()
	cfg := MCTSConfig{
		TimeBudget:  50 * time.Millisecond,
		MaxRollout:  15,
		Exploration: 1.414,
	}
	start := time.Now()
	_, ok := MCTS(g, cfg)
	elapsed := time.Since(start)
	if !ok {
		t.Fatal("MCTS failed to return a move")
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("MCTS exceeded budget: took %v for 50ms budget", elapsed)
	}
}

func TestPlayerMCTSIntegration(t *testing.T) {
	g := game.New()
	p := Player{
		Name:       "mcts-bot",
		MCTS:       true,
		TimeBudget: 20 * time.Millisecond,
	}
	m, ok := p.ChooseMove(g)
	if !ok {
		t.Fatal("player failed to choose move with MCTS")
	}
	if !g.IsLegalMoveFor(m, g.Turn) {
		t.Fatalf("chosen move %v is not legal", m)
	}
}

func TestMCTSDeterminism(t *testing.T) {
	g := game.New()
	cfg1 := MCTSConfig{
		Simulations: 200,
		MaxRollout:  15,
		Exploration: 1.414,
		RNG:         rand.New(rand.NewSource(42)),
	}
	cfg2 := MCTSConfig{
		Simulations: 200,
		MaxRollout:  15,
		Exploration: 1.414,
		RNG:         rand.New(rand.NewSource(42)),
	}
	m1, ok1 := MCTS(g, cfg1)
	m2, ok2 := MCTS(g, cfg2)
	if !ok1 || !ok2 {
		t.Fatal("MCTS failed")
	}
	if m1 != m2 {
		t.Fatalf("MCTS non-deterministic with same seed: got %v and %v", m1, m2)
	}
}

func TestMCTSTerminalCheckmate(t *testing.T) {
	// Position where Black is checkmated (Scholar's mate on board).
	g, err := game.ParseFEN("r1bqkb1r/pppp1Qpp/2n5/4p3/2B1P3/8/PPPP1PPP/RNB1K1NR b KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 100}
	_, ok := MCTS(g, cfg)
	if ok {
		t.Fatal("expected MCTS to return ok=false for terminal checkmate position")
	}
}

func TestMCTSDoesNotMutateGameRepetition(t *testing.T) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 50,
		MaxRollout:  10,
	}
	_, ok := MCTS(g, cfg)
	if !ok {
		t.Fatal("MCTS failed")
	}
	if g.IsThreefoldRepetition() {
		t.Fatal("MCTS corrupted parent game: reported threefold repetition")
	}
	e2 := board.Sq{File: 4, Rank: 1}
	e4 := board.Sq{File: 4, Rank: 3}
	if cnt := g.CountIfPlayed(e2, e4); cnt != 0 {
		t.Fatalf("MCTS mutated parent game repetition map: e2-e4 count=%d, expected 0", cnt)
	}
	if g.IsOver() {
		t.Fatal("MCTS corrupted parent game: reported game over")
	}
}

func BenchmarkMCTSv2(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		MaxRollout:  2,
		RNG:         rand.New(rand.NewSource(1)),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv2(g, cfg)
	}
}

func BenchmarkMCTSv3(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		RNG:         rand.New(rand.NewSource(1)),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv3(g, cfg)
	}
}

func BenchmarkMCTSv3_4Threads(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		Threads:     4,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv3(g, cfg)
	}
}

func BenchmarkMCTSv4(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		RNG:         rand.New(rand.NewSource(1)),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv4(g, cfg)
	}
}

func BenchmarkMCTSv4_4Threads(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		Threads:     4,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv4(g, cfg)
	}
}

func BenchmarkMCTSv1(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		MaxRollout:  30,
		RNG:         rand.New(rand.NewSource(1)),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv1(g, cfg)
	}
}

func BenchmarkMCTSv2_4Threads(b *testing.B) {
	g := game.New()
	cfg := MCTSConfig{
		Simulations: 500,
		MaxRollout:  2,
		Threads:     4,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MCTSv2(g, cfg)
	}
}

func TestMCTSv3MateInOneWhite(t *testing.T) {
	g, err := game.ParseFEN("r1bqkb1r/pppp1ppp/2n5/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 400}
	m, ok := MCTSv3(g, cfg)
	if !ok {
		t.Fatal("MCTSv3 failed to return a move")
	}
	wantFrom := board.Sq{File: 7, Rank: 4} // h5
	wantTo := board.Sq{File: 5, Rank: 6}   // f7
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv3 picked %v, want Qxf7# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSv3MateInOneBlack(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/pppp1ppp/8/4p3/6P1/5P2/PPPPP2P/RNBQKBNR b KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 400}
	m, ok := MCTSv3(g, cfg)
	if !ok {
		t.Fatal("MCTSv3 failed to return a move")
	}
	wantFrom := board.Sq{File: 3, Rank: 7} // d8
	wantTo := board.Sq{File: 7, Rank: 3}   // h4
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv3 picked %v, want Qh4# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSv3TakesFreeQueen(t *testing.T) {
	g, err := game.ParseFEN("4k3/8/8/4q3/8/8/8/4R1K1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 600}
	m, ok := MCTSv3(g, cfg)
	if !ok {
		t.Fatal("MCTSv3 failed to return a move")
	}
	wantFrom := board.Sq{File: 4, Rank: 0} // e1
	wantTo := board.Sq{File: 4, Rank: 4}   // e5
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv3 picked %v, want Rxe5 (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSDoesNotBlunderQueenToPawn(t *testing.T) {
	// White queen on d1, Black pawn on e5. White has choice of Qd4/Qf3 (safe) vs Qf4 (attacked by e5 pawn).
	// White must NOT blunder Qf4.
	g, err := game.ParseFEN("rnb1kbnr/pppp1ppp/8/4p3/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 500}
	m, ok := MCTSv3(g, cfg)
	if !ok {
		t.Fatal("MCTSv3 failed to return a move")
	}
	blunderFrom := board.Sq{File: 3, Rank: 0} // d1
	blunderTo := board.Sq{File: 5, Rank: 3}   // f4
	if m.From == blunderFrom && m.To == blunderTo {
		t.Fatalf("MCTSv3 blundered queen to pawn: %v", m)
	}
}

func TestMCTSv4MateInOneWhite(t *testing.T) {
	g, err := game.ParseFEN("r1bqkb1r/pppp1ppp/2n5/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 400}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return a move")
	}
	wantFrom := board.Sq{File: 7, Rank: 4} // h5
	wantTo := board.Sq{File: 5, Rank: 6}   // f7
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv4 picked %v, want Qxf7# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSv4MateInOneBlack(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/pppp1ppp/8/4p3/6P1/5P2/PPPPP2P/RNBQKBNR b KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 400}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return a move")
	}
	wantFrom := board.Sq{File: 3, Rank: 7} // d8
	wantTo := board.Sq{File: 7, Rank: 3}   // h4
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv4 picked %v, want Qh4# (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSv4TakesFreeQueen(t *testing.T) {
	g, err := game.ParseFEN("4k3/8/8/4q3/8/8/8/4R1K1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 600}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return a move")
	}
	wantFrom := board.Sq{File: 4, Rank: 0} // e1
	wantTo := board.Sq{File: 4, Rank: 4}   // e5
	if m.From != wantFrom || m.To != wantTo {
		t.Fatalf("MCTSv4 picked %v, want Rxe5 (%v->%v)", m, wantFrom, wantTo)
	}
}

func TestMCTSv4DoesNotBlunderQueenToPawn(t *testing.T) {
	g, err := game.ParseFEN("rnb1kbnr/pppp1ppp/8/4p3/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 500}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return a move")
	}
	blunderFrom := board.Sq{File: 3, Rank: 0} // d1
	blunderTo := board.Sq{File: 5, Rank: 3}   // f4
	if m.From == blunderFrom && m.To == blunderTo {
		t.Fatalf("MCTSv4 blundered queen to pawn: %v", m)
	}
}

func TestMCTSv4AvoidsTacticalBlunderG5(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/pp1pp3/2p2pp1/7p/P1PPP3/8/1P3PPP/RNBQKBNR b KQkq - 0 2")
	if err != nil {
		t.Fatal(err)
	}
	cfg := MCTSConfig{Simulations: 500}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return a move")
	}
	if m.UCI() == "g6g5" {
		t.Fatalf("MCTSv4 played suicidal blunder g6g5 allowing Bxh5+ and Qxh5#")
	}
}

func TestMCTSv4AvoidsHangingBishopA7(t *testing.T) {
	g, err := game.ParseFEN("rnb1kbnr/ppp1ppp1/3q4/3p3p/8/3PB1P1/PPP1PP1P/RN1QKBNR w KQkq h6 0 1")
	if err != nil {
		t.Fatal(err)
	}
	findMove := func(gm *game.Game, uci string) game.Move {
		for _, mv := range gm.AllLegalMoves(gm.Turn) {
			if gm.MoveUCI(mv) == uci {
				return mv
			}
		}
		return game.Move{}
	}
	g2 := *g
	mE3A7 := findMove(&g2, "e3a7")
	g2.Apply(mE3A7)
	t.Logf("After e3a7: White score = %f", PositionScoreEval(&g2.Board, board.White, mctsPSTEval))
	mA8A7 := findMove(&g2, "a8a7")
	g2.Apply(mA8A7)
	t.Logf("After a8a7: White score = %f", PositionScoreEval(&g2.Board, board.White, mctsPSTEval))
	t.Logf("tacticalMaterialPayoff after a8a7 = %f", tacticalMaterialPayoff(&g2.Board, mA8A7, board.Black))

	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return move")
	}
	if g.MoveUCI(m) == "e3a7" {
		t.Fatalf("MCTSv4 played blunder e3a7 hanging bishop to rook")
	}
}

func TestMCTSv4AvoidsBlunderC1G5(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/p1p2ppp/1p1p4/4p3/8/2PP1P2/PP2P1PP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	findMove := func(gm *game.Game, uci string) game.Move {
		for _, mv := range gm.AllLegalMoves(gm.Turn) {
			if gm.MoveUCI(mv) == uci {
				return mv
			}
		}
		return game.Move{}
	}
	g2 := *g
	mC1G5 := findMove(&g2, "c1g5")
	undo := g2.Board.MakeMove(mC1G5.From, mC1G5.To)
	t.Logf("IsAttackedBy(g5, Black): %v", g2.Board.IsAttackedBy(mC1G5.To, board.Black))
	t.Logf("IsAttackedBy(g5, White): %v", g2.Board.IsAttackedBy(mC1G5.To, board.White))
	t.Logf("payoff(c1g5): %f", tacticalMaterialPayoff(&g2.Board, mC1G5, board.White))
	g2.Board.UnmakeMove(undo)

	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("MCTSv4 failed to return move")
	}
	if g.MoveUCI(m) == "c1g5" {
		t.Fatalf("MCTSv4 blundered bishop to queen: c1g5")
	}
}

func TestMCTSv4AvoidsBlunderF1A6(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/p1p2ppp/1p1p4/4p3/8/2PP1P2/PP2P1PP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	movesToPlay := []string{"h2h4", "b8d7", "c1g5", "f8e7", "g5e7", "g8e7", "d3d4", "d6d5", "e2e4", "c8b7"}
	for _, uci := range movesToPlay {
		found := false
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == uci {
				g.Apply(m)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("could not play move %s", uci)
		}
	}
	findMove := func(gm *game.Game, uci string) game.Move {
		for _, mv := range gm.AllLegalMoves(gm.Turn) {
			if gm.MoveUCI(mv) == uci {
				return mv
			}
		}
		return game.Move{}
	}
	g2 := *g
	mF1A6 := findMove(&g2, "f1a6")
	g2.Apply(mF1A6)
	bMoves := g2.AllLegalMoves(g2.Turn)
	t.Logf("Black legal moves after f1a6: %d", len(bMoves))
	for _, bm := range bMoves {
		if g2.MoveUCI(bm) == "b7a6" {
			t.Logf("b7a6 IS LEGAL!")
		}
	}
	orderMovesForMCTS(bMoves, &g2.Board)
	for i, m := range bMoves {
		_, isCap := g2.Board.PieceAt(m.To)
		t.Logf("sorted %d: %s isCap=%v", i, g2.MoveUCI(m), isCap)
	}

	for run := 0; run < 10; run++ {
		cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
		m, ok := MCTSv4(g, cfg)
		if !ok {
			t.Fatal("failed to pick move")
		}
		t.Logf("run %d picked: %s", run, g.MoveUCI(m))
		if g.MoveUCI(m) == "f1a6" {
			t.Fatalf("run %d: MCTSv4 blundered bishop f1a6 to b7 bishop", run)
		}
	}
}

func TestMCTSv4AvoidsBlunderH6G4(t *testing.T) {
	// Game 6 position before Black move 3:
	// start fen: rnbqk2r/ppppbppp/7n/4p3/4PB2/3P4/PPP2PPP/RN1QKBNR w KQkq - 1 1
	// moves: f4e5 f7f6 e5f4 g7g5 f4e3
	g := mustFEN(t, "rnbqk2r/ppppbppp/7n/4p3/4PB2/3P4/PPP2PPP/RN1QKBNR w KQkq - 1 1")
	moves := []string{"f4e5", "f7f6", "e5f4", "g7g5", "f4e3"}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	g4 := board.Sq{File: 6, Rank: 3}
	h6 := board.Sq{File: 7, Rank: 5}
	t.Logf("IsAttackedBy(g4, White): %v", g.Board.IsAttackedBy(g4, board.White))
	t.Logf("IsAttackedByExcluding(g4, Black, h6): %v", g.Board.IsAttackedByExcluding(g4, board.Black, h6))
	movesCopy := g.AllLegalMoves(g.Turn)
	orderMovesForMCTS(movesCopy, &g.Board)
	for i, m := range movesCopy {
		if g.MoveUCI(m) == "h6g4" {
			t.Logf("h6g4 rank in ordered moves: %d / %d", i, len(movesCopy))
		}
	}
	for run := 0; run < 50; run++ {
		cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
		m, ok := MCTSv4(g, cfg)
		if !ok {
			t.Fatal("no move")
		}
		if g.MoveUCI(m) == "h6g4" {
			t.Fatalf("run %d: MCTSv4 blundered knight h6g4 to d1 queen", run)
		}
	}
}

func TestDebugGame2(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/pp1pp1p1/2p2p2/7p/P1P1P3/8/1P1P1PPP/RNBQKBNR w KQkq h6 0 1")
	moves := []string{
		"g1f3", "c6c5", "b2b4", "c5b4", "f3h4", "e7e5", "f1e2", "d7d5",
		"c4d5", "f6f5", "e2h5", "e8d7", "h4f5", "g8f6", "h5f7", "f6e4",
		"d1c2", "e4g5", "f7g6", "d8f6", "f5e3", "e5e4", "g6f5",
	}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	t.Logf("Turn: %v, InCheck: %v, fen: %s", g.Turn, g.Board.IsInCheck(g.Turn), g.FEN())
	for _, m := range g.AllLegalMoves(g.Turn) {
		t.Logf("legal evasion: %s", g.MoveUCI(m))
	}
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		if g.MoveUCI(child.move) == "d7d6" {
			t.Logf("d7d6 has %d visits:", child.visits)
			for wChild := child.firstChild; wChild != nil; wChild = wChild.nextSibling {
				t.Logf("  white reply: %s, visits: %d, wins: %.2f (rate: %.3f)", g.MoveUCI(wChild.move), wChild.visits, wChild.wins, wChild.wins/float64(wChild.visits))
			}
		}
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
}

func TestDebugGame1(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/pp1pp1p1/2p2p2/7p/P1P1P3/8/1P1P1PPP/RNBQKBNR w KQkq h6 0 1")
	moves := []string{
		"d2d4", "h5h4", "c4c5", "b7b6", "c5b6", "e7e6", "b6a7", "d8a5",
		"b1c3", "a8a7", "d4d5", "f8c5", "d5c6", "a5b6",
	}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	t.Logf("Turn: %v, fen: %s", g.Turn, g.FEN())
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
}

func TestDebugGame2BlunderB4A5(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/p2p1ppp/4p3/1pp5/8/2PP1P2/PP2P1PP/RNBQKBNR w KQkq - 0 1")
	moves := []string{
		"d1b3", "b5b4", "c3b4", "c5b4", "e2e3", "g8f6", "a2a3", "a7a5", "a3b4", "d8b6", "b3c4", "c8a6",
	}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	t.Logf("Turn: %v, fen: %s", g.Turn, g.FEN())
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		winRate := 0.0
		if child.visits > 0 {
			winRate = child.wins / float64(child.visits)
		}
		t.Logf("move: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f, untried: %d",
			g.MoveUCI(child.move), child.visits, child.wins, winRate, child.prior, child.movesUntried)
		if g.MoveUCI(child.move) == "b4a5" {
			t.Logf("--- b4a5 children replies ---")
			for reply := child.firstChild; reply != nil; reply = reply.nextSibling {
				rRate := 0.0
				if reply.visits > 0 {
					rRate = reply.wins / float64(reply.visits)
				}
				t.Logf("  reply: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
					g.MoveUCI(reply.move), reply.visits, reply.wins, rRate, reply.prior)
			}
		}
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("Worker picked: %s", g.MoveUCI(m))
}

func TestDebugGame1CheckEvasion(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/p2p1ppp/4p3/1pp5/8/2PP1P2/PP2P1PP/RNBQKBNR w KQkq - 0 1")
	moves := []string{"e2e4", "d7d5", "d3d4", "d5e4", "f1b5"}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	t.Logf("Turn: %v, InCheck: %v, fen: %s", g.Turn, g.Board.IsInCheck(g.Turn), g.FEN())
	for _, m := range g.AllLegalMoves(g.Turn) {
		t.Logf("legal check evasion: %s", g.MoveUCI(m))
	}
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	m, ok := MCTSv4(g, cfg)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
	if g.MoveUCI(m) == "e8e7" {
		t.Fatalf("MCTSv4 played suicidal king evasion e8e7 instead of interposing (c7c6, b8c6, c8d7, b8d7)")
	}
}

func TestDebugCheckEvasionE1E2(t *testing.T) {
	g := mustFEN(t, "r1bqk2r/pp1p2p1/n1p2nN1/7p/PbP1P3/8/1P3PPP/RNBQKB1R w KQkq - 3 2")
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		winRate := 0.0
		if child.visits > 0 {
			winRate = child.wins / float64(child.visits)
		}
		t.Logf("move: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
			g.MoveUCI(child.move), child.visits, child.wins, winRate, child.prior)
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
	if g.MoveUCI(m) == "e1e2" {
		t.Fatalf("MCTSv4 played suicidal e1e2 instead of interposing (c1d2, b1d2, b1c3)")
	}
}

func TestDebugGame1Move9(t *testing.T) {
	g := mustFEN(t, "r1b3nr/pp3kp1/5p2/3q3p/Pb1P4/2N2N2/1P3PPP/R1BQK2R w KQ - 0 1")
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		winRate := 0.0
		if child.visits > 0 {
			winRate = child.wins / float64(child.visits)
		}
		t.Logf("move: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
			g.MoveUCI(child.move), child.visits, child.wins, winRate, child.prior)
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
}

func TestDebugGame1RookTakesBishopH5(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/pp1pp1p1/2p2p2/7p/P1P1P3/8/1P1P1PPP/RNBQKBNR w KQkq h6 0 1")
	moves := []string{"b1c3", "e7e5", "d2d4", "e5d4", "f1e2", "d4c3", "e2h5"}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	t.Logf("Turn: %v, InCheck: %v, fen: %s", g.Turn, g.Board.IsInCheck(g.Turn), g.FEN())
	for _, m := range g.AllLegalMoves(g.Turn) {
		t.Logf("legal evasion: %s", g.MoveUCI(m))
	}
	gCopy := *g
	var mE8E7, mB2C3 game.Move
	for _, m := range gCopy.AllLegalMoves(gCopy.Turn) {
		if gCopy.MoveUCI(m) == "e8e7" {
			mE8E7 = m
		}
	}
	gCopy.Apply(mE8E7)
	t.Logf("Payoff after e8e7: %f", tacticalMaterialPayoff(&gCopy.Board, mE8E7, board.Black))
	for _, m := range gCopy.AllLegalMoves(gCopy.Turn) {
		if gCopy.MoveUCI(m) == "b2c3" {
			mB2C3 = m
		}
	}
	gCopy.Apply(mB2C3)
	t.Logf("Payoff after b2c3: %f", tacticalMaterialPayoff(&gCopy.Board, mB2C3, board.White))
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		winRate := 0.0
		if child.visits > 0 {
			winRate = child.wins / float64(child.visits)
		}
		t.Logf("move: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
			g.MoveUCI(child.move), child.visits, child.wins, winRate, child.prior)
		for reply := child.firstChild; reply != nil; reply = reply.nextSibling {
			rRate := 0.0
			if reply.visits > 0 {
				rRate = reply.wins / float64(reply.visits)
			}
			t.Logf("  reply: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
				g.MoveUCI(reply.move), reply.visits, reply.wins, rRate, reply.prior)
		}
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
	if g.MoveUCI(m) != "h8h5" {
		t.Fatalf("MCTSv4 picked %s, want h8h5 (taking checking bishop)", g.MoveUCI(m))
	}
}

func TestMCTSv4AvoidsBlunderDiscoveredCheck(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/p2p1ppp/4p3/1pp5/8/2PP1P2/PP2P1PP/RNBQKBNR w KQkq - 0 1")
	moves := []string{
		"b1a3", "c8a6", "c3c4", "d8b6", "c4b5", "a6b7", "c1f4", "d7d5",
		"f4b8", "a8b8", "d1a4", "g8h6", "a4f4", "b6d8", "f4e5", "a7a6",
		"b5a6", "b7a6", "e1c1", "c5c4", "d3c4", "d8c8", "c4c5", "f8c5",
	}
	for _, mStr := range moves {
		var found game.Move
		for _, m := range g.AllLegalMoves(g.Turn) {
			if g.MoveUCI(m) == mStr {
				found = m
				break
			}
		}
		g.Apply(found)
	}
	arena := mctsArenaV4Pool.Get().(*mctsArenaV4)
	arena.reset()
	var rootMovesBuf [128]game.Move
	legalMoves := g.AppendLegalMoves(rootMovesBuf[:0], g.Turn)
	cfg := MCTSConfig{TimeBudget: 10 * time.Millisecond}
	root := runMCTSv4Worker(g, cfg, legalMoves, 42, 0, arena)
	for child := root.firstChild; child != nil; child = child.nextSibling {
		winRate := 0.0
		if child.visits > 0 {
			winRate = child.wins / float64(child.visits)
		}
		t.Logf("move: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
			g.MoveUCI(child.move), child.visits, child.wins, winRate, child.prior)
		if g.MoveUCI(child.move) == "e5g7" {
			t.Logf("--- e5g7 replies ---")
			for reply := child.firstChild; reply != nil; reply = reply.nextSibling {
				rRate := 0.0
				if reply.visits > 0 {
					rRate = reply.wins / float64(reply.visits)
				}
				t.Logf("  reply: %s, visits: %d, wins: %.2f (rate: %.3f), prior: %.2f",
					g.MoveUCI(reply.move), reply.visits, reply.wins, rRate, reply.prior)
			}
		}
	}
	m, ok := pickBestRootMoveV4(g, root, legalMoves)
	if !ok {
		t.Fatal("no move")
	}
	t.Logf("MCTSv4 picked: %s", g.MoveUCI(m))
	if g.MoveUCI(m) == "e5g7" {
		t.Fatalf("MCTSv4 blundered queen: played e5g7 falling into discovered check c5d4+ and losing queen")
	}
}

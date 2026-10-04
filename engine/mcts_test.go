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

func TestCompareMCTS(t *testing.T) {
	g := game.New()
	cfg := MCTSConfig{
		TimeBudget: 50 * time.Millisecond,
	}
	mv4, _ := MCTSv4(g, cfg)
	mv3, _ := MCTSv3(g, cfg)
	mv2, _ := MCTSv2(g, cfg)
	mv1, _ := MCTSv1(g, cfg)
	t.Logf("MCTSv4: %s, MCTSv3: %s, MCTSv2: %s, MCTSv1: %s", g.MoveUCI(mv4), g.MoveUCI(mv3), g.MoveUCI(mv2), g.MoveUCI(mv1))
}






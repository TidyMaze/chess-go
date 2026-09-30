package lichessbot

import (
	"context"
	"testing"
	"time"

	"chess/engine"
)

// NanoZeroOrg accepted two games and never moved. Lichess left both
// "started" for more than a day, and with max-games 2 they held both slots:
// the bot declined every other challenge in that time. A game where either
// side has yet to make its first move can still be aborted, so after a
// minute the bot aborts it.
func TestAGameTheOpponentNeverStartsIsAborted(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/bot/game/stream/z1"] = `{"type":"gameFull","id":"z1","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"initialFen":"startpos","speed":"bullet","state":{"type":"gameState","moves":"","status":"started","wtime":60000,"btime":60000,"winc":1000,"binc":1000}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&syncBuf{}), FirstMoveTimeout: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.playGame(ctx, "z1")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range f.postedPaths() {
			if p == "/api/bot/game/z1/abort" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no abort after the opponent never moved; posts: %v", f.postedPaths())
}

// Once both sides have moved the game is a real game: no abort.
func TestAGameBothSidesStartedIsNotAborted(t *testing.T) {
	f := newFakeAPI()
	f.streams["/api/bot/game/stream/z2"] = `{"type":"gameFull","id":"z2","white":{"id":"opponent"},"black":{"id":"tidymazebot"},"initialFen":"startpos","speed":"bullet","state":{"type":"gameState","moves":"e2e4 e7e5 g1f3","status":"started","wtime":60000,"btime":60000,"winc":1000,"binc":1000}}` + "\n"
	b := &Bot{API: f, Player: engine.Strong(1), Username: "tidymazebot", Log: newBufLogger(&syncBuf{}), FirstMoveTimeout: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.playGame(ctx, "z2")
	time.Sleep(300 * time.Millisecond)
	for _, p := range f.postedPaths() {
		if p == "/api/bot/game/z2/abort" {
			t.Fatalf("aborted a game both sides had started; posts: %v", f.postedPaths())
		}
	}
}

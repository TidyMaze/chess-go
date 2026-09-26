package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/engine"
)

// 5.bxa8=N is followed by 6.Nc7+, a move only the knight can make. Queening
// instead makes Nc7+ unresolvable and the whole game is thrown away.
const underpromotionPGN = `[Result "1-0"]

1.e4 d5 2.exd5 c6 3.dxc6 Qb6 4.cxb7 Qxb2 5.bxa8=N Qxc1 6.Nc7+ Kd8 7.Nb5 Qxd1+ 8.Kxd1 Nc6 1-0
`

func TestPGNImportReplaysAnUnderpromotion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportPGN(strings.NewReader(underpromotionPGN), path, 0, 2, 0, 0.8, 0.35, 0, false, engine.Strong(2), 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadPool(path, 0); len(got) == 0 {
		t.Error("imported 0 samples: the game with 5.bxa8=N 6.Nc7+ did not replay")
	}
}

// A Lichess principal variation that underpromotes and then moves the new
// knight: every step has to be replayed on the real board.
func TestExtractBookReplaysAnUnderpromotion(t *testing.T) {
	dump := `{"fen":"4k3/8/8/8/8/8/p7/4K3 b - -","evals":[{"pvs":[{"cp":-500,"line":"a2a1n e1d2 a1b3"}],"depth":30}]}` + "\n"
	out := filepath.Join(t.TempDir(), "book.txt")
	if err := ExtractBook(strings.NewReader(dump), out, 0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "|a1b3") {
		t.Errorf("the knight move after a2a1n was not written, so the variation was replayed with a queen:\n%s", data)
	}
}

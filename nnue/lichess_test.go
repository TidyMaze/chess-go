package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sign convention is the single most expensive thing to get wrong
// here, and it was got wrong: the first version of this test asserted the
// UCI convention (score from the side to move) because that is what the
// engine's own Stockfish link uses, and the dump does not follow it. The
// scores are already White-relative.
//
// A test written from an assumption tests the assumption, not the file.
// This one now encodes what was measured against the real dump: over
// 15,834 records lopsided by five pawns or more, cp agrees with the side
// up material 72.5% of the time as written and 48.4% after a
// side-to-move flip, which is chance.
//
// The cost of the wrong version: labels almost exactly uncorrelated with
// material (0.012), a network that learned 0.8% of held-out variance, and
// a log that read as healthy throughout because the meaningless network
// still "beat" the hand evaluation on a loss the constant predictor beat
// as well.
func TestImportKeepsWhitesPointOfView(t *testing.T) {
	// Black to move, and the dump says White is winning. A flip would turn
	// this negative, so the sign of the stored target is the whole test.
	//
	// The black king stands on g8 rather than a corner because the import
	// drops positions that are in check, and every corner is attacked by a
	// queen on the first rank.
	const fen = "6k1/8/8/8/8/8/8/QQQQK3 b - -"
	line := fmt.Sprintf(`{"fen":%q,"evals":[{"depth":40,"pvs":[{"cp":5000}]}]}`, fen)

	pool := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportLichess(strings.NewReader(line), pool, 0, 0.35, false, false); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(pool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("imported %d positions, want 1", len(got))
	}
	if got[0].target <= 0 {
		t.Errorf("target %v: the dump's scores are already White-relative, so a "+
			"record saying White is winning must import positive whichever "+
			"side is to move", got[0].target)
	}
}

// The dump records several analyses per position at different depths.
// Taking the first rather than the deepest would silently train on
// shallow labels while the run looks identical.
func TestImportPicksTheDeepestAnalysis(t *testing.T) {
	const fen = "4k3/8/8/8/8/8/8/4K2R w K -"
	line := `{"fen":"` + fen + `","evals":[` +
		`{"depth":12,"pvs":[{"cp":100}]},` +
		`{"depth":45,"pvs":[{"cp":300}]},` +
		`{"depth":30,"pvs":[{"cp":200}]}]}`

	pool := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportLichess(strings.NewReader(line), pool, 0, 0.35, false, false); err != nil {
		t.Fatal(err)
	}
	got, _ := loadPool(pool, 0)
	if len(got) != 1 {
		t.Fatalf("imported %d, want 1", len(got))
	}
	// The depth-45 score of 300 centipawns, in pawns.
	if got[0].target < 2.99 || got[0].target > 3.01 {
		t.Errorf("target %v, want 3.0 from the depth-45 analysis", got[0].target)
	}
}

// Mates saturate any target and teach a static evaluation nothing, which
// is why the self-play path skips them too.
func TestImportSkipsMates(t *testing.T) {
	line := `{"fen":"6k1/6p1/8/4K3/4NN2/8/8/8 w - -","evals":[{"depth":40,"pvs":[{"mate":15}]}]}`
	pool := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportLichess(strings.NewReader(line), pool, 0, 0.35, false, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadPool(pool, 0); len(got) != 0 {
		t.Errorf("imported %d mate positions, want 0", len(got))
	}
}

// The dump mixes in variant positions. They must be rejected without
// stopping the import: one Horde game must not end a 21 GB stream.
func TestImportSurvivesVariantPositions(t *testing.T) {
	input := strings.Join([]string{
		`{"fen":"7K/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/q6k b - -","evals":[{"depth":40,"pvs":[{"cp":10}]}]}`,
		`{"fen":"4k3/8/8/8/8/8/8/4K2R w K -","evals":[{"depth":40,"pvs":[{"cp":300}]}]}`,
	}, "\n")
	pool := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportLichess(strings.NewReader(input), pool, 0, 0.35, false, false); err != nil {
		t.Fatalf("a variant position ended the import: %v", err)
	}
	if got, _ := loadPool(pool, 0); len(got) != 1 {
		t.Errorf("imported %d, want the 1 standard position", len(got))
	}
}

// A 21 GB stream will be interrupted. Restarting must not re-import what
// is already on disk, or the pool doubles up and the second run is wasted
// compute.
func TestImportResumesRatherThanRepeating(t *testing.T) {
	var b strings.Builder
	fens := []string{
		"4k3/8/8/8/8/8/8/4K2R w K -",
		"4k3/8/8/8/8/8/8/4K1R1 w - -",
		"4k3/8/8/8/8/8/8/4KR2 w - -",
		"4k3/8/8/8/8/8/8/4K3 w - -",
	}
	for _, f := range fens {
		fmt.Fprintf(&b, "{\"fen\":%q,\"evals\":[{\"depth\":40,\"pvs\":[{\"cp\":150}]}]}\n", f)
	}
	dir := t.TempDir()
	pool := filepath.Join(dir, "pool.bin")

	// First pass stops after two positions.
	if err := ImportLichess(strings.NewReader(b.String()), pool, 2, 0.35, false, false); err != nil {
		t.Fatal(err)
	}
	first, _ := loadPool(pool, 0)
	if len(first) == 0 {
		t.Fatal("first pass imported nothing")
	}
	progress, err := os.ReadFile(pool + ".progress")
	if err != nil {
		t.Fatalf("no progress marker written, so a restart cannot resume: %v", err)
	}
	if strings.TrimSpace(string(progress)) == "0" {
		t.Fatal("progress marker is 0 after importing positions")
	}

	// Second pass over the same input must add only what is left.
	if err := ImportLichess(strings.NewReader(b.String()), pool, 0, 0.35, false, true); err != nil {
		t.Fatal(err)
	}
	all, _ := loadPool(pool, 0)
	if len(all) > len(fens) {
		t.Errorf("pool holds %d positions after resuming over %d records: the resume re-imported",
			len(all), len(fens))
	}
}

// The tuner negates the score for a black-to-move position, so the file
// it reads must use the UCI convention (score from the side to move)
// while the dump uses White's point of view. Getting this backwards
// negates the label on half the positions and is invisible in every
// summary statistic; it is exactly what went wrong on the first import,
// where it left the labels correlating 0.012 with material.
func TestExtractTuningWritesSideToMoveScores(t *testing.T) {
	// Black to move, White winning by the dump's White-relative +5.00.
	// Written for the tuner, that must be -5.00, because the tuner will
	// negate it back.
	const fen = "6k1/8/8/8/8/8/8/QQQQK3 b - -"
	line := fmt.Sprintf(`{"fen":%q,"evals":[{"depth":40,"pvs":[{"cp":500}]}]}`, fen)

	path := filepath.Join(t.TempDir(), "tuning.jsonl")
	if err := ExtractTuning(strings.NewReader(line), path, 0, false, 0.35); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		FEN   string  `json:"fen"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unreadable record %q: %v", data, err)
	}
	if rec.Score >= 0 {
		t.Errorf("score %v: with Black to move and White winning, the tuner's "+
			"convention needs a negative number", rec.Score)
	}
}

func TestExtractTuningKeepsWhiteToMoveScores(t *testing.T) {
	const fen = "4k3/8/8/8/8/8/8/QQQQK3 w - -"
	line := fmt.Sprintf(`{"fen":%q,"evals":[{"depth":40,"pvs":[{"cp":500}]}]}`, fen)
	path := filepath.Join(t.TempDir(), "tuning.jsonl")
	if err := ExtractTuning(strings.NewReader(line), path, 0, false, 0.35); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var rec struct {
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unreadable record %q: %v", data, err)
	}
	if rec.Score <= 0 {
		t.Errorf("score %v: with White to move and White winning it must stay positive", rec.Score)
	}
}

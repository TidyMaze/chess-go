package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServesTheChampionAndFailsLoudlyWithoutOne(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "champion.json")
	if err := os.WriteFile(good, []byte(`{"label":"t","depth":2,"hand_blend":0.45}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if code := run([]string{"-champion", good}, strings.NewReader("uci\ngo depth 1\nquit\n"), &out, &errw); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
	if !strings.Contains(out.String(), "uciok") || !strings.Contains(out.String(), "bestmove ") {
		t.Errorf("did not serve UCI:\n%s", out.String())
	}
	if code := run([]string{"-champion", filepath.Join(dir, "absent.json")}, strings.NewReader("uci\n"), &out, &errw); code != 1 {
		t.Errorf("missing champion must exit 1, got %d", code)
	}
	if code := run([]string{"-bogus"}, strings.NewReader(""), &out, &errw); code != 2 {
		t.Errorf("bad flag must exit 2, got %d", code)
	}
}

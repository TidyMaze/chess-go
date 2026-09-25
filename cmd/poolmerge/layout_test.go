package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"chess/pool"
)

// poolWithMeta writes a one-record pool and, unless meta is "", its sidecar.
func poolWithMeta(t *testing.T, dir, name, meta string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	write(t, p, []record{{game: 0, own: []uint16{uint16(len(name))}, opp: []uint16{2}}})
	if meta != "" {
		if err := os.WriteFile(pool.MetaPath(p), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// runMain runs this command in a child process, since main exits on error.
func runMain(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMain$")
	cmd.Env = append(os.Environ(), "POOLMERGE_ARGS="+strings.Join(args, "\x1f"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestHelperMain(t *testing.T) {
	args := os.Getenv("POOLMERGE_ARGS")
	if args == "" {
		t.Skip("run by runMain")
	}
	os.Args = append([]string{"poolmerge"}, strings.Split(args, "\x1f")...)
	main()
	os.Exit(0)
}

// Pools of two feature layouts merged into one give an index two meanings,
// so the merge is refused, naming both files, and nothing is written.
func TestPoolsOfAnotherLayoutAreNotMerged(t *testing.T) {
	for _, c := range []struct{ name, a, b string }{
		{"mirrored and plain", `{"buckets": 8, "king_mirror": true}`, `{"buckets": 8, "king_mirror": false}`},
		{"mirrored and silent", `{"buckets": 8, "king_mirror": true}`, `{"buckets": 8}`},
		{"mirrored and no sidecar", `{"buckets": 8, "king_mirror": true}`, ""},
		{"8 and 32 buckets", `{"buckets": 8, "king_mirror": false}`, `{"buckets": 32, "king_mirror": false}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			a, b := poolWithMeta(t, dir, "a.bin", c.a), poolWithMeta(t, dir, "b.bin", c.b)
			if _, err := sameLayout([]string{a, b}); err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
				t.Errorf("sameLayout = %v, want an error naming %s and %s", err, a, b)
			}
			merged := filepath.Join(dir, "merged.bin")
			if out, err := runMain(t, "-out", merged, a, b); err == nil {
				t.Errorf("poolmerge exited 0:\n%s", out)
			}
			for _, p := range []string{merged, pool.MetaPath(merged)} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s was written (stat %v)", p, err)
				}
			}
		})
	}
}

// Pools that agree are merged, and the merged sidecar says the layout too, or
// the trainer would read a merge of mirrored pools as unmirrored.
func TestAMergeOfOneLayoutKeepsItInItsSidecar(t *testing.T) {
	for _, c := range []struct {
		name, a, b string
		mirror     bool
	}{
		{"mirrored", `{"buckets": 8, "king_mirror": true}`, `{"buckets": 8, "king_mirror": true}`, true},
		{"plain and silent", `{"buckets": 32, "king_mirror": false}`, `{"buckets": 32}`, false},
		{"no sidecars", "", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			a, b := poolWithMeta(t, dir, "a.bin", c.a), poolWithMeta(t, dir, "bb.bin", c.b)
			merged := filepath.Join(dir, "merged.bin")
			if out, err := runMain(t, "-out", merged, a, b); err != nil {
				t.Fatalf("poolmerge: %v\n%s", err, out)
			}
			if got := len(read(t, merged)); got != 2 {
				t.Errorf("merged %d records, want 2", got)
			}
			data, err := os.ReadFile(pool.MetaPath(merged))
			if err != nil {
				t.Fatal(err)
			}
			var meta map[string]any
			if err := json.Unmarshal(data, &meta); err != nil || meta["king_mirror"] != c.mirror {
				t.Errorf("merged sidecar %s (%v), want king_mirror %v", data, err, c.mirror)
			}
		})
	}
}

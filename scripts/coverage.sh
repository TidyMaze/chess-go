#!/bin/bash
# Coverage of the engine and AI packages, measured across packages: a
# statement counts as covered when any test in any of these packages
# executes it. Prints per-package coverage and, per file, the line ranges
# nothing executed. Target: 100% on every package listed.
#
# Usage: scripts/coverage.sh [go test flags...]
set -u
cd "$(dirname "$0")/.." || exit 1
PKGS=./board,./moves,./game,./engine,./nnue
PROFILE=/tmp/chesslogs/coverage.cov
mkdir -p /tmp/chesslogs
go test ./board ./moves ./game ./engine ./nnue -count=1 -timeout 1800s \
  -coverpkg="$PKGS" -coverprofile="$PROFILE" -covermode=count "$@" 2>&1 | grep -E "^(ok|FAIL|---)" 
echo
echo "== per package =="
go tool cover -func="$PROFILE" | awk '
  $1 != "total:" { split($1, a, "/"); pkg = a[2]; sub(/:.*/, "", pkg); }
  $1 == "total:" { print "total " $NF; next }
  { n[pkg]++; if ($NF == "100.0%") c[pkg]++ }
  END { for (p in n) printf "%-8s %d/%d functions fully covered\n", p, c[p], n[p] }' | sort
echo
echo "== uncovered line ranges =="
python3 - "$PROFILE" <<'PY'
import sys, collections
prof = sys.argv[1]
count = collections.defaultdict(int); stmts = {}
for line in open(prof):
    if line.startswith("mode:"): continue
    loc, st, c = line.split()
    count[loc] += int(c); stmts[loc] = int(st)
blocks = collections.defaultdict(list); tot = 0
for loc, c in count.items():
    if c == 0:
        f, rng = loc.split(":"); a, b = rng.split(",")
        blocks[f].append((int(a.split(".")[0]), int(b.split(".")[0]), stmts[loc])); tot += stmts[loc]
for f in sorted(blocks, key=lambda f: -sum(s for _, _, s in blocks[f])):
    rs = sorted(blocks[f]); merged = []
    for l1, l2, s in rs:
        if merged and l1 <= merged[-1][1] + 1: merged[-1] = (merged[-1][0], max(merged[-1][1], l2), merged[-1][2] + s)
        else: merged.append((l1, l2, s))
    print(f"{f} ({sum(s for _,_,s in rs)}): " + ", ".join(f"{a}-{b}" if a != b else f"{a}" for a, b, _ in merged))
print("TOTAL uncovered statements:", tot)
PY

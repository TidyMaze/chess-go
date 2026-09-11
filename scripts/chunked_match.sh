#!/bin/bash
# Run a long match as several short ones and pool the results.
#
# A 2,000 game match is a quarter of an hour during which nothing can be
# learned or changed. Four 500 game chunks give the same precision, print
# a running total after each, and can be stopped early when the direction
# is already clear. Each chunk gets a different opening offset so the
# chunks are different games rather than the same games repeated.
#
# Usage:
#   scripts/chunked_match.sh <total-games> <chunk-size> [gauntlet flags...]
# Stops early when the result has settled. A sequential test walks a
# log-likelihood ratio between two bounds; a feature at -70 Elo crosses
# the lower one after a hundred games and the remaining three hundred are
# pure cost. Set CHESS_SPRT=off to play every game regardless, and
# CHESS_SPRT_ELO1 to change what "worth having" means (default 15).
set -u

total=${1:?total games}
chunk=${2:?chunk size}
shift 2
SPRT=${CHESS_SPRT:-on}
SPRT_ELO1=${CHESS_SPRT_ELO1:-15}
# Every binary this script runs is built here. Three races in one evening
# ran on a gauntlet-bin older than the engine; the last one raced a network
# its loader could not read and reported "chunk failed" as the screen.
go build -o gauntlet-bin ./gauntlet || exit 1
if [ "$SPRT" != "off" ]; then
  go build -o sprtcheck-bin ./sprtcheck || exit 1
fi

# START_OFFSET lets a second run continue a match on fresh openings, so the
# two can be pooled instead of replaying the same 200 positions.
w=0; d=0; l=0; done_games=0; offset=${START_OFFSET:-0}
while [ "$done_games" -lt "$total" ]; do
  n=$chunk
  if [ $((done_games + n)) -gt "$total" ]; then n=$((total - done_games)); fi

  out=$(./gauntlet-bin -games "$n" -opening-offset "$offset" "$@" 2>&1 | tail -20)
  line=$(echo "$out" | grep "W-D-L" || true)
  if [ -z "$line" ]; then
    echo "chunk failed:"; echo "$out"; exit 1
  fi
  cw=$(echo "$line" | sed -E 's/.*W-D-L ([0-9]+)-([0-9]+)-([0-9]+).*/\1/')
  cd=$(echo "$line" | sed -E 's/.*W-D-L ([0-9]+)-([0-9]+)-([0-9]+).*/\2/')
  cl=$(echo "$line" | sed -E 's/.*W-D-L ([0-9]+)-([0-9]+)-([0-9]+).*/\3/')
  w=$((w + cw)); d=$((d + cd)); l=$((l + cl))
  done_games=$((done_games + n)); offset=$((offset + n))

  if [ "$SPRT" != "off" ]; then
    verdict=$(./sprtcheck-bin -w "$w" -d "$d" -l "$l" -elo0 0 -elo1 "$SPRT_ELO1" | tail -1)
    echo "$verdict"
  else
    verdict=""
  fi

  python3 - "$w" "$d" "$l" <<'PY'
import math, sys
w, d, l = (int(x) for x in sys.argv[1:4])
n = w + d + l
s = (w + 0.5 * d) / n
se = math.sqrt(max(s * (1 - s), 1e-9) / n)
elo = lambda p: -400 * math.log10(1 / min(max(p, 1e-6), 1 - 1e-6) - 1)
lo, hi = elo(max(0.001, s - 1.96 * se)), elo(min(0.999, s + 1.96 * se))
print("  pooled %d games: W-D-L %d-%d-%d, score %.3f, Elo %+.0f +/- %.0f"
      % (n, w, d, l, s, elo(s), (hi - lo) / 2))
PY

  case "$verdict" in
    *better|*worse)
      echo "  stopping early: the test has settled after $done_games games"
      break
      ;;
  esac
done

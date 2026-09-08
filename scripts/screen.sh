#!/bin/bash
# Screen one search feature quickly: the champion with the feature on
# against the champion without it, both on a 200ms per-move clock, paired
# openings. 400 games take about ten minutes and resolve +/- 34 Elo.
#
# Survivors go to the slow ruler (1s per move against Stockfish 2400,
# 300 games, calibrate -levels 2400 -features X); nothing is adopted on
# the screen alone.
#
# Usage: scripts/screen.sh <feature[,feature]> [games] [ms]
set -u
cd "$(dirname "$0")/.." || exit 1
FEATURE=${1:?feature name}
GAMES=${2:-400}
MS=${3:-200}
REF=/tmp/champ_t${MS}.json
python3 - "$MS" "$REF" <<'PY'
import json, sys
ms, out = int(sys.argv[1]), sys.argv[2]
c = json.load(open("champion.json"))
c["time_ms"], c["depth"] = ms, 5
c["label"] = "champion on a %dms clock (screening reference)" % ms
json.dump(c, open(out, "w"), indent=2)
PY
LOG=/tmp/chesslogs/screen_${FEATURE//,/_}_${MS}ms.log
echo "$(date +%H:%M:%S)  screening [$FEATURE] at ${MS}ms/move, $GAMES games -> $LOG"
./scripts/chunked_match.sh "$GAMES" 200 -depth 5 -halfkp champion_net.json -blend 0.45 \
  -time-ms "$MS" -features "$FEATURE" -ref-champion "$REF" -match-openings openings.txt > "$LOG" 2>&1
tail -1 "$LOG"

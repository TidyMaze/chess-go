#!/bin/bash
# Screen one search feature: the champion with the feature on against the
# champion without it, both on the same per-move clock, paired openings
# with colours reversed. 400 games resolve +/- 34 Elo.
#
# Use the clock the engine plays on, which is the default here. A short
# clock does not predict a long one and can invert the sign: reverse
# futility measured +40 +/- 34 at 200ms and -1 +/- 34 at 1000ms over the
# same 400 paired games, because at 200ms the engine is node-starved and
# saving nodes pays, while at 1000ms the approximation costs what it
# saves. Screening cheaply is a false economy when the answer changes.
#
# Calibration against Stockfish is for placing an adopted result on a
# public scale, not for comparing two builds: two independent samples
# against a third party have errors that add, and 600 such games resolved
# only +/- 59 where 400 paired ones resolve +/- 34.
#
# Usage: scripts/screen.sh <feature[,feature]> [games] [ms]
set -u
cd "$(dirname "$0")/.." || exit 1
FEATURE=${1:?feature name}
GAMES=${2:-400}
MS=${3:-1000}
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

# The binary must know the feature, or the whole screen fails in under a
# second and the chain runs on as though it had measured something. Three
# screens were lost that way to a gauntlet-bin built before the features
# it was being asked for.
go build -o gauntlet-bin ./gauntlet || exit 1
if ! ./gauntlet-bin -features "$FEATURE" -games 2 -depth 1 -max-moves 4 >/dev/null 2>&1; then
  echo "gauntlet does not accept -features $FEATURE"; exit 1
fi
echo "$(date +%H:%M:%S)  screening [$FEATURE] at ${MS}ms/move, $GAMES games -> $LOG"
./scripts/chunked_match.sh "$GAMES" 200 -depth 5 -halfkp champion_net.json -blend 0.45 \
  -time-ms "$MS" -features "$FEATURE" -ref-champion "$REF" -match-openings openings.txt > "$LOG" 2>&1
tail -1 "$LOG"

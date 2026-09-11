#!/bin/bash
# A ladder from nothing, small and fast, to prove the loop actually climbs.
#
# The production ladder starts from a champion that already has a network,
# where one rung is worth about ten Elo and needs thousands of games to see.
# From scratch the first rungs are worth hundreds, so a few hundred games at
# a tenth of a second settle each one, and the whole climb is visible in an
# hour instead of a week.
#
# It never touches champion.json. Its own descriptor, its own pools, its own
# networks, so the deployed engine is not at risk from an experiment.
#
# Usage: scripts/ladder_scratch.sh [rungs] [positions] [games] [ms]
set -u
cd "$(dirname "$0")/.." || exit 1

RUNGS=${1:-6}
POSITIONS=${2:-400000}
GAMES=${3:-300}
MS=${4:-100}
CHAMP=scratch_champion.json
LOG=/tmp/chesslogs/scratch.log
PGN=lichess_games_2015-01.pgn.zst

say() { echo "$(date +%H:%M:%S)  $*" | tee -a "$LOG"; }
mkdir -p /tmp/chesslogs nets_torch

# Rung 0 is the hand-written evaluation with no network at all.
if [ ! -f "$CHAMP" ]; then
  printf '{"label":"scratch rung 0: hand evaluation, no network","depth":4,"time_ms":%d,"hand_blend":0.45}\n' "$MS" > "$CHAMP"
  say "starting from the hand evaluation alone"
fi

say "ladder from scratch: $RUNGS rungs, $POSITIONS positions each, $GAMES games at ${MS}ms"

for rung in $(seq 1 "$RUNGS"); do
  pool="scratch_r${rung}.bin"
  net="nets_torch/scratch_r${rung}.json"
  label=$(python3 -c "import json;print(json.load(open('$CHAMP'))['label'])")
  say "rung $rung: labelling $POSITIONS positions with [$label] at depth 3, lambda 1"

  have=$([ -f "$pool" ] && ./nnue-bin -count-pool "$pool" 2>/dev/null || echo 0)
  if [ "${have:-0}" -lt "$POSITIONS" ]; then
    zstd -dcq "$PGN" 2>/dev/null | ./nnue-bin -import-pgn - \
      -pool-file "$pool" -import-max $((POSITIONS - have)) -label-depth 3 \
      -pgn-skip-plies 8 -label-champion "$CHAMP" -lambda 1 \
      >> "/tmp/chesslogs/scratch_gen_r${rung}.log" 2>&1
  fi
  say "rung $rung: pool holds $(./nnue-bin -count-pool "$pool") positions"

  # Every pool so far: volume is the one thing that has always mattered, and
  # the older pools were labelled by weaker champions but are still positions.
  pools=""
  for prev in $(seq 1 "$rung"); do [ -f "scratch_r${prev}.bin" ] && pools="$pools scratch_r${prev}.bin"; done
  say "rung $rung: training on [$pools]"
  .venv/bin/python -u pytorch/train.py --pool $pools --epochs 0 \
    --patience 15 --lr-decay 6 --hidden 64 --batch 16384 --device mps \
    --lr 0.005 --smooth 0.5 --fresh --loss mse \
    --checkpoint "nets_torch/scratch_r${rung}.ckpt" --out "$net" \
    --status nnue_status.json --label "scratch rung $rung" \
    > "/tmp/chesslogs/scratch_train_r${rung}.log" 2>&1
  say "rung $rung: trained, $(grep -oE 'explains [0-9.]+%' /tmp/chesslogs/scratch_train_r${rung}.log | tail -1)"

  # The contract that keeps training and play the same function.
  ./nnue-bin -emit-eval-check /tmp/scratch_ec.json -net-file "$net" > /dev/null 2>&1
  if ! .venv/bin/python pytorch/verify.py --net "$net" --cases /tmp/scratch_ec.json 2>/dev/null | grep -q identically; then
    say "rung $rung: STOPPING, the exported network does not evaluate identically in Go"
    exit 1
  fi

  # Screen, then confirm on openings the screen never used. Only the
  # confirmation decides, because a rung adopted on the race that selected
  # it is selected on its own test set.
  ./scripts/chunked_match.sh "$GAMES" 100 -depth 4 -time-ms "$MS" -max-moves 160 \
    -halfkp "$net" -blend 0.45 -ref-champion "$CHAMP" \
    -match-openings openings.txt > "/tmp/chesslogs/scratch_screen_r${rung}.log" 2>&1
  screen=$(grep pooled "/tmp/chesslogs/scratch_screen_r${rung}.log" | tail -1)
  say "rung $rung screen: $screen"
  selo=$(echo "$screen" | sed -nE 's/.*Elo ([-+][0-9]+) .*/\1/p')
  if [ -z "${selo:-}" ] || [ "$selo" -le 0 ]; then
    say "rung $rung: not adopted, the screen did not favour it. The ladder has stopped climbing."
    exit 0
  fi

  START_OFFSET=$((120000 + rung * 2000)) ./scripts/chunked_match.sh "$GAMES" 100 -depth 4 -time-ms "$MS" -max-moves 160 \
    -halfkp "$net" -blend 0.45 -ref-champion "$CHAMP" \
    -match-openings openings.txt > "/tmp/chesslogs/scratch_confirm_r${rung}.log" 2>&1
  result=$(grep pooled "/tmp/chesslogs/scratch_confirm_r${rung}.log" | tail -1)
  say "rung $rung confirm: $result"

  elo=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\1/p')
  margin=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\2/p')
  if [ -z "${elo:-}" ]; then say "rung $rung: STOPPING, no Elo in the confirmation"; exit 1; fi
  if [ $((elo - margin)) -gt 0 ]; then
    cp "$net" "scratch_net.json.tmp" && mv -f "scratch_net.json.tmp" scratch_net.json
    python3 - "$rung" "$elo" "$margin" "$CHAMP" "$MS" <<'PY'
import json, sys, time
rung, elo, margin, path, ms = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4], int(sys.argv[5])
c = json.load(open(path))
json.dump({"label": "scratch rung %s" % rung, "depth": 4, "time_ms": ms,
           "elo": c.get("elo", 0) + elo, "margin": margin,
           "net_file": "scratch_net.json", "hand_blend": 0.45,
           "adopted": time.strftime("%Y-%m-%dT%H:%M:%S%z")},
          open(path + ".tmp", "w"), indent=2)
import os; os.replace(path + ".tmp", path)
PY
    say "rung $rung: ADOPTED (+$elo +/- $margin, running total $(python3 -c "import json;print(json.load(open('$CHAMP'))['elo'])"))"
  else
    say "rung $rung: not adopted, the confirmation did not clear zero ($elo +/- $margin). The ladder has stopped climbing."
    exit 0
  fi
done
say "ladder from scratch finished all $RUNGS rungs"

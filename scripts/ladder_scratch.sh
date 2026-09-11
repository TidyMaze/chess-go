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
# The one rule that decides whether a rung is worth anything: the labelling
# search must be DEEPER than the search that plays. A network taught by a
# depth-3 search helps a shallower search and hurts a deeper one, because a
# deeper search already computes everything the network was taught and the
# network can then only add its own error. Measured on one network against
# one opponent: +14 at depth 1, +17 +/- 12 over 3500 games at depth 2, +16 at
# depth 4, and -36 to -66 at 100ms, which reaches 5 to 9 plies.
#
# Usage: scripts/ladder_scratch.sh [rungs] [positions] [games] [play-depth] [label-depth] [first-rung]
set -u
cd "$(dirname "$0")/.." || exit 1

RUNGS=${1:-4}
POSITIONS=${2:-800000}
GAMES=${3:-1500}
DEPTH=${4:-2}
LABEL_DEPTH=${5:-3}
FIRST=${6:-1}

if [ "$LABEL_DEPTH" -le "$DEPTH" ]; then
  echo "label depth $LABEL_DEPTH must be deeper than play depth $DEPTH, or the rung has nothing to teach" >&2
  exit 1
fi
# Build what this script runs; a race this evening went through a binary
# older than the engine and reported the loader's refusal as its result.
go build -o nnue-bin ./nnue || exit 1
# Nothing to run means stop here. BSD seq counts down when first is above
# last, so "seq 1 0" prints 1 and 0 and a run asked for zero rungs ran two.
[ "$RUNGS" -gt 0 ] || exit 0
CHAMP=scratch_champion.json
LOG=/tmp/chesslogs/scratch.log
PGN=lichess_games_2015-01.pgn.zst

say() { echo "$(date +%H:%M:%S)  $*" | tee -a "$LOG"; }
mkdir -p /tmp/chesslogs nets_torch

# Rung 0 is the hand-written evaluation with no network at all.
if [ ! -f "$CHAMP" ]; then
  printf '{"label":"scratch rung 0: hand evaluation, no network","depth":%d,"hand_blend":0.45}\n' "$DEPTH" > "$CHAMP"
  say "starting from the hand evaluation alone"
fi

say "ladder from scratch: $RUNGS rungs, $POSITIONS positions each, $GAMES games at fixed depth $DEPTH, labels at depth $LABEL_DEPTH"

for rung in $(seq "$FIRST" $((FIRST + RUNGS - 1))); do
  pool="scratch_r${rung}.bin"
  net="nets_torch/scratch_r${rung}.json"
  label=$(python3 -c "import json;print(json.load(open('$CHAMP'))['label'])")
  say "rung $rung: labelling $POSITIONS positions with [$label] at depth $LABEL_DEPTH, lambda 1"

  have=$([ -f "$pool" ] && ./nnue-bin -count-pool "$pool" 2>/dev/null || echo 0)
  if [ "${have:-0}" -lt "$POSITIONS" ]; then
    zstd -dcq "$PGN" 2>/dev/null | ./nnue-bin -import-pgn - \
      -pool-file "$pool" -import-max $((POSITIONS - have)) -label-depth "$LABEL_DEPTH" \
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
  ./scripts/chunked_match.sh "$GAMES" 250 -depth "$DEPTH" -max-moves 160 \
    -halfkp "$net" -blend 0.45 -ref-champion "$CHAMP" \
    -match-openings openings.txt > "/tmp/chesslogs/scratch_screen_r${rung}.log" 2>&1
  screen=$(grep pooled "/tmp/chesslogs/scratch_screen_r${rung}.log" | tail -1)
  say "rung $rung screen: $screen"
  selo=$(echo "$screen" | sed -nE 's/.*Elo ([-+][0-9]+) .*/\1/p')
  if [ -z "${selo:-}" ] || [ "$selo" -le 0 ]; then
    say "rung $rung: not adopted, the screen did not favour it. The ladder has stopped climbing."
    exit 0
  fi

  START_OFFSET=$((700000 + rung * 10000)) ./scripts/chunked_match.sh $((GAMES * 2)) 500 -depth "$DEPTH" -max-moves 160 \
    -halfkp "$net" -blend 0.45 -ref-champion "$CHAMP" \
    -match-openings openings.txt > "/tmp/chesslogs/scratch_confirm_r${rung}.log" 2>&1
  result=$(grep pooled "/tmp/chesslogs/scratch_confirm_r${rung}.log" | tail -1)
  say "rung $rung confirm: $result"

  elo=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\1/p')
  margin=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\2/p')
  if [ -z "${elo:-}" ]; then say "rung $rung: STOPPING, no Elo in the confirmation"; exit 1; fi
  if [ $((elo - margin)) -gt 0 ]; then
    cp "$net" "scratch_net.json.tmp" && mv -f "scratch_net.json.tmp" scratch_net.json
    python3 - "$rung" "$elo" "$margin" "$CHAMP" "$DEPTH" <<'PY'
import json, sys, time
rung, elo, margin, path, depth = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4], int(sys.argv[5])
c = json.load(open(path))
json.dump({"label": "scratch rung %s" % rung, "depth": depth,
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

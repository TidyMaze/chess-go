#!/bin/bash
# Run the bootstrap ladder unattended.
#
# One rung is: generate labels with the CURRENT champion, train a network on
# them, race it against that champion, and adopt it if it genuinely won. The
# next rung then labels with whatever was adopted, so each rung's teacher is
# stronger than the last. That is the only mechanism left that can raise
# strength without an external engine's evaluations: a network fitted to
# labels from player X imitates X and plays like X, measured at +0 +/- 22.
#
# Adoption rule: the 95% lower bound must clear zero. Not the 1% bar used
# for one-off changes, because a ladder that demands +19.6 per rung never
# takes its first step; but stricter than "point estimate positive", so it
# cannot drift upward on noise.
#
# Everything is resumable. The pool appends and records how many games it
# consumed, the trainer checkpoints on every improvement, and champion.json
# is the only state that carries between rungs.
#
# The smoothing prior is on at every rung and is not optional. Without it,
# a rung trained on 400k real-game positions measured -63 Elo; with it, -7.
# Fifty-six Elo from a regulariser, because Elo here tracks how much the
# evaluation moves between neighbouring positions and not how accurately it
# predicts.
#
# Usage: scripts/ladder.sh [rungs] [label-depth] [positions-per-rung]
set -u
cd "$(dirname "$0")/.." || exit 1

RUNGS=${1:-20}
DEPTH=${2:-5}
POSITIONS=${3:-400000}
GAMES=${4:-1000}
LOG=/tmp/chesslogs/ladder.log
PGN=lichess_games_2015-01.pgn.zst

say() { echo "$(date +%H:%M:%S)  $*" | tee -a "$LOG"; }

mkdir -p /tmp/chesslogs nets_torch
say "ladder starting: $RUNGS rungs, depth $DEPTH labels, $POSITIONS positions, $GAMES games per race"

for rung in $(seq 1 "$RUNGS"); do
  pool="ladder_r${rung}.bin"
  net="nets_torch/ladder_r${rung}.json"
  ckpt="nets_torch/ladder_r${rung}.ckpt"

  champ_label=$(python3 -c "import json;print(json.load(open('champion.json'))['label'])" 2>/dev/null || echo unknown)
  say "rung $rung: labelling with champion [$champ_label] at depth $DEPTH"

  # Generate. -label-champion is what makes this a ladder rather than a
  # loop: without it every rung would produce identical labels.
  if [ ! -f "$pool" ]; then
    zstd -dcq "$PGN" 2>/dev/null | ./nnue-bin -import-pgn - \
      -pool-file "$pool" -import-max "$POSITIONS" -label-depth "$DEPTH" \
      -pgn-skip-plies 8 -label-champion champion.json \
      >> /tmp/chesslogs/ladder_gen_r${rung}.log 2>&1
  fi
  kept=$(ls -la "$pool" 2>/dev/null | awk '{print $5}')
  say "rung $rung: pool $pool is ${kept:-0} bytes"

  # Train, until the held-out loss stops improving.
  .venv/bin/python -u pytorch/train.py --pool "$pool" --epochs 0 \
    --patience 20 --lr-decay 8 --hidden 64 --batch 16384 --device mps \
    --lr 0.005 --smooth 0.5 \
    --checkpoint "$ckpt" --out "$net" --status nnue_status.json \
    --label "ladder rung $rung, depth-$DEPTH labels" \
    > /tmp/chesslogs/ladder_train_r${rung}.log 2>&1
  explains=$(grep -oE "explains [0-9.]+%" /tmp/chesslogs/ladder_train_r${rung}.log | tail -1)
  say "rung $rung: trained, $explains"

  # The contract that keeps training and play the same function.
  ./nnue-bin -emit-eval-check /tmp/ladder_ec.json -net-file "$net" > /dev/null 2>&1
  if ! .venv/bin/python pytorch/verify.py --net "$net" --cases /tmp/ladder_ec.json 2>/dev/null | grep -q identically; then
    say "rung $rung: STOPPING, the exported network does not evaluate identically in Go"
    exit 1
  fi

  # Race it against the champion it was trained from.
  ./scripts/chunked_match.sh "$GAMES" 500 -depth 4 -halfkp "$net" -blend 0.45 \
    -match-openings openings.txt > /tmp/chesslogs/ladder_race_r${rung}.log 2>&1
  result=$(tail -1 /tmp/chesslogs/ladder_race_r${rung}.log)
  say "rung $rung: $result"

  elo=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\1/p')
  margin=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\2/p')
  if [ -z "${elo:-}" ]; then
    say "rung $rung: STOPPING, could not read an Elo from the race"
    exit 1
  fi

  lower=$((elo - margin))
  if [ "$lower" -gt 0 ]; then
    cp "$net" "champion_net.json"
    python3 - "$rung" "$elo" "$margin" <<'PY'
import json, sys, time
rung, elo, margin = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
c = json.load(open("champion.json"))
prev = c.get("elo", 1955)
json.dump({
    "label": "ladder rung %s: network trained on depth-labelled real games" % rung,
    "depth": 5, "elo": prev + elo, "margin": margin,
    "net_file": "champion_net.json", "hand_blend": 0.45,
    "adopted": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
}, open("champion.json", "w"), indent=2)
PY
    say "rung $rung: ADOPTED (+$elo, lower bound +$lower). The next rung labels with it."
  else
    say "rung $rung: not adopted (lower bound $lower). The ladder has stopped climbing."
    say "rung $rung: stopping here rather than relabelling with an unchanged champion,"
    say "            which would only reproduce this rung."
    exit 0
  fi
done
say "ladder finished all $RUNGS rungs"

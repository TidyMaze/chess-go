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
# Adoption rule: screen first, then confirm on openings the screen never
# used, and let only the confirmation decide. The 95% lower bound must
# clear zero there.
#
# The two stages exist because a rung adopted on the same race that
# measured it is selected on its own test set. Two networks from the
# identical recipe, differing only in weight initialisation and batch
# order, measured 18 +/- 16 Elo apart over 1750 games, and the gap
# repeated on a second set of openings. A rung is therefore mostly a draw
# from that spread, and "it won its race" is not evidence that it is
# better than the champion.
#
# Everything is resumable. The pool appends and records how many games it
# consumed, the trainer checkpoints on every improvement, and champion.json
# is the only state that carries between rungs.
#
# Labels are pure search scores (-lambda 1). The default 0.8 mixes a fifth
# of the game result into every target, and three arms measured at depth 4
# put lambda 0.5 at -52 +/- 44 and lambda 0.8 at -29 +/- 31 against lambda
# 1. Between engines this strong a game result is too noisy a statement
# about a position to pay for its variance. Every pool built before
# 2026-09-11 carries the 0.8 targets.
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
# 2500 games, not 1000.
#
# The ladder produces roughly +20 to +30 Elo per rung, and 1000 games has a
# margin of +/- 22, so a genuine +25 fails the "lower bound clears zero"
# test about half the time. Rung 2 measured +21 +/- 22 and stopped the
# ladder on a lower bound of -1. The effect was real; the ruler was too
# short. 2500 games gives about +/- 14.
GAMES=${4:-2500}
# 2026-09-08: the ladder does not climb, and it is not a measurement
# problem any more.
#
# Rung 8 was the first run with the transposition table fixed, so its
# labels came from a search that returns what it claims. Trained on 7.5M
# positions it measured -20 +/- 18 against its own teacher over 1500 games,
# an interval that excludes zero. Trained on only the 400k positions
# labelled by the fixed search, -61 +/- 18: volume beats label quality by a
# wide margin, so regenerating the whole corpus would not rescue it either.
#
# The reason is structural rather than a bug. The rung is trained to
# predict the champion's depth-5 score and then plays at depth 4, so the
# best it can do is reproduce its teacher, and distillation error makes it
# slightly worse. Nothing in this loop can produce information the champion
# did not already have. Every rung ever run has landed between +2 and -20,
# which is exactly what that predicts.
#
# What would change it is labelling deeper than the engine plays, so the
# targets carry something the playing search cannot reach. That is how
# Stockfish trains its network and it uses no outside evaluations, so it
# stays inside the rule that only the engine's own search may score a
# position. The earlier depth-3 against depth-5 label comparison ran
# through the broken table and has to be redone before it means anything.
# Where to resume. Rungs already adopted must not be re-run: their pools
# were labelled by an older champion, so retraining them against the
# current one races stale data and stops the ladder on a false negative.
START=${5:-1}
# Build what this script runs; a race this evening went through a binary
# older than the engine and reported the loader's refusal as its result.
go build -o nnue-bin ./nnue || exit 1
# Nothing to run means stop here. BSD seq counts down when first is above
# last, so "seq 1 0" prints 1 and 0 and a run asked for zero rungs ran two.
[ "$RUNGS" -gt 0 ] || exit 0
LOG=/tmp/chesslogs/ladder.log
PGN=lichess_games_2015-01.pgn.zst

say() { echo "$(date +%H:%M:%S)  $*" | tee -a "$LOG"; }

mkdir -p /tmp/chesslogs nets_torch
say "ladder starting: $RUNGS rungs, depth $DEPTH labels, $POSITIONS positions, $GAMES games per race"

for rung in $(seq "$START" $((START + RUNGS - 1))); do
  pool="ladder_r${rung}.bin"
  net="nets_torch/ladder_r${rung}.json"
  ckpt="nets_torch/ladder_r${rung}.ckpt"

  champ_label=$(python3 -c "import json;print(json.load(open('champion.json'))['label'])" 2>/dev/null || echo unknown)
  say "rung $rung: labelling with champion [$champ_label] at depth $DEPTH"

  # Generate. -label-champion is what makes this a ladder rather than a
  # loop: without it every rung would produce identical labels.
  #
  # "The file exists" is not "the pool is complete": a killed run leaves a
  # partial pool, and treating that as done would train the rung on a
  # fraction of its data and blame the labels. Positions average about 86
  # bytes, so compare against that and let the import resume, which it does
  # from a marker written only after the samples are on disk.
  # Counted, not estimated from the file size. Positions average 96 bytes
  # on real-game pools and 82 on self-play ones, so one constant misjudges
  # completeness by a fifth and a resumed rung would call its pool finished
  # at 80% of target and train on four fifths of the data.
  have=$([ -f "$pool" ] && ./nnue-bin -count-pool "$pool" 2>/dev/null || echo 0)
  if [ "${have:-0}" -lt "$POSITIONS" ]; then
    need=$((POSITIONS - have))
    say "rung $rung: generating $need more positions (have $have of $POSITIONS)"
    # -import-max counts this run only, so ask for the shortfall. Asking for
    # the full target after a resume overshoots by whatever was already
    # there, which is how rung 1 ended up with 730k instead of 600k.
    zstd -dcq "$PGN" 2>/dev/null | ./nnue-bin -import-pgn - \
      -pool-file "$pool" -import-max "$need" -label-depth "$DEPTH" \
      -pgn-skip-plies 8 -label-champion champion.json -lambda 1 \
      >> /tmp/chesslogs/ladder_gen_r${rung}.log 2>&1
    have=$(./nnue-bin -count-pool "$pool" 2>/dev/null || echo 0)
  fi
  say "rung $rung: pool $pool holds $have positions"

  # Train on everything accumulated, not just this rung's pool.
  #
  # Volume dominates here and it is not close: the same recipe measured -7
  # Elo on 400k positions and +35 on 3.18M. A rung that trains only on its
  # own 600k would lose to the champion on data alone, however much better
  # its labels are, and the ladder would stop on the wrong evidence.
  #
  # Older pools carry labels from weaker champions, which dilutes the
  # improvement. That is the trade Stockfish makes too, and at this scale
  # dilution is much cheaper than starving the network.
  pools="nnue_pool.bin games_d5.bin"
  for prev in $(seq 1 "$rung"); do
    [ -f "ladder_r${prev}.bin" ] && pools="$pools ladder_r${prev}.bin"
  done
  say "rung $rung: training on [$pools]"

  # Train, until the held-out loss stops improving.
  .venv/bin/python -u pytorch/train.py --pool $pools --epochs 0 \
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

  # Race it against the champion it was trained from, network included.
  #
  # -ref-champion is not optional and its absence invalidated the first six
  # rungs. Without it the gauntlet's reference is the plain hand-written
  # evaluation, so every rung was measured against the original baseline
  # instead of against its teacher. Seven deltas all measured the same
  # comparison, were summed as though they compounded, and produced a
  # claimed 2146 Elo. Calibration against Stockfish said 2041, the same
  # instrument put the baseline at 1987, and rung 6 raced against rung 5
  # measured +2 +/- 31. The ladder had been flat the whole time and the
  # harness could not see it.
  # Two stages, because a rung adopted on the same race that measured it
  # is selected on its own test set. Two networks from the identical
  # recipe measured 18 +/- 16 Elo apart over 1750 games, reproduced on
  # openings the first race never used, so a rung is largely a draw from
  # that spread and "it won its race" is not evidence it is better.
  #
  # The screen only decides whether confirming is worth the time. The
  # adoption decision belongs to the confirmation alone, which runs on
  # openings the screen never touched.
  ./scripts/chunked_match.sh 1000 250 -depth 4 -halfkp "$net" -blend 0.45 \
    -ref-champion champion.json \
    -match-openings openings.txt > /tmp/chesslogs/ladder_screen_r${rung}.log 2>&1
  screen=$(grep pooled /tmp/chesslogs/ladder_screen_r${rung}.log | tail -1)
  say "rung $rung screen: $screen"
  selo=$(echo "$screen" | sed -nE 's/.*Elo ([-+][0-9]+) .*/\1/p')
  if [ -z "${selo:-}" ] || [ "$selo" -le 0 ]; then
    say "rung $rung: not adopted (the screen did not favour it, so confirming would only buy a lucky second draw)."
    exit 0
  fi
  START_OFFSET=50000 ./scripts/chunked_match.sh "$GAMES" 250 -depth 4 -halfkp "$net" -blend 0.45 \
    -ref-champion champion.json \
    -match-openings openings.txt > /tmp/chesslogs/ladder_race_r${rung}.log 2>&1
  result=$(grep pooled /tmp/chesslogs/ladder_race_r${rung}.log | tail -1)
  say "rung $rung confirm (openings the screen never used): $result"

  elo=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\1/p')
  margin=$(echo "$result" | sed -nE 's/.*Elo ([-+][0-9]+) \+\/- ([0-9]+).*/\2/p')
  if [ -z "${elo:-}" ]; then
    say "rung $rung: STOPPING, could not read an Elo from the race"
    exit 1
  fi

  lower=$((elo - margin))
  # Elo is now a gain over the previous champion, so it compounds honestly
  # and the running total means something. It did not before.
  if [ "$lower" -gt 0 ]; then
    # Written then renamed, because the play server is live and watches
    # this file: a plain copy can be read half-written, and the watcher
    # would fall back to the hand evaluation mid-game. rename is atomic on
    # the same filesystem.
    cp "$net" "champion_net.json.tmp" && mv -f "champion_net.json.tmp" "champion_net.json"
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
}, open("champion.json.tmp", "w"), indent=2)
import os
os.replace("champion.json.tmp", "champion.json")
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

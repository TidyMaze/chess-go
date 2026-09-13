#!/bin/sh
# Generate a deep-labelled training pool while the lichess bot keeps
# playing, and keep going across restarts.
#
# The open question this answers is recorded in NEXT_STEPS.md: label depth
# 3, 5 and 8 all measured zero, but at 60000 positions, where the note
# says every arm was data-starved and that the result "cannot rule out
# that depth-8 labels pay off at a volume where the network is not
# starved". Depth 8 ran at 4 positions a second then, so volume was out of
# reach; it runs at about 24 now.
#
# Niced and capped so the bot, which is playing rated games on the same
# machine, always wins the contention.
set -u
cd "$(dirname "$0")/.." || exit 1
POOL=${POOL:-deep_d8.bin}
DEPTH=${LABEL_DEPTH:-8}
TARGET=${TARGET:-400000}
CORES=${CORES:-4}
PGN=lichess_games_2015-01.pgn.zst
LOG=/tmp/chesslogs/deep_label.log

# -count-pool prints its error to stdout and exits 0, so a missing file
# comes back as text, not as a failure. Keep only digits.
count() { ./nnue-bin -count-pool "$1" 2>/dev/null | tr -cd '0-9' | head -c 12; }
have=$(count "$POOL"); [ -n "$have" ] || have=0
echo "$(date +%H:%M:%S) pool $POOL holds $have of $TARGET at depth $DEPTH" >> "$LOG"
while [ "$have" -lt "$TARGET" ]; do
  need=$((TARGET - have))
  zstd -dcq "$PGN" 2>/dev/null | GOMAXPROCS="$CORES" nice -n 10 ./nnue-bin -import-pgn - \
    -pool-file "$POOL" -import-max "$need" -label-depth "$DEPTH" \
    -pgn-skip-plies 8 -label-champion champion.json -lambda 1 >> "$LOG" 2>&1
  new=$(count "$POOL"); [ -n "$new" ] || new=0
  echo "$(date +%H:%M:%S) pool now $new" >> "$LOG"
  [ "$new" -le "$have" ] && { echo "$(date +%H:%M:%S) no progress, stopping" >> "$LOG"; break; }
  have=$new
done
echo "$(date +%H:%M:%S) deep labelling done at $have positions" >> "$LOG"

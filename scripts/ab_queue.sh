#!/bin/sh
# Run a queue of A/B races one at a time. Time-based races must not overlap:
# two of them share the machine and both read the contention as weakness.
# Each entry is "<name> <challenger champion file>"; the reference is always
# the deployed champion, and every race is SPRT-gated.
set -u
games=${GAMES:-2000}; chunk=${CHUNK:-200}; ms=${MS:-10}
while read -r name file; do
  [ -z "$name" ] && continue
  log=/private/tmp/chesslogs/ab_$name.log
  echo "=== $name ($(date +%H:%M:%S)) ==="
  CHESS_SPRT=on CHESS_SPRT_ELO1=15 scripts/chunked_match.sh "$games" "$chunk" \
    -champion "$file" -ref-champion champion_bot.json -threads 1 -time-ms "$ms" > "$log" 2>&1
  tail -2 "$log"
done

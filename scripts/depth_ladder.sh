#!/bin/sh
# Elo per ply: the champion at depth d against itself at depth d-1, fixed
# depth, no clock, so machine load cannot tilt it. One line per rung in the
# results file, resumable: rungs already there are skipped.
#
# Usage: scripts/depth_ladder.sh <games> <maxdepth> <out.tsv>
set -u
games=${1:?games}; maxd=${2:?max depth}; out=${3:?out.tsv}
go build -o gauntlet-bin ./gauntlet || exit 1
[ -f "$out" ] || printf 'depth\tgames\tW\tD\tL\tscore\telo_vs_prev\tmargin\tseconds\n' > "$out"
d=2
while [ "$d" -le "$maxd" ]; do
  if awk -F'\t' -v d="$d" 'NR>1 && $1==d {found=1} END {exit !found}' "$out"; then d=$((d+1)); continue; fi
  t0=$(date +%s)
  res=$(./gauntlet-bin -games "$games" -champion /tmp/champion_depth.json -ref-champion /tmp/champion_depth.json \
        -cdepth "$d" -depth $((d-1)) -threads 1 2>&1 | tail -3)
  wdl=$(echo "$res" | sed -nE 's/.*W-D-L ([0-9]+)-([0-9]+)-([0-9]+) +score ([0-9.]+).*/\1\t\2\t\3\t\4/p')
  elo=$(echo "$res" | sed -nE 's/.*Elo gap ([-+][0-9]+) \+\/- ([0-9]+).*/\1\t\2/p')
  [ -z "$wdl" ] && { echo "rung $d failed:"; echo "$res"; exit 1; }
  printf '%s\t%s\t%s\t%s\t%s\n' "$d" "$games" "$wdl" "$elo" "$(( $(date +%s) - t0 ))" >> "$out"
  echo "depth $d vs $((d-1)): $(tail -1 "$out")"
  d=$((d+1))
done

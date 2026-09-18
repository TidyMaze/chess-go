# Queue

Nothing here runs while the generator holds the machine: a race at 100 ms a move
under ten busy workers measures the load, not the change. Check `uptime` and
`sysctl vm.swapusage` first.

## 1. Race razoring (ready, one command)

```
CHESS_SPRT=on CHESS_SPRT_ELO1=10 scripts/chunked_match.sh 2400 300 \
  -champion champion.json -ref-champion champion.json -features razoring \
  -threads 1 -time-ms 100
```

Same file on both sides, so the feature flag is the only difference. This is a
config A/B and it is valid here only because the flag is read at runtime by the
search: TestRazoringFlagReachesTheSearch and TestRazoringVisitsFewerNodes prove
it arrives and changes the node count (94.7% of plain over six positions).

## 2. Merge the new pool, retrain, race

The generator writes /private/tmp/chesslogs/deep_play6.bin with its provenance
sidecar. Check the first generation's positions merge cleanly against the
existing corpus BEFORE letting it run all night:

```
go run ./cmd/poolmerge -out /tmp/merge_probe.bin \
  /private/tmp/chesslogs/merged_dedup.bin /private/tmp/chesslogs/deep_play6.bin
```

What matters is the duplicate count. deep_d8.bin was 99% already present in
clean_r9, which is why re-labelling old PGN positions bought nothing; fresh
self-play at play-depth 6 should overlap far less.

## Closed this session

- deduplicated corpus: +15 +/- 11 over 3,600 games, adopted
- 128 hidden units: 1.3705 against 1.3618 held out, rejected without a race
- all five pools with duplicates: -19 +/- 20, rejected
- self-play positions alone: -26 +/- 20, rejected

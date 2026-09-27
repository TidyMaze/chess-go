# chess-go: Project State
> Updated: 2026-09-22 23:15 CEST

---

## Current State

### Champion: Rung 23
| Field | Value |
|---|---|
| Net | `net_v23_fine.json` → `champion_net.json` |
| Internal Elo | 3163 **as recorded, but rungs 19 to 23 were measured with a biased harness** (see below). Clean: rung 23 vs rung 19 net = +5 +/- 15 |
| Stockfish Elo | 2646 (vs SF2700: 5-5-6; vs SF2800: 0-7-9; 32 games at depth 10, so wide error bars) |
| Config | `champion.json`: depth 4, time_ms 1000, threads 8, tt_bits 22 |
| Features | lmp, deeplmp, rfp, scaledlmr, countermove, see, historymalus, lmrtwostep, nullgate, iir, kingsafety, improving, razoring, conthist, histlmr |
| Book | `games_book_v5.txt` |

### The ladder since rung 19 did not climb
Rungs 19 to 23 were raced with `gauntlet-bin -halfkp <net> -ref-champion champion.json` (via an uncommitted `scripts/gauntlet.py`). That puts the net on `engine.Strong` without the champion's search features, which at fixed depth 4 searches a wider tree and wins by itself.

All at 2,000 games, depth 4, `openings.txt`:
| Match | W-D-L | Elo |
|---|---|---|
| Null control: champion net via `-halfkp` vs champion | 709-800-491 | **+38 +/- 15** |
| net_v24_fine vs champion (both from champion files) | 575-846-579 | -1 +/- 15 |
| net_v25_fine vs champion | 518-782-700 | -32 +/- 15 |
| Champion vs champion config with the rung 19 net | 575-877-548 | +5 +/- 15 |

Rungs 20 to 23 claimed +194 together; measured cleanly they are +5 +/- 15. Rungs 13 to 16 used `-champion candidate.json` and stand; 17 and 18 used `-halfkp` at 10 ms (bias of unknown size and sign).

Fixed: `gauntlet-bin` now exits 2 on `-halfkp`/`-net` against `-ref-champion` without `-champion`. Clean recipe:
```bash
jq '.net_file="backup_pools/<net>.json"' champion.json > /tmp/chesslogs/cand.json
./gauntlet-bin -games 2000 -depth 4 -match-openings openings.txt -champion /tmp/chesslogs/cand.json -ref-champion champion.json
```

### Held-out split leaked across rungs (fixed)
`train.py` drew the held-out games with `randperm(len(games))`, so every rung re-drew them: 85% of rung 25's held-out games were training games under rung 24's split. Every fine-tune "stopped at epoch 1" because of it. The split is now a hash of the game id (`held_out_games`). The fine-tuned lineage has seen nearly every old game, so only a from-scratch net gets a clean held-out number.

### Speed of the champion's real search
The earlier perf commits were measured on `BenchmarkFullEngineDepth5`, which uses the hand evaluation. The champion runs `hand_blend 0`, so hand-eval work (`2e5c530`, pawn structure) never runs for it. `BenchmarkChampionSearch` (champion net and features, depth 8, one thread, 8 positions, seeded tie-break) measures the real workload:

| Build | ms/op | knps | nodes/op |
|---|---|---|---|
| `4aaf19a` (before the perf series) | ~300 | ~1,140 | 342,165 |
| `1ccaed2` (HEAD) | ~270 | ~1,260 | 342,628 |

About 11% faster, not +159%. Only `9483013` changes the tree: when the stand-pat cuts off it no longer checks for stalemate first, so a stalemated side that is ahead on material gets its material score. Rare; not fixed.

Search is not reproducible run to run without `SeedRandom`: the root tie-break (`search2.go`, `tied[randIntn(...)]`) draws from a time-seeded source every iteration.

### Background processes
| Process | Status |
|---|---|
| `lichessbot-bin` | Running, https://lichess.org/@/tidymazebot |
| `net_v25_scratch` training | Running, `tail -f /tmp/chesslogs/train_v25_scratch.log` |

---

## Next Steps
1. **Race `net_v25_scratch` cleanly** against the champion when training ends. First clean held-out number on this corpus.
2. **Fix or retire `ladder.sh`, `ladder_scratch.sh`, `screen.sh`**: they pass the refused `-halfkp ... -ref-champion` flags.
3. **Data is the lever that used to pay** (+22 for 3.8M new positions at rung 12 era). The corpus shrank from 24.58M (rung 19) to 13.67M; the last generation kept only 65% of its positions. Rebuild volume, and change openings or play depth if the duplicate rate keeps rising.
4. **Stalemate at stand-pat cutoff** (`9483013`): a failing test first, then decide if the cheap `HasAnyLegalMove` check costs less than it is worth.
5. `NEXT_STEPS.md` items still open: pawn-structure gap, label depth test, recalibration at 100 ms and 1 s.

---

## Learnings
- **A match only measures the network if both sides come from champion files.** Any other flag set changes search, and at fixed depth less pruning is a free +38.
- **A held-out set must be keyed on the record, not on its position in the run.** A seeded permutation over a growing count is a new sample every time.
- **Benchmark the workload that ships.** Optimising the hand evaluation bought nothing for a champion that never calls it.
- **A pure speed-up must leave the node count identical.** Seed the tie-break and compare nodes before timing anything; that is how `9483013` was found.
- **HalfKP accumulator order is bit-exact**: keep `occupied[]` traversal order or update every golden value.
- **Avoid maps on hot paths**; arrays indexed by small integers do not allocate.

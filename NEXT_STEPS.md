# Queue

The absolute scale changed on 2026-09-18: the engine is about 1,985 at 10 ms,
not the 2,493 the SF@2800 rung implied. See `analyses/elo-calibration/report.html`.
Every A/B in LEARNINGS.md still stands, they are head to head at a fixed clock.

## 1. Move agreement audit against Stockfish as a judge

An engine that searches 12 plies at 1 s and rates 1,985 has evaluation defects,
not diminishing returns. Find them: take a few hundred positions from the pools,
ask for our best move and Stockfish's best move at a fixed depth, and bucket the
disagreements by phase and by what the position contains (passed pawn, open file
next to a king, material imbalance, locked centre).

Stockfish is a judge here, never a teacher: it scores nothing that reaches the
training pool. The output is a list of themes where we choose badly, which is a
list of evaluation features worth building.

## 2. Recalibrate at 100 ms and 1 s

Only 10 ms has a five-rung curve. The 100 ms and 1 s numbers still come from the
2800 rung alone and are inflated the same way. 400 games per rung, SF@1800 to
2400 is the bracket to try at 100 ms given the 10 ms crossover.

## 3. Merge the overnight pool and retrain

`/private/tmp/chesslogs/gen_fast.bin`, play depth 3, label depth 6, about 132
positions a second. Merge into the deduplicated corpus and check the duplicate
rate first: generation 1 of the slow run was 92.3% new, which is the number to
beat.

## Closed this session

- deduplicated corpus: +15 +/- 11 over 3,600 games, adopted
- razoring: +14 +/- 12 over 3,000 games, adopted
- 128 hidden units: 1.3705 against 1.3618 held out, rejected
- all five pools with duplicates: -19 +/- 20, rejected
- self-play positions alone: -26 +/- 20, rejected
- champion files can no longer drift apart (test)
- generation is label-bound: depth 8 to 6 is 8.2x throughput

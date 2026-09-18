# Queue

The absolute scale changed on 2026-09-18: the engine is about 1,985 at 10 ms,
not the 2,493 the SF@2800 rung implied. See `analyses/elo-calibration/report.html`.
Every A/B in LEARNINGS.md still stands, they are head to head at a fixed clock.

## 1. Chase the pawn finding

moveaudit says the evaluation reaches for pieces where the oracle reaches for
pawns (pawn push 51 against 24, develops 11 against 28, over 500 positions).
Three ways to test whether that is the missing term, cheapest first:

- Label depth. The labels come from our own depth 6 to 8 search, which cannot
  see what a pawn move is worth. Generate one pool at label depth 10 and one at
  6 from the same games, train both, and rerun the audit on each: if the pawn
  gap narrows with deeper labels, the teacher is the ceiling.
- The hand evaluation still has a pawn `Structure` term that `hand_blend 0`
  switched off. Blending it back costs 18% of nodes per second, which is why it
  was dropped, but a pawn-only blend was never tried separately.
- Feed the network something it cannot currently see. HalfKP is king-piece
  pairs; passed pawns and pawn chains are not directly representable at 64
  hidden units, and width is dead, so this means new inputs rather than more of
  them.

## 2. Move agreement audit against Stockfish as a judge (built)

An engine that searches 12 plies at 1 s and rates 1,985 has evaluation defects,
not diminishing returns. Find them: take a few hundred positions from the pools,
ask for our best move and Stockfish's best move at a fixed depth, and bucket the
disagreements by phase and by what the position contains (passed pawn, open file
next to a king, material imbalance, locked centre).

Stockfish is a judge here, never a teacher: it scores nothing that reaches the
training pool. The output is a list of themes where we choose badly, which is a
list of evaluation features worth building.

## 3. Recalibrate at 100 ms and 1 s

Only 10 ms has a five-rung curve. The 100 ms and 1 s numbers still come from the
2800 rung alone and are inflated the same way. 400 games per rung, SF@1800 to
2400 is the bracket to try at 100 ms given the 10 ms crossover.

## 4. Merge the overnight pool and retrain

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

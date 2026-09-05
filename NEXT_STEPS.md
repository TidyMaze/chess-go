# Next steps to improve the engine

Updated 2026-09-05.

## Read this first: what a measurement here can and cannot detect

This governs everything below, because most of the session's wasted
effort came from testing changes with a tool too blunt to see them.

| Method | Cost | Resolution (95%) | Use it for |
|---|---|---|---|
| Stockfish calibration | ~140s | **+/- 130 Elo** | the absolute number, rarely |
| Head-to-head, 300 games | ~2 min | +/- 39 Elo | quick reject of a bad idea |
| Head-to-head, 1500 games | ~10 min | +/- 17 Elo | confirming a real change |
| Head-to-head, 5000 games | ~35 min | +/- 10 Elo | a 1% change (19 Elo) |
| Stockfish agreement probe | ~1 min | n/a | separating search from evaluation |

Repeated calibrations of nearly identical builds returned 1892, 1909,
1840 and 1786. That spread is the measurement, not the engine. **A 1%
improvement is 19 Elo and calibration cannot see it.** Use head-to-head
with enough games to decide whether a change works, and calibration only
to place the result on the public scale.

Current standing: roughly **1850 +/- 100** on the Stockfish scale, which
is Class A, a strong club player.

## The central finding: the evaluation is the ceiling

From the agreement probe (`agree`), which asks how often this engine
picks the same move as a depth-14 Stockfish, over the same 300 positions
in every arm:

| depth | full evaluation | material only |
|---|---|---|
| 1 | 45.0% | 34.9% |
| 2 | 48.7% | 37.6% |
| 3 | 54.7% | 37.2% |
| 4 | 60.4% | 45.6% |
| 5 | 59.7% | 43.6% |
| 6 | 61.4% | 41.6% |

Agreement stops improving after depth 4, and with a weaker evaluation it
falls instead of plateauing. A deeper search optimises harder for
whatever the evaluation says is good, so a weak evaluation gets worse
with depth. Confirmed independently in games: depth 5 over depth 4 is
+60 +/- 70, depth 6 over depth 5 is +38 +/- 69, at 3.8x the time.

**More search is not the lever. Evaluation quality is.**

## What has been tried, and what it measured

| Change | Result | Verdict |
|---|---|---|
| Futility pruning (Heinz 1998) | +55 +/- 63, 25% fewer nodes | kept as a speed win |
| Texel tuning on game outcomes | -16 +/- 48 (200 games) | rejected |
| Texel tuning on Stockfish scores | +20 +/- 26 (700 games) | kept, unconfirmed |
| NNUE, full replacement, sigmoid target | -700 +/- 112 | rejected |
| NNUE, residual on depth-12 search | -338 +/- 76 | rejected |
| NNUE, residual on static eval | -308 +/- 70 | rejected |
| Castling | +19 +/- 39 (300 games) | kept, it is a rule |
| Disabling LMR | +23 +/- 39 | worth retesting at power |
| Quiescence cap 4 -> 12 | +23 +/- 39 | worth retesting at power |
| Quiescence cap 4 -> 24 | +6 +/- 39 | no |
| Disabling null-move | -13 +/- 39 | no |

Nothing here is confirmed at 1%. Several land around +20, which is
exactly the size that 300 games cannot resolve.

## Do these next, in this order

### 1. Confirm the +20s at power, then stack them

`-no-lmr` and `-qply 12` each measured +23 +/- 39. If both are real, the
pair is worth ~45 Elo, which 1500 games can see. Run each alone at 1500
games, then together. This is the cheapest available Elo in the
repository right now and needs no new code.

### 2. Fix why the network failed, rather than abandoning it

The failures were informative and none of them was "neural evaluation
does not work here":

- The sigmoid target saturated, so the network read "a rook down" as
  -1.38 instead of -5. Fixed by regressing on the score in pawns.
- Fitting a *static* function to a *search* score asks it to predict
  tactics. Fixed by using Stockfish's static eval, which is 180x cheaper
  to label anyway (37,780 positions/sec against 210).
- 64 hidden units could not fit even the training set. 256 units cut
  training error from 3.13 to 1.29 pawns squared, which moved the
  constraint from capacity to data.

That leaves the actual blocker: **the training set is too small**. At 256
units, held-out error is 2.95 against a training error of 1.29, which is
overfitting. Labelling is nearly free now, so generate a few million
positions rather than 60,000. Held-out error needs to be well under 0.5
pawns before a network is worth putting in a game: at ~2 pawns it hangs
pieces, which is what -300 Elo looks like.

### 3. En passant

The only chess rule still missing. Worth little Elo directly, but it is a
rule, and its absence means FEN cannot describe some positions the
opponent can reach.

### 4. Tune the tables entry by entry

The tuner currently fits 6 piece-square scalars. With enough positions it
should fit all 768 entries. Do this after the data set is bigger, and
only against Stockfish scores, never against game outcomes.

### 5. King safety that counts attackers

Every fit so far has pushed the king-shelter term around without
conviction, because counting shelter pawns is too crude to be worth
weight. The standard form counts attacking pieces near the king and
weights by attacker value. This is also the term most likely to matter
now that castling exists and kings actually reach safety.

## Ideas that look attractive and are not

- **Lazy SMP / parallel search.** Ten cores are idle during search, so
  this looks like free strength. It is not: every measurement here is at
  fixed depth, where more nodes per second buys nothing, and the
  depth-to-Elo slope is only ~40 Elo per ply. Worth doing only after a
  time-controlled measurement setup exists and the evaluation ceiling is
  lifted.
- **Bitboards.** A large rewrite for speed, and speed is not the
  constraint: see the whole first section.
- **Deeper search.** Same reason.

## Loose ends, not on the critical path

- The legacy alpha-beta path agrees with Stockfish less at depth 2
  (26.7%) than at depth 1 (42.3%). That path is not used in matches.
- Aspiration windows once measured 20 points of agreement better than a
  full-window search, which they should not affect at all. On the current
  build the gap is gone (39.5% against 41.1%), so it was probably a
  property of the old position set, but it was never explained.
- The depth-4 iterative search converts K+R vs K only 22/25 of the time.
- `maxQuiescePly` is 4, which is low; raising it to 12 measured +23 +/- 39
  and is item 1 above.

## How to run things

```bash
go test ./...                                    # includes Stockfish rule validation
./gauntlet-bin -games 1500 -depth 4 -no-lmr      # A/B one change
./calibrate-bin2 -games 30 -depth 5              # absolute Elo, ~140s
./agree-bin -positions 300 -oracle-depth 14      # search vs evaluation
./play-bin -port 8765                            # UI and play against the engine
```

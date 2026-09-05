# Next steps to improve the engine

## Stop: there is a search defect to find first (2026-09-05)

Do not tune evaluation or add features until this is resolved. Evidence,
all from `./agree-bin` (agreement with Stockfish depth 14 on 300 self-play
positions, same positions in every arm):

| depth | iterative, all on | iterative, no aspiration | legacy alpha-beta |
|---|---|---|---|
| 1 | 45.0% | 31.5% | 42.3% |
| 2 | 48.7% | 36.6% | **26.7%** |
| 3 | 54.7% | 40.6% | 27.7% |
| 4 | 60.4% | 40.3% | 49.7% |
| 5 | 59.7% | 42.3% | 46.3% |
| 6 | 61.4% | 38.6% | n/a |

Three things are wrong here:

1. **Agreement is not monotonic in depth.** The legacy search agrees with
   Stockfish *less* at depth 2 (26.7%) than at depth 1 (42.3%). More
   search returning worse moves is a bug, not a tuning problem. The same
   dip appears at depth 5 in the iterative search.
2. **Aspiration windows change move quality by 20 points.** They are a
   speed optimisation and must not change which move is returned once the
   fail-high/fail-low re-search is correct. A 20-point swing means one of
   the two paths is wrong, and since the aspiration arm is the *better*
   one, the suspect is the full-window path.
3. **Extra depth buys almost nothing in games either**, which is the same
   story measured a different way: depth 5 over depth 4 is +60 +/- 70 Elo,
   depth 6 over depth 5 is +38 +/- 69 Elo, both inside their margins,
   while depth 6 costs 3.8x the time of depth 5.

Reproduce with:

```
./agree-bin -positions 300 -oracle-depth 14 -max-depth 6
./agree-bin -positions 300 -oracle-depth 14 -max-depth 6 -aspiration=false
./agree-bin -positions 300 -oracle-depth 14 -max-depth 5 -iterative=false
```

Places to look, in order: the root loop of `ChooseMoveIterative` never
narrows alpha across root moves; `terminalScore` returns mate scores that
depend on `depthLeft` and the transposition table stores them without
adjusting for ply; the even/odd oscillation in the legacy arm is the
classic signature of a leaf evaluation that is not symmetric with respect
to the side to move.

Fixing this is worth more than any item below: an engine whose 6th ply is
worth 38 Elo is leaving most of its search on the floor.

## Calibration reality check (2026-09-05)

150 games against Stockfish at five Elo-limited settings, per build:

| Build | Calibrated Elo | 95% CI |
|---|---|---|
| Go port baseline | 1879 | 1739-2019 |
| + ext/asp/SEE | 1855 | 1708-2001 |
| + pawn structure | 1909 | 1781-2037 |

The intervals overlap almost entirely. Head-to-head matches said +215 and
+78 for those two changes; neither is visible against an outside
opponent. Head-to-head only shows that a build beats its own ancestor.
Any future claim of an Elo gain needs an external confirmation before it
goes in the UI as a number.

Also noted: the depth-4 iterative search converts K+R vs K only 22/25 of
the time. That is a real endgame weakness, unrelated to futility pruning
(21/25 with it on).

Current state (round-robin, max-likelihood fit, random = 0 Elo, all measured on this machine):

| Engine | Elo |
|---|---|
| depth 5 FULL | +1712 |
| depth 4 FULL | +1506 |
| depth 3 FULL | +1302 |
| depth 2 +PST +quiescence | +1090 |
| **depth 2 (baseline)** | **+745** |
| depth 1 | +324 |
| random | 0 |

"FULL" = iterative deepening + PVS + killers + history + LMR + null move + TT + quiescence + tapered PST.

---

## 1. Evaluation tuning (biggest expected win, and replaces the genetic algorithm)

- [ ] **Texel tuning of the piece-square tables and piece values.** Generate a few hundred thousand
      positions from self-play, label each with the game's final result, then fit all evaluation
      parameters by logistic regression against those labels. This optimises ~400 numbers from a
      static dataset with no games in the loop. The genetic algorithm optimises 5 numbers and needs
      a noisy match per candidate. Expected: this is where classical engines got most of their eval
      strength.
- [ ] **Retire the genetic algorithm** once Texel tuning works, or keep it only as a baseline to
      measure against. It has produced no measurable Elo in this project.
- [ ] Add the eval terms that are cheap and known to be worth real Elo: passed pawns, doubled and
      isolated pawns, rook on open file, king-safety pawn shield, mobility.

## 2. Search

- [ ] **Aspiration windows** around the previous iteration's score in iterative deepening; re-search
      wider on fail-high/low. Cheap, standard, straightforward given ID is already in.
- [ ] **Static exchange evaluation (SEE)** to prune losing captures in quiescence, because quiescence
      currently searches every capture including obviously bad ones.
- [ ] **Futility / reverse futility pruning** near the leaves.
- [ ] **Check extensions**: extend the search by a ply when in check, so forced sequences are not
      cut off mid-way.
- [ ] Tune the LMR reduction formula (currently a flat 1 ply after the 3rd move); depth- and
      move-index-dependent reductions are standard.

## 3. Measurement (protect against fooling ourselves)

- [ ] **Report confidence intervals on every rating**, not just the point estimate. Several
      "improvements" in this project turned out to be inside the noise band.
- [ ] **Fixed opening book / varied start positions.** Every game currently starts from the initial
      position, so games between similar engines correlate heavily and the draw rate is inflated.
      Standard practice is a set of balanced opening positions, each played twice with colours
      reversed.
- [ ] **Time-controlled matches** rather than fixed depth. Fixed depth flatters slow techniques: a
      change that makes the search 2x slower for +20 Elo at equal depth is a loss in real play.
      This is the honest way to value TT / LMR / null-move.
- [ ] Add a regression suite of tactical positions (mate-in-N, known best moves) so search bugs are
      caught without a full match.

## 4. Performance (more nodes = more depth = more Elo)

- [ ] **Make/unmake moves instead of copying the board.** Every node currently copies a 144-byte
      board; an undo stack removes that entirely.
- [ ] **Incremental evaluation and Zobrist hashing**: update material/PST/hash on each move rather
      than recomputing from scratch per node.
- [ ] **Bitboards.** A significant rewrite, but it makes move generation and attack detection near
      free compared to the current square-by-square scanning.
- [ ] **Lazy SMP** (multiple search threads sharing a transposition table). The machine has 10
      cores; the search currently uses one per game, not per search.

## 5. Bigger swings (different algorithm class)

- [ ] **NNUE-style evaluation**: small neural net evaluating positions, trained on self-play labels,
      run with int8 SIMD on CPU. This is what replaced hand-tuned evaluation in strong engines.
- [ ] **MCTS + policy/value network** (AlphaZero-style). This is the only direction where the
      machine's GPU would actually help. Alpha-beta is sequential and branch-divergent, so a GPU
      makes it slower, but MCTS batches thousands of positions through a network, which is exactly
      what a GPU is for.

## Deliberately not doing

- **GPU-accelerated alpha-beta.** Structural mismatch: pruning is inherently sequential, branches
  diverge per thread, and per-node work is far too small to amortise kernel launches. Documented in
  the literature as losing to CPU implementations.

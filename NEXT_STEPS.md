# Next steps to improve the engine

## The evaluation is the ceiling, not the search (2026-09-05)

Measured with `./agree-bin`: sample positions from self-play, ask
Stockfish at depth 14 for the best move in each, and count how often this
engine picks the same move at each of its own depths. Same 300 positions
in every arm (`engine.SeedRandom`).

| depth | full eval | material only | no aspiration | legacy alpha-beta |
|---|---|---|---|---|
| 1 | 45.0% | 34.9% | 31.5% | 42.3% |
| 2 | 48.7% | 37.6% | 36.6% | 26.7% |
| 3 | 54.7% | 37.2% | 40.6% | 27.7% |
| 4 | 60.4% | 45.6% | 40.3% | 49.7% |
| 5 | 59.7% | 43.6% | 42.3% | 46.3% |
| 6 | 61.4% | 41.6% | 38.6% | n/a |

Read the first two columns together, because that comparison is the whole
finding:

- The full evaluation is worth about 15 points of agreement at every
  depth. The positional terms do work.
- Agreement stops improving after depth 4. Depth 6 is 61.4% against depth
  4's 60.4%, for 6x the time.
- With a material-only evaluation, agreement *falls* after depth 4 (45.6
  to 41.6). A deeper search optimises harder for whatever the evaluation
  says is good, so a weak evaluation gets actively worse with depth.

That is an evaluation ceiling, not a search defect. The engine already
searches deep enough to reach the limit of what its evaluation can tell
it apart, which is also why the games agreed: depth 5 over depth 4 was
+60 +/- 70 Elo and depth 6 over depth 5 +38 +/- 69, both inside their
margins, while depth 6 cost 3.8x the time.

So: **evaluation quality is the next lever, and more search is not.**
Texel tuning (below) moves from "nice to have" to the main event.

Reproduce:

```
./agree-bin -positions 300 -oracle-depth 14 -max-depth 6
./agree-bin -positions 300 -oracle-depth 14 -max-depth 6 -material=true -pst=false
```

### Two loose ends, neither on the critical path

- Turning aspiration windows off costs 20 points of agreement (60.4% to
  40.3% at depth 4). Aspiration is a speed optimisation and should return
  the same move as a full-window search once the fail-high/fail-low
  re-search is right, so a swing that large is not yet explained. The
  aspiration arm is the better one and is what matches use, so this is not
  urgent, but it is not understood either.
- The legacy alpha-beta path agrees less at depth 2 (26.7%) than at depth
  1 (42.3%). That path is not used in matches. Worth a look only if the
  aspiration question leads back to it.

## Texel tuning: fitted, measured, not adopted (2026-09-05)

Built `gendata` (self-play positions labelled with the game result,
appended per game so the expensive part is crash-resumable) and `tune`
(coordinate descent on the squared error between sigmoid(K*score) and the
result). 166,201 positions from 1,994 games.

The fit worked as a fit: squared error fell 1.78% on the training slice
and 2.10% on a shuffled held-out slice, so it found real signal rather
than memorising. (The first run had the held-out error starting *below*
the training error, because positions arrive in game order and an
unshuffled tail is the last few hundred games, not a sample. Fixed.)

It did not become Elo: **-16 +/- 48 over 200 games at depth 4**. Lower
prediction error is not the same objective as winning games, which is the
known caveat of the method, and this run is a clean example of it. The
parameters are kept behind `Player.Tuned` rather than adopted.

Two things worth reading in the fitted values, in `engine/tuned.go`:

- The rook drops from 5.00 to 4.70 while the open-file bonus climbs from
  0.20 to 0.76: value moves out of the piece and into where it stands.
- The king table is scaled to zero. In self-play games between engines
  this weak, king placement does not predict the result. That is a
  statement about the training set, not about chess, and points at the
  real gap: there is no king-safety term that counts attackers, only one
  that counts shelter pawns.

Next things to try on this, in order:
1. Label positions with a Stockfish evaluation instead of the self-play
   result. Self-play games between ~1900 engines are a weak teacher, and
   the king-table collapse is what that weakness looks like.
2. Tune the 768 individual table entries, not 6 scalars. 166k positions
   supports more parameters than this used.
3. Add a real king-safety term first, then tune it.

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

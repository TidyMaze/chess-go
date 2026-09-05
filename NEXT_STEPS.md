# Next steps to improve the engine

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
      static dataset with no games in the loop — the genetic algorithm optimises 5 numbers and needs
      a noisy match per candidate. Expected: this is where classical engines got most of their eval
      strength.
- [ ] **Retire the genetic algorithm** once Texel tuning works, or keep it only as a baseline to
      measure against. It has produced no measurable Elo in this project.
- [ ] Add the eval terms that are cheap and known to be worth real Elo: passed pawns, doubled and
      isolated pawns, rook on open file, king-safety pawn shield, mobility.

## 2. Search

- [ ] **Aspiration windows** around the previous iteration's score in iterative deepening; re-search
      wider on fail-high/low. Cheap, standard, straightforward given ID is already in.
- [ ] **Static exchange evaluation (SEE)** to prune losing captures in quiescence — quiescence
      currently searches every capture including obviously bad ones.
- [ ] **Futility / reverse futility pruning** near the leaves.
- [ ] **Check extensions** — extend the search by a ply when in check, so forced sequences are not
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
- [ ] **Incremental evaluation and Zobrist hashing** — update material/PST/hash on each move rather
      than recomputing from scratch per node.
- [ ] **Bitboards.** A significant rewrite, but it makes move generation and attack detection near
      free compared to the current square-by-square scanning.
- [ ] **Lazy SMP** (multiple search threads sharing a transposition table). The machine has 10
      cores; the search currently uses one per game, not per search.

## 5. Bigger swings (different algorithm class)

- [ ] **NNUE-style evaluation**: small neural net evaluating positions, trained on self-play labels,
      run with int8 SIMD on CPU. This is what replaced hand-tuned evaluation in strong engines.
- [ ] **MCTS + policy/value network** (AlphaZero-style). This is the only direction where the
      machine's GPU would actually help — alpha-beta is sequential and branch-divergent, so a GPU
      makes it slower, but MCTS batches thousands of positions through a network, which is exactly
      what a GPU is for.

## Deliberately not doing

- **GPU-accelerated alpha-beta.** Structural mismatch: pruning is inherently sequential, branches
  diverge per thread, and per-node work is far too small to amortise kernel launches. Documented in
  the literature as losing to CPU implementations.

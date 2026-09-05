# Review brief: chess engine and NNUE training pipeline

Written for an independent reviewer. Everything here is measured, not
assumed. Where a hypothesis was tested and falsified it is recorded, so
please do not re-propose it without new evidence.

Repository: `chess-go`, Go, no external dependencies.
Hardware: Apple Silicon, 10 cores. All training and measurement is local.
Reference engine: Stockfish 17, used as an oracle and a rating anchor.

---

## 1. What the engine is

Classical alpha-beta with a hand-written evaluation, plus an optional
learned evaluation currently in training.

**Board.** 12x12 padded byte array (0 off-board, 1 empty, 2+ = colour x 6
+ type + 2). Occupied squares held as 0-63 indices. Make/unmake against a
single board, no copying.

**Rules.** Complete except under-promotion. Validated by Perft against
the standard positions:

| position | depth | result |
|---|---|---|
| initial | 4 | 197,281 exact |
| kiwipete | 3 | 97,862 exact |
| position 3 (en passant) | 4 | 43,238 exact |
| position 4 | 2 | 228 vs 264, short by exactly the under-promotions |

Move generation also matches Stockfish exactly on 200 self-play positions
via `go perft 1`.

**Search.** Iterative deepening, principal variation search, killers,
history heuristic, late move reductions, null-move pruning, futility
pruning, check extensions, aspiration windows, quiescence with static
exchange evaluation, transposition table with Zobrist hashing (including
castling rights and en passant), repetition detection on the search path.
Effective branching factor 1.9 to 4.6 per ply, mean about 2.4.

**Evaluation (hand-written).** Material, tapered piece-square tables,
bishop pair, pawn structure, rook on open and semi-open files, king pawn
shelter, attacker-counting king danger, mobility, endgame king driving.

**Strength.** Roughly 1850 Elo on a Stockfish-calibrated scale, which is
a strong club player.

---

## 2. Measurement methodology

This is the part most worth reviewing, because most of the project's
wrong turns were measurement errors rather than engineering errors.

| method | cost | 95% resolution |
|---|---|---|
| Stockfish calibration | ~140 s | +/- 130 Elo |
| head-to-head, 300 games | ~2 min | +/- 39 Elo |
| head-to-head, 1500 games | ~10 min | +/- 17 Elo |
| SPRT (elo0=0, elo1=5) | variable | rejects a clear regression in ~410 games |

Six calibrations of near-identical builds returned 1892, 1909, 1840,
1823, 1866, 1786. That spread is the instrument, not the engine, so
calibration cannot resolve a 1% change and is used only for absolute
placement.

Harness validation: two identical engines score exactly 0.500 over 300
games; a known-large gap (depth 4 vs depth 2) reads +196 +/- 57 with
randomised openings and +198 +/- 57 without.

Matches start from six paired random plies, the same opening played twice
with colours reversed. Without this, two engines differing in one flag
played nearly identical games and 40% of results were draws.

**Nothing is adopted on a positive point estimate.** Adoption requires
the measured Elo to exceed its own margin.

---

## 3. The diagnosis driving current work

The evaluation limits strength, not the search. Three independent
measurements agree:

**Agreement with a depth-14 Stockfish**, same 300 positions in every arm:

| engine depth | full evaluation | material only |
|---|---|---|
| 1 | 45.0% | 34.9% |
| 4 | 60.4% | 45.6% |
| 6 | 61.4% | 41.6% |

Agreement stops improving past depth 4, and with a weaker evaluation it
*falls* with depth: a deeper search optimises harder for whatever the
evaluation asserts.

**Elo per ply** collapses: about 98 from depth 2 to 4, then +14 +/- 27
from depth 5 to 6 over 600 games, at 3.8x the time.

**Blunder analysis** (Stockfish scores every position before and after
each of the engine's moves): no blunder in the sample was a hung piece.
The search is tactically sound; the losses are positional.

---

## 4. The NNUE pipeline as it stands

**Features: HalfKP with 8 king buckets.** Feature = (king bucket, piece,
square). 8 x 10 x 64 = 5,120 inputs per side. Both perspectives share one
first layer and are concatenated; Black's squares are mirrored
vertically. Kings are excluded from the piece part since the king square
is the conditioning variable.

**Topology.** 5120 -> 16 (shared, applied to both perspectives) -> 32 ->
1, clipped ReLU [0,1], 81,969 parameters.

**Data generation.** Self-play at depth 2, 900 games per generation,
10 random opening plies. Positions kept only when quiet: not in check,
and the quiescence score within 0.35 pawns of the static score. About
57,000 positions per generation, 65 seconds on 10 cores.

**Target.** `0.8 * search_score + 0.2 * result_pawns`, in pawns, clamped
to +/-12, where the search score is a depth-3 search of the same engine
and `result_pawns` maps the game outcome to -4/0/+4.

**Training.** Adam (second moment only, no momentum) with per-touched-
column weight decay, Hogwild across 10 cores. 10 epochs per generation
over a sliding pool capped at 8M positions.

**Validation split is by game, never by position.** This matters
enormously, see section 5.

**Adoption.** Every 8th generation the candidate plays 400 games against
the champion and is adopted only if Elo exceeds its margin.

**Current state.** Held-out error 1.52 against the hand-written
evaluation's 7.07, so roughly 4.6x more accurate. Jump per move 0.414
against 0.376, so 1.10x jumpier, down from 3.7x when smoothness was first
measured. No network has yet won its match, so none has been adopted and
the engine's playing strength is unchanged.

Progress of the test match through the session, each measured over 400 to
800 games against the champion:

| stage | Elo |
|---|---|
| first network (units bug) | -798 |
| units fixed | -490 |
| king buckets and Adam | -187 |
| smoothing prior, more data | -102 |
| best blend with the hand evaluation | **-30 +/- 24** |

The blend curve is flat at its optimum: 70% network -40, 60% network -31,
50% network -30, all +/- 24 over 800 games, against -118 for the network
alone. So the last 30 Elo is not a weighting problem. No blend of this
network beats the hand-written evaluation, and finding a better one will
not change that; it needs a better network.

**What changed the picture.** Smoothness, not accuracy. Accuracy passed
the hand-written evaluation early and kept improving without moving the
Elo. Every point of Elo since has tracked the jumpiness falling, and the
blend optimum moved with it: when the network jumped 0.97 pawns per move
the best use of it was 10%, and at 0.41 the best use is 60%.

---

## 5. Falsified hypotheses, with evidence

Please do not re-propose these without new evidence.

**"The validation score shows it is working."** Held-out positions were
originally drawn at random from the same pool as training positions.
Consecutive positions in a game differ by one move, so every held-out
position had near-copies in training. The same network measured held-out
1.36 against the hand evaluation's 5.89 ("4x better") and lost 0-0-60.
Splitting by game turned that into 10.05 against 3.78, i.e. 2.7x worse.

**"Predictive accuracy is what an evaluation needs."** A network 1.6x
more accurate than the hand evaluation still lost at -490 Elo. Ranking
matters, not closeness: scoring every legal move with each evaluation and
comparing the pick against a depth-6 search gives network 5%, hand
evaluation 5%. The extra accuracy buys no better move ordering.

**"Fit the evaluation to a strong engine."** Four supervised fits against
three targets. All improved their objective; none improved play. The
sharpest case isolates one term: hand-picked mobility weights measured
+9 +/- 34, and weights fitted to Stockfish's static evaluation (14%
better fit) measured -73 +/- 35.

**"More search depth."** +14 +/- 27 Elo per ply past depth 5, see
section 3.

**"Speed converts to strength."** A 1.28x faster search is worth about 4
Elo at fixed depth. Speed is still worth having because it multiplies
data generation and match throughput, which are the real constraints, but
not because it makes the engine play better.

**"The network just needs more capacity or less overfitting."** Swept 4,
16, 64 and 256 hidden units on the previous feature set: worse than the
hand evaluation at every size, including sizes far too small to overfit.

---

## 6. Bugs found, as a guide to what to distrust

1. **Units.** The network was trained on a win probability and consumed
   as pawns. Probabilities are never negative, so every lost position
   evaluated as slightly good: "Black is a rook up" read +0.245. This
   alone accounts for every -300 to -800 Elo network result.
2. **Residual mismatch.** The network's output was added to the hand
   evaluation but trained on the full score, so the engine counted the
   evaluation twice. -211 +/- 41.
3. **Contaminated profile.** A profile attributing 38.6% of runtime to
   the transposition table was taken while a 1500-game match ran; idle it
   is 19.5%. The optimisation it motivated turned out speed-neutral.
4. **float32 in the transposition table** grew the search 18%, because
   principal variation search probes with 1e-6 windows and float32
   quantises a mate score more coarsely than that.
5. **Repetition** was detected only as a root tie-break, not inside the
   search, so a repeated position scored 9.67 instead of 0.
6. **En passant pin.** An en passant capture removes a pawn from a third
   square and can expose the king along a rank, which pin detection
   cannot see. Caught by Perft position 3.

---

## 7. Where review would help most

1. **Smoothness.** The network is accurate but jumpy: 0.58 pawns of
   change per move against the hand evaluation's 0.38, and a blend sweep
   gave a clean dose-response (+9 Elo at 10% network, -80 at 30%, -207 at
   50%). Is the diagnosis right, and is regularisation, more data, or an
   architectural change the fix?
2. **Is the target right?** Labels come from a depth-3 search of the same
   engine, which is an ~1850 player. Does bootstrapping from yourself
   have a ceiling below the teacher, and does the 0.2 weight on the game
   result meaningfully break that?
3. **The 5% ranking result.** Both evaluations agree with a depth-6
   search only 5% of the time on best move. Is the experiment sound, and
   is that figure plausible?
4. **Should this become AlphaZero-style?** MCTS with a policy head, GPU
   batching, ResNet. The judgement so far has been no: on this hardware
   it needs orders of magnitude more compute, and alpha-beta beats MCTS
   for chess without a strong policy network. GPU inference specifically
   looks wrong for alpha-beta, which is latency-bound rather than
   throughput-bound.

## 8. How to run things

```bash
go test ./...                                     # includes Perft and Stockfish validation
./nnue-bin -generations 800 -games 900            # the training loop
./gauntlet-bin -games 1500 -depth 4 <flags>       # A/B one change
./calibrate-bin2 -games 30 -depth 5               # absolute Elo, ~140s
./agree-bin -positions 300 -oracle-depth 14       # search vs evaluation
./analyze-bin -games 6                            # what the engine gets wrong
./play-bin -port 8765                             # UI, live training, play the engine
```

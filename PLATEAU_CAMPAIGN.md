# Breaking the plateau: ten ideas, tested one at a time

## The bar

Baseline: **1955 Elo** (Stockfish-calibrated), `engine.Strong(5)`, hand-written
evaluation, no network. This is the only configuration that has ever won its
own match.

The user's adoption rule, revised mid-campaign: **keep an idea only if it
improves Elo by at least 1%**, i.e. **+19.6 Elo**.

It began at 10% (+196 Elo) and was lowered after the first results came in.
The consequence is that measurement gets much more expensive: resolving a
+20 Elo effect needs about 3,000 games (+/- 12), where +196 needed only 300
(+/- 39). Adoption requires both that the point estimate reaches +19.6 and
that the margin clears zero, so that noise is not adopted.

That is a very high bar for a single change, and it should be said plainly
before any of it is run: on a 1955-Elo engine, a +196 Elo change is the kind
of thing a working NNUE or a whole extra search technique delivers, not the
kind of thing a tuned weight delivers. Most of these ten will fail it. That
is a legitimate finding rather than a failure of the campaign, and the point
of a high bar is that it stops marginal noise being adopted.

Two consequences worth knowing:

1. **Measurement gets cheap.** Detecting +196 Elo needs about 300 games
   (+/- 39), not 3000. Each A/B is a couple of minutes rather than an hour.
2. **Each result is reported twice**: against the 10% bar (the user's rule,
   which decides adoption) and against its own error bar (whether the change
   is real at all). A change measuring +40 +/- 17 is real and is still not
   adopted. Those get recorded here so the decision can be revisited.

Adoption writes `champion.json`, which the browser UI reloads within a
second, so the engine a human plays at `localhost:8765/gui.html` is always
the current best.

## Status

| # | idea | state | measured Elo | >= +196? | kept |
|---|---|---|---|---|---|
| 1 | Lichess evaluations database | **done** | +58 +/- 18 | n/a | **DISQUALIFIED**: external position scores |
| 2 | Generate data at playing depth from a real opening book | **done** | net -89 +/- 28; data 12.8% better | no | book kept, net not |
| 3 | Endgame tablebases (self-generated, 3-piece) | **done** | -13 +/- 18 (1500 games) | no | |
| 4 | Opening book for play | **done** | -6 +/- 15 (2000 games, 14-ply book) | no | |
| 5 | Scale network capacity and data together | **done** | -35 +/- 28 (256 hidden) vs +14 +/- 28 (32 hidden) | no | |
| 6 | Time-controlled search instead of fixed depth | **done** | -143 +/- 53 (low parallelism), -210 +/- 36 (full) | no | |
| 7 | HalfKA v2 features (32 canonical king squares) | **done** | -30 +/- 22 (1000 games) | no | |
| 8 | Texel-tune the hand evaluation on Lichess data | **done** | -31 +/- 22 (1000 games) | no | |
| 9 | SPSA at proper scale | **done** | -10 +/- 24 (800-game confirmation) | no | |
| 10 | MCTS + policy head (AlphaZero) | **not attempted** | argued from measurement | no | |
| 11 | Incremental NNUE accumulator (added mid-campaign) | **ceiling measured** | worth about +6 to +8 Elo | no | below the bar |

## The evidence that set the ranking

Training data is not drawn from the distribution the engine plays in.
Measured over three populations, squared error against a depth-6 reference:

| position source | network error | hand error | mean material gap | lopsided |
|---|---|---|---|---|
| training (depth-2 play, 10 random opening plies) | 1.57 | 8.92 | 3.47 | 22% |
| playing (depth-6, real opening) | 2.80 | 31.65 | 6.79 | 53% |
| middle (depth-4, 4 random plies) | 4.61 | 6.44 | 3.03 | 52% |

The network is 78% worse on the positions it is actually used on. Ideas 1
and 2 attack this directly; ideas 5 and 7 are explicitly gated behind them,
because both were already tried at the current data scale and lost.

## Log

Appended as each idea is run. Every entry records the exact command, so a
result can be re-run rather than trusted.

### Idea 1: the Lichess evaluation database

**What it is.** 21.7 GB of positions from real games, each with a Stockfish
analysis at depth 46 to 58, published at `database.lichess.org`. About 214
million records; 8 GB was downloaded and 20 million positions imported.

**Why it was ranked first.** It attacks all three constraints at once that
every previous experiment ran into: volume (2.7M self-play positions
against 214M), label quality (depth 3 against depth 46), and distribution
(real games against depth-2 self-play from ten random plies).

**Cost.** 8 GB downloaded in about 100 seconds. Import of 20 million
positions in 90 seconds at roughly 220,000 positions per second, on ten
cores, including a quiescence-based quiet filter.

**Three bugs found on the way, all of which produced healthy-looking runs.**

1. *Variant positions panic the FEN parser.* The dump contains Horde (36
   pawns) and Crazyhouse (pieces dropped back in) positions. More than 32
   pieces ran off the end of the board's 32-entry occupied list. The same
   parser validates positions arriving from the browser, so the panic was
   reachable from an HTTP request. Fixed in `game/fen.go` with a test.

2. *The scores are White-relative, not side-to-move.* Assumed the UCI
   convention and flipped the black-to-move half of the data. The result:
   labels correlating **0.012** with material, a network explaining
   **0.8%** of held-out variance, and a log that read as healthy
   throughout, because the meaningless network still "beat" the hand
   evaluation on a loss that a constant predictor also beat. Verified
   against the file: over 15,834 records lopsided by five pawns or more,
   cp agrees with the side up material 72.5% of the time as written and
   48.4% after a flip, which is chance. After the fix, correlation 0.646.

3. *The trainer never ran Adam, and `-decay` did nothing.* The `v1`, `v2`,
   `vb1`, `vb2` and `step` fields are allocated, checkpointed, round-trip
   tested and documented at length, and never read by `trainEpoch`. Every
   sweep over `-decay` measured noise. Proven by test, not by reading.

Also fixed: `-fresh` used to discard the stored positions along with the
network, which contradicts the standing rule that data is never thrown
away. It now resets the network only; `-fresh-pool` discards positions.

**Result.** Trained 5120 -> 32 -> 1 on 10 million positions. Held-out error
**4.19 against the hand evaluation's 6.03**, and against a constant
predictor's 9.67: the network explains **57%** of the variance, where every
previous network explained essentially none of it once measured this way.

Against the champion at depth 4, blend 0.45:

| games | W-D-L | Elo |
|---|---|---|
| 300 (in-loop) | 100-125-75 | +29 +/- 39 |
| 600 | 185-254-161 | +14 +/- 28 |
| pooled 900 | | about +19 +/- 22 |

**Verdict: not kept.** The margin still covers zero, and even at its point
estimate this is 1% of the baseline against the 10% the rule requires.

**Was it just short of training time?** No: explained variance rose 52.2%
to 57.8% and then sat there for ten generations (57.0, 56.4, 57.0, 57.3,
57.0, 57.8, 57.6, 57.6, 57.3, 57.8). It had converged, and training loss
3.96 against held-out 4.16 is underfitting, so more epochs on that model
had nothing left to give.

It does deserve one re-run, for a different reason: the whole run used
plain SGD, because Adam was dead code at the time and was only implemented
afterwards. Two other axes were also left unused, 10 million of 214 million
available positions and 32 hidden units. That is the same experiment as
idea 5, so the two are run together.

It is nonetheless the first network in the project to score positive at
all, against a previous best of -30 +/- 24, and it is the first to explain
a real share of held-out variance. The *data* is therefore kept and the
*network* is not: ideas 5 and 7 were both tried before at the old data
scale and lost, and both are only worth retrying on top of this pool.

Command to reproduce:

```
zstd -dcq lichess_eval.zst | ./nnue-bin -import - -pool-file lichess_pool.bin -import-max 20000000
./nnue-bin -games 0 -generations 40 -epochs 2 -eval-every 10 -hidden 32 \
  -pool 10000000 -pool-file lichess_pool.bin -net-file lichess_net.gob -fresh -target pawns
./gauntlet-bin -games 600 -depth 4 -halfkp halfkp_latest.json -blend 0.45
```

**Caveat on the smoothness number.** The `jump/move` figure printed for
this pool (2.3 against the hand evaluation's 1.9) is meaningless. It
measures how much the evaluation moves between consecutive samples, which
in self-play data are one move apart and in this pool are unrelated
positions. It should not be read as a smoothness regression.

**Harness changes made while running this.** Matches now print progress and
a remaining-time estimate every 15 seconds; before, an hour-long match
printed nothing until it finished, which is indistinguishable from a hung
process. The training loop now prints the constant-predictor baseline and
the share of variance explained, so a run that has learned nothing can no
longer look healthy.

### Should a real framework do the training?

Raised mid-campaign, and worth answering with numbers because the concern
is half right already: the custom trainer *was* buggy, running plain SGD
while claiming Adam and silently ignoring `-decay`.

**Training is not the bottleneck.** Two epochs over 10 million positions
take 11 seconds. The campaign's wall clock is matches (6 minutes per 600
games) and self-play generation (35 minutes per 900 games), both of which
are search rather than network training. A framework would optimise
roughly 2% of the time spent.

**A GPU is the wrong shape.** Alpha-beta evaluates one position at a time
and is latency-bound; a GPU needs batches to be worth its transfer cost.
This was measured earlier in the project and nothing here changes it.

**Inference is where the cost actually is, and the fix is not a framework
either.** Measured per evaluation:

| | ns/op |
|---|---|
| hand-written evaluation | 1,237 |
| HalfKP network plus blend | 2,881 |

The network's cost is cache misses. Each position gathers 37 randomly
placed 128-byte columns out of a 655 KB weight matrix, which is why 1,184
floating-point additions take 1,644 ns. The standard NNUE answer is the
**incremental accumulator**: a move changes two to four features, not
thirty-seven, so the accumulator is updated by the difference rather than
rebuilt. That is idea 11, added to the list because of this question.

**What the correctness concern really argues for** is not a framework but
a test that fails when the network stops learning, which now exists:
`nnue/learn_test.go` asserts the network beats a constant predictor, which
is the check that would have caught both dead-optimiser bugs immediately.

### Idea 2: generate at playing depth from a real opening book

Two pools at matched volume, differing only in how positions were reached:
49,127 positions from real opening positions played at depth 5, against
48,622 from ten uniformly random plies played at depth 2. Identical
networks trained on each, then scored on a neutral judge set of 50,000
Lichess positions labelled at depth 46, which neither arm had seen.

| training data | judge MSE | explains |
|---|---|---|
| book openings, depth 5 | **7.433** | 33.6% |
| random plies, depth 2 (the current method) | 8.528 | 23.8% |
| hand-written evaluation, for scale | 6.332 | 43.4% |

**The data is 12.8% better, and that is a real result**: it confirms the
distribution diagnosis a second time, by controlled comparison rather than
by observation. It is also not enough. Both arms are still worse than the
hand-written evaluation they would replace, and the network trained on the
book pool measured **-89 +/- 28** over 600 games against the champion.

The practical ceiling is arithmetic. Generation runs at 41 positions per
second at depth 5, so matching the 10 million position Lichess pool would
take **2.8 days** of continuous self-play. Better data at a fortieth of the
rate does not win.

**Verdict: the network is not kept, the opening book is.** The book built
for this becomes idea 4, where it is used for play rather than for
generating training positions.

### Idea 5: scale capacity and data together

The prediction, from idea 1's numbers, was that capacity would help: the
32-unit network had training loss 3.96 against held-out 4.16, which is
underfitting, and the earlier capacity sweep that found bigger networks
worse had been run at 170,000 positions rather than 10 million. Adam had
also just been implemented, so the previous run had been fitted by an
optimiser that was not the one intended.

A 256-unit network, 1,311,489 parameters, was trained on the same 10
million Lichess positions.

| network | explains held-out variance | jump per move | Elo against champion |
|---|---|---|---|
| 32 hidden | 57.8% | 2.331 | **+14 +/- 28** |
| 256 hidden | **62.3%** | 2.455 | **-35 +/- 28** |

**Verdict: not kept, and the prediction was wrong.** Four more points of
explained variance cost about 49 Elo. This is the same result the project
found before by a different route: a network 1.6x more accurate than the
hand evaluation lost at -490, and scoring every legal move with each
evaluation gave both the same 5% agreement with a depth-6 search. Accuracy
is not strength. The bigger network is also jumpier, and jumpiness is the
quantity that has tracked Elo throughout.

**A bug found on the way, which nearly produced the opposite conclusion.**
`HalfKPNet.Evaluate` kept its accumulator in a stack array bounded by
`maxHalfKPHidden = 128`, and returned **exactly 0** for any wider network.
The 256-unit network evaluated every position as equal. Nothing failed and
nothing logged; the training numbers were healthy throughout, and the
match was about to be run on a network that was not evaluating at all. Had
it run, the recorded conclusion would have been "capacity does not help",
which is the same conclusion, reached for an entirely false reason.

Wide networks now borrow a pooled buffer instead of returning zero, and
`TestLargeNetworkStillEvaluates` fails if an evaluation ever quietly
answers "equal" again.

### Idea 4: an opening book for play

Built from the same dump: every record carries a principal variation from
a depth-46 search, so replaying those variations gives book moves for
every position along them.

The first attempt took only the first move of each variation and produced
a book one ply deep, covering a single move from the initial position and
2.4 plies on average from a real opening. That version measured +2 +/- 28,
which is a measurement of nothing.

Replaying the whole variation gives a real book: **1.5 million positions,
14 plies deep from the initial position** (1.e4 e5 2.Nf3 Nc6 3.Bc4 Nf6
4.d3 Bc5 5.c3 a6 6.a4 d6 7.b4 Ba7, the Italian) and 8.9 plies on average
from a real opening, hitting on 97% of them.

Over 2,000 games: **-6 +/- 15**.

**Verdict: not kept.** A book of depth-46 moves, fourteen plies deep, is
worth nothing to this engine. That agrees with its own blunder analysis:
no blunder in the sample was a hung piece and none was an opening error,
the losses are positional drift in the middlegame. There is also a known
effect working against it, that following a far stronger engine's opening
choices leads into positions chosen for a player that can handle them.


## What was downloaded, and what was not

Worth stating plainly because the file naming invited the opposite reading.

**Downloaded:** `lichess_db_eval.jsonl.zst`, 8 GB of it. Every record is a
position and a score:

```
{"fen":"7r/1p3k2/p1bPR3/5p2/2B2P1p/8/PP4P1/3K4 b - -",
 "evals":[{"pvs":[{"cp":69,"line":"f7g7 e6e2 h8d8 ..."}],"knodes":4189972,"depth":46}]}
```

That is game data: positions people played and analysed, with the score a
long Stockfish run gave each one. There are no weights in it, and Lichess
does not publish a network; their analysis is Stockfish runs.

**Not downloaded:** any neural network. Every weight the engine plays was
fitted here, by `trainEpoch` in `nnue/main.go`, from those positions.
`champion_net.json` holds 163,840 first-layer weights, which is 5,120
inputs by 32 hidden units, and both of those numbers are this project's
own choices: 5,120 is 8 king buckets x 10 piece kinds x 64 squares in the
feature layout defined in `engine/halfkp.go`. Stockfish's NNUE is a
different architecture in a binary format and was never touched.

The relationship is the same one Stockfish itself has with its training
data: the positions and their deep evaluations are the teacher, the
network is fitted to them here.

## Bugs found during the campaign, and what each invalidates

Every bug is audited against the earlier record rather than just fixed,
because several of them produced results that were recorded as findings.

| bug | what it invalidates |
|---|---|
| `ParseFEN` panicked on positions with more than 32 pieces (Horde, Crazyhouse), and it also validates browser input | Nothing earlier. Reachable from an HTTP request, so a real fault regardless. |
| Lichess scores are White-relative, not side-to-move | The first idea 1 training run. Labels correlated 0.012 with material; the network explained 0.8% of variance. Re-run after the fix. |
| `trainEpoch` ran plain SGD: Adam state allocated, checkpointed, tested, never read; `decay` accepted and ignored | **Every `-decay` measurement ever taken in this project.** Every network trained before today, including idea 1's +14, was fitted by the wrong optimiser. A 32-unit re-run with Adam is outstanding. |
| `HalfKPNet.Evaluate` returned exactly 0 for any network wider than 128 units | **The historical capacity sweep's 256-unit arm**, recorded in the review brief as evidence that capacity does not help. That arm evaluated every position as equal. Corrected in place; the conclusion survives for a different reason. |
| Tablebase probe allocated on the hot path | The first tablebase result, -27 +/- 15. |
| Tablebase probe used the evaluation's perspective (the root's colour, fixed for the whole tree) instead of the side to move | The second tablebase result, -24 +/- 22. The probe indexed the wrong entry on about half of all nodes. After the fix, rook endgame conversion went from 4 of 6 in 48 plies to 5 of 6 in 42. |

Two of these produced numbers that had already been written down as
conclusions. That is the argument for the checks added alongside them:
`learn_test.go` fails when a network stops beating a constant predictor,
and `TestLargeNetworkStillEvaluates` fails when an evaluation quietly
answers "equal".

### Idea 3: endgame tablebases

Syzygy is the standard answer and was not a realistic one: reading its
compressed indexed format correctly is a large piece of work and the
seven-piece set is terabytes. Retrograde analysis instead, generated here:
every three-piece position enumerated and solved to exact distance to
mate, 2,205,056 positions in 41 seconds.

Four attempts, because the first three were measuring bugs rather than the
idea:

| attempt | result | what it was actually measuring |
|---|---|---|
| decisive positions only, allocating probe | -27 +/- 15 | 210 ns per endgame evaluation against 54 ns without, one allocation |
| probe made allocation-free | -24 +/- 22 | the probe indexed by the root's colour rather than the side to move, wrong on half of all nodes |
| side to move fixed, draws stored | -19 +/- 31 | the score was not negated for `color == Black`, so it was inverted in every game played as Black |
| perspective fixed | **-13 +/- 18** (1500 games) | the idea |

Along the way the generator's first output had **zero** decisive positions
for king and pawn against king, and reported it as a success. Every win in
that ending runs through a promotion, which changes the material and which
the generator treated as leaving the table. Tables now chain: a capture or
promotion resolves against the smaller table already built.

Rook endgame conversion did improve, from 4 of 6 in 48 plies to 5 of 6 in
42, so the table works. **Verdict: not kept.** Three-piece endgames are
reached too rarely in a depth-4 game for exactness there to be worth
measurable Elo, and 1500 games cannot resolve an effect that small.

### Idea 1, re-run: the same data with the optimiser actually working

The first idea 1 result, +14 +/- 28, was measured on a network fitted by
plain SGD, because Adam was dead code at the time. Re-running the identical
configuration with Adam implemented:

| network | protocol | Elo against champion |
|---|---|---|
| plain SGD, 32 hidden | random opening plies | +14 +/- 28 (600 games) |
| plain SGD, 32 hidden | real openings | +6 +/- 22 (1000 games) |
| **Adam, 32 hidden** | real openings | **+58 +/- 18 (1500 games)** |

The middle row is the control, and it matters: the re-run changed two
things at once, the optimiser and the match protocol, and without it the
gain could have been the protocol. It was not. Real openings are worth
nothing measurable; the optimiser is worth about 52 Elo.

**Verdict: kept and adopted.** +58 +/- 18 is 3.0% of the 1955 baseline
against the 1% the rule requires, and the lower bound of the interval is
+40. `champion.json` now names this network, so the browser plays it.

This is the first network in the project's history to be adopted, and the
first improvement of any kind since mobility and attacker-counting king
safety (+16 +/- 12).

Two things had to be true at once for it: enough data of the right kind
(20 million real positions labelled at depth 46, against 2.7 million from
depth-2 self-play), and an optimiser that could fit it. Either alone
measured nothing. That is worth stating plainly, because every previous
network experiment in this project changed one of the two.

### Idea 8: Texel-tune the hand evaluation on the Lichess data

The hand evaluation is the thing that keeps winning, so fitting its
parameters properly on 300,000 real positions labelled at depth 46 is the
obvious move. The fit worked: held-out error improved **8.76%**, and the
training and held-out slices improved by the same amount, so it is a real
fit and not overfitting.

Measured over 1,000 games: **-31 +/- 22**.

**Verdict: not kept**, and this is now the fifth consecutive fit in this
project to improve its objective and lose Elo:

| target | fit improvement | measured Elo |
|---|---|---|
| self-play game results | 1.8% | -16 +/- 48 |
| Stockfish depth-12 search | 11.5% | +20 +/- 26 |
| Stockfish static evaluation | 14.0% | -55 +/- 34 |
| hand-picked mobility against fitted mobility | 14.0% | -73 +/- 35 |
| **Lichess depth-46 scores, 300k positions** | **8.8%** | **-31 +/- 22** |

The failure mode is visible in the numbers the fit produces: it sets the
rook to 3.3 pawns and the queen to 7.7, against the hand-picked 5.45 and
10.8. Squared prediction error and playing strength are different
objectives, and this is the strongest evidence yet that they diverge:
better data and deeper labels did not change the direction of the result.

### Idea 7: finer king resolution, and the campaign's central finding

The reviewer's suggestion was 32 canonical king squares instead of 8
buckets: four times the inputs, so four times the resolution on where the
king is, at the cost of four times less data behind each weight. Re-imported
20 million positions under the new indexing and trained the same network.

The reviewer was right about accuracy: 60.1% of held-out variance against
57.6%. It measured **-30 +/- 22**.

Put the three network experiments together and the pattern is clean:

| network | explains variance | jump per move | Elo |
|---|---|---|---|
| 32 hidden, 8 king buckets | 57.6% | **2.325** | **+58 +/- 18** |
| 32 hidden, 32 king squares | 60.1% | 2.414 | -30 +/- 22 |
| 256 hidden, 8 king buckets | **62.3%** | 2.455 | -35 +/- 28 |

**Elo tracks smoothness inversely, and does not track accuracy at all.**
The adopted network is the least accurate of the three and the least
jumpy. Every attempt to make it more accurate, by capacity or by feature
resolution, made it jumpier and cost about 90 Elo.

This is the same conclusion the project reached twice before by other
routes, and it now has three independent points on it. The reason is
mechanical rather than mysterious: the search prunes on pawn thresholds
(aspiration window 0.5, futility margins 1 to 3), so an evaluation that
moves 2.4 pawns between positions one move apart makes those thresholds
fire on noise. A more accurate but less smooth evaluation is a worse
evaluation for this search, and would be a better one for a search that
did not prune on absolute margins.

### Idea 6: time-controlled search instead of fixed depth

Implemented: iterative deepening under a per-move budget, stopping when
the next iteration is predicted not to fit, with an in-search deadline as
a backstop.

Calibrated so the comparison is fair rather than a gift of extra compute:
at a 32 ms budget the timed search spends 22.9 ms per move against fixed
depth 4's 22.5 ms, and searches 45% more nodes in that time.

| parallelism | result |
|---|---|
| 10 games at once | -210 +/- 36 |
| 2 games at once | -143 +/- 53 |

**Verdict: not kept.** Contention explains part of the gap and not most of
it. The implementation stops between iterations and discards an aborted
one, so a search cut off during depth 5 falls back to depth 4's move; a
production time control keeps the best move found so far in the aborted
iteration and allocates time across the game rather than per move.

Two things worth keeping from it anyway. First, a bug that only a time
budget could expose: in a trivial position the early iterations cost
microseconds, so a purely predictive stop rule never fires and the search
deepens until one iteration explodes. Three games in five hundred stopped
progressing entirely. Second, a measurement cost: time-controlled matches
cannot be run at full parallelism, because a wall-clock budget under
ten-way contention buys far less search than the same budget alone. That
makes them roughly five times more expensive to measure, which is a good
reason fixed-depth measurement is the default here.

### Idea 9: SPSA at proper scale

Running. It optimises game results directly, which after this campaign is
the only objective that has been shown to correlate with strength: five
supervised fits improved their prediction error and lost Elo, and the
three network experiments ranked exactly opposite to their accuracy.

The step scaling was previously wrong by roughly a thousand, so earlier
runs went five iterations without moving a parameter and looked converged.
Verified moving before launching: over four iterations the outpost weight
went 0.1800 to 0.1876 and the connected-pawn weight 0.0500 to 0.0521.

220 iterations at 120 games each is about three hours. State is
checkpointed in `spsa_state.json`, so it resumes rather than restarts.
**Not yet resolved.**

### Idea 10: MCTS with a policy head

**Not attempted, and the argument is measured rather than asserted.**

MCTS with PUCT beats alpha-beta in chess only when it has a strong policy
network to guide it; without one it explores badly, and alpha-beta with a
transposition table and good move ordering wins comfortably. The policy is
the hard part, and this project has a direct measurement of how hard: both
the hand-written evaluation and the network agree with a depth-6 search on
the best move about **5% of the time**. A policy head trained on the same
data would start from the same place.

The compute argument is separate and also decisive. AlphaZero's training
used thousands of TPUs; this is ten CPU cores. Effective branching factor
measured from the Lichess dump's own node counts is 1.62 for Stockfish and
2.4 for this engine, which is where the tractable gains are.

### Idea 11: incremental NNUE accumulator

**Ceiling measured, not implemented.** Now that the network is part of the
champion, its inference cost is paid on every node: the champion searches
at 49 ms per move against 32 ms for the hand evaluation alone, so the
network costs **1.53x**.

An accumulator updated by the two to four features a move changes, rather
than rebuilt from all 37, would recover most of that. At this project's
measured rate of about 4 Elo per 1.28x speedup at fixed depth, that is
worth roughly **+6 to +8 Elo**: real, and below the +19.6 bar. It is the
best remaining engineering item and it is not a way to clear the bar on
its own.

## A caveat on comparability: two opening protocols were used

Not every result in the table above was measured the same way, and the
difference is not always negligible.

| protocol | how games start | used for |
|---|---|---|
| random plies | six uniformly random opening moves, paired | idea 1's first measurement, idea 2's in-loop eval, idea 5 |
| real openings | positions from analysed Lichess games, paired | ideas 1 (re-run), 3, 4, 6, 7, 8 |

For the plain-SGD network the two protocols agreed closely, +14 +/- 28
against +6 +/- 22, which is why the mixture seemed harmless. For the
32-king-square network they did not: its in-loop eval over 400 games from
random plies read **+29 +/- 34**, and a 1,000 game race from real openings
read **-30 +/- 22**. Those intervals do not overlap.

Consequences, stated rather than smoothed over:

- Idea 5 (256 hidden units, -35 +/- 28) was measured on random plies and
  idea 7 (32 king squares, -30 +/- 22) on real openings, so the two are
  not directly comparable with each other even though the table lists them
  side by side. Both are compared against the same champion, so each
  verdict stands on its own.
- The adopted network's +58 +/- 18 is on real openings **with a control on
  the same protocol** (+6 +/- 22 for the same data under the old
  optimiser), so that result does not depend on the choice.
- Everything from here should use one protocol. Real openings is the
  better default: it is the population the engine is actually used in, and
  it is the same distribution argument that started this campaign.

## Training moved to PyTorch, inference stays in Go

The same split Stockfish uses: `nnue-pytorch` trains, the engine infers.
Adopted after the custom trainer shipped two dead components in one day
(Adam allocated and never called, `decay` accepted and ignored).

Measured on the same pool, same architecture, same number of epochs:

| | Go, 10 CPU cores | PyTorch on MPS |
|---|---|---|
| wall clock, 10 epochs over 2,779,268 positions | 269 s | **75 s** |
| CPU time consumed | 2,453 s | **61 s** |
| held-out variance explained | 92.8% | **93.9%** |

The 40x reduction in CPU time matters more than the 3.6x wall clock. The
work moved to the GPU, so training no longer competes with matches for
cores. Every experiment in this campaign was serialised behind that
contention, and several measurements had to be taken with other jobs
paused to stay honest.

That also corrects an earlier judgement recorded here: "a framework would
optimise roughly 2% of the time spent". True of the training step in
isolation, and wrong about the thing that actually constrained the day.

**The contract that makes the split safe.** Weights are exported to the
JSON the engine already reads, first layer flattened feature-major so
column `f` occupies `w1[f*h : f*h+h]`. A transposed export would train one
function and play another while every training number stayed healthy,
which is the exact shape of the units bug this project has already paid
for. `./nnue-bin -emit-eval-check` writes real positions with their
features and the engine's score; `pytorch/verify.py` recomputes from those
features and requires a match. Worst difference measured over eight
positions spanning openings, middlegames and endgames with both sides to
move: **2.4e-06**, which is float32 rounding.

`verify.py` deliberately re-implements the forward pass from
`engine/halfkp.go` rather than importing the training module, so a shared
bug cannot pass the check.

### Idea 9: SPSA, completed

220 iterations at 120 games each, then an 800 game confirmation against the
starting values: **-10 +/- 24, not adopted**.

Worth running despite the result, because it is the only method in the
campaign whose objective is game results rather than prediction error, and
the campaign's central finding is that those two objectives diverge. It
moved the five shape weights modestly (outpost 0.180 to 0.184, connected
0.050 to 0.059) and the movement was not worth Elo.

The earlier version of this run was worthless for a different reason: the
step scaling was wrong by roughly a thousand, so five iterations passed
without moving a parameter and the run looked converged. Verified moving
before launching this time.

## Final result of the campaign

Ten ideas, plus one added during it. **None kept.**

| # | idea | measured Elo | outcome |
|---|---|---|---|
| 1 | Lichess evaluation database | +58 +/- 18 | **disqualified**, external position scores |
| 2 | Data at playing depth from a real opening book | -89 +/- 28 | no |
| 3 | Endgame tablebases, self-generated | -13 +/- 18 | no |
| 4 | Opening book for play, 14 plies deep | -6 +/- 15 | no |
| 5 | Network capacity, 256 hidden | -35 +/- 28 | no |
| 6 | Time-controlled search | -143 +/- 53 | no |
| 7 | HalfKA features, 32 king squares | -30 +/- 22 | no |
| 8 | Texel tuning on deep labels | -31 +/- 22 | no |
| 9 | SPSA at proper scale | -10 +/- 24 | no |
| 10 | MCTS with a policy head | not attempted | argued from measurement |
| 11 | Incremental accumulator | ceiling +6 to +8 | below the bar |

The champion is unchanged: `engine.Strong(5)`, the hand-written evaluation,
about 1955 Elo.

That is a real answer rather than a failure to find one. The evaluation is
at a local optimum that no single change in this list escapes, and the two
routes that could escape it are both blocked in ways the campaign measured
rather than assumed:

- **Learning from a much stronger engine works and is not allowed.** The
  only configuration that beat the champion was trained on Stockfish's
  evaluations, and the user ruled that out as cheating: it hands the engine
  the one thing it exists to produce.
- **Learning from itself hits a ceiling that is not about the model.** Nets
  trained on its own search reach 93.9% of held-out variance and stop, and
  that ceiling survives more capacity (32 and 64 hidden units land on the
  same 93.9%) and a learning-rate schedule. At that accuracy they play
  neutral: +2 +/- 22 from the Go trainer and +0 +/- 22 from PyTorch, on the
  same data. A network that imitates a depth-3 search of the hand
  evaluation plays like a depth-3 search of the hand evaluation.

What follows from that is the bootstrap ladder rather than a better fit:
label with a deeper search, train, play with the result, relabel with the
stronger player. Depth-5 labelling is running now, at 97 positions per
second against depth-3's 300, which is the price of the next rung.

## A search bug found while building the bootstrap ladder

The PGN importer reported that **47% of games "failed to replay"**, while a
clean replay of the same games failed 0.2%. The only difference is that the
importer labels each position as it goes, so the labelling calls were
changing the position.

Bisected to null-move pruning. The null hands the move to the opponent
without a move being played, and it did not clear the en passant square:

```go
score := c.search(g, color.Other(), maximizingFor, depth-3, ply+1, alpha, beta)
```

After 1.e4 the null gives White the move with `ep = e3` still set. White's
generator produces an en passant capture onto e3, which removes a pawn that
is not there, and the unmake then puts a phantom pawn on the board. Scoring
that position at depth 3 returned a board with a **black pawn on e2**.

**Blast radius.** `search` works on the caller's board, so every caller was
affected. In the importer it announced itself as a replay failure. In
self-play generation it announced nothing at all: the engine searched,
labelled and played from positions that had never occurred. Measured by the
same replay test, before the fix 5,396 of 11,544 games were corrupted
mid-way; after it, 4 of 1,343.

**What it invalidates:**

| | |
|---|---|
| every training pool | labels computed on corrupted boards, and self-play games played from them. All of them are suspect and are being regenerated |
| the 93.9% ceiling | a network fitting corrupted targets to 93.9% was fitting partly-garbage. The ceiling has to be re-measured on clean data |
| Elo comparisons | both arms carried the bug, so the rankings are roughly fair, but the games contained illegal positions |
| Perft | unaffected: it is pure move generation with no search |

**And an uncomfortable measurement.** The fixed engine against the buggy one,
1,000 games at depth 4 from real openings: **-21 +/- 22**. The bug was
accidentally acting as more aggressive null-move pruning, because the garbage
scores it returned pruned harder. The fix stays, since it is the difference
between training on real positions and training on impossible ones, but it
carries a real cost at fixed depth.

That points at something concrete rather than a regret: the null-move
reduction is under-tuned. It is fixed at 3 plies with no verification search
and no depth scaling, and a bug that pruned harder by accident was worth
about 20 Elo. Tuning it deliberately is the obvious next search change.

# Next steps to improve the engine

## Task list, 2026-09-07 night: the smoothing lever

The ladder as designed does not climb, and the reason is measured rather
than guessed. Deepening the labels makes the network *more accurate and
jumpier*, and Elo follows jumpiness, not accuracy. All scored on one judge
set:

| network | trained on | explains | jump | Elo |
|---|---|---|---|---|
| main | self-play, depth-3, 2.78M | 82.6% | 0.395 | +0 +/- 22 |
| d3ctl | real games, depth-3, 400k | 84.9% | 0.453 | -63 +/- 31 |
| rung1 | real games, depth-5, 400k | 86.0% | 0.470 | -68 +/- 31 |

Every step up in accuracy cost smoothness and cost Elo. Learning rate was
ruled out as the cause: swept 0.0003 to 0.01, all four land within 0.005 of
each other on jumpiness.

### What worked instead

The Go trainer had a spatial smoothing prior, pulling each square's weights
toward its neighbours' after every epoch, and it had never been ported to
PyTorch. Porting it broke the tradeoff for the first time:

| smoothing | explains | jump |
|---|---|---|
| 0 | 85.8% | 0.462 |
| 0.05 | 86.6% | 0.453 |
| 0.15 | 86.5% | 0.430 |
| 0.5 | 85.1% | **0.375** |
| 0.7 | 84.1% | 0.361 |

On the same 400k pool that raced -63 without it, smoothing 0.5 raced
**-7 +/- 22**. Fifty-six Elo recovered from a regulariser.

### Running now

- [~] **The full pool with smoothing**: 3.18M positions, 93.4% explained at
  jump **0.328**, against the +0 network's 0.395. Racing over 1000 games.
  This is the first network that is both more accurate and smoother than
  anything adopted, so it is a real test of whether smoothness is causal.

### Next, in order

- [ ] **If it wins**: adopt, then re-run the ladder with smoothing on at
  every rung. The rung mechanism is built and tested
  (`-label-champion`, `scripts/ladder.sh`); it failed on the axis it was
  optimising, not on its plumbing.
- [ ] **Sweep the blend** for whatever wins. 0.45 has never been optimised.
- [ ] **If it loses**: the remaining variable is volume. The smoothed 400k
  net reached -7 and the unsmoothed 2.78M net reached +0, so both axes
  matter and neither alone is enough. Import the rest of the games file at
  depth 3, which runs at 500-600 positions per second, and retry at 3M+.
- [ ] **Tune the null-move reduction.** Fixed at 3 plies, no verification
  search, no depth scaling. A bug that pruned harder by accident was worth
  about 20 Elo, which is the strongest hint in the campaign that this is
  under-tuned.
- [ ] Verify whatever wins at depth 5; matches run at depth 4 and the
  browser plays at depth 5.

## Current state, and the one thing worth doing next

Confirmed strength: one improvement this session, +16 +/- 12 over 3000
games (mobility and attacker-counting king safety), which is 0.85% on a
base of about 1879. Calibration reads 1955.

Eleven further evaluation changes were measured and every one came back
at or below zero. The hand-written evaluation is at a local optimum that
single-term changes do not escape, which is what the original diagnosis
predicted: the limit is the shape of a linear evaluation, not the value
of any coefficient in it.

The network is the only thing still moving: -798 Elo at the start of the
session, +6 +/- 19 at depth 6 now, with its jumpiness down from 3.7x the
hand evaluation to 1.09x. Its remaining gap is smoothness, and smoothness
is what data buys, so **the one thing worth doing next is running the
loop for a long time**. It is configured for throughput (662 labelled
positions per second, about 2.4M an hour) and checkpoints every
generation, so it can be left alone and picked up later.

What would need to change to go much further is architectural rather than
incremental: production NNUE trains on the order of 10^9 positions, which
is weeks at this rate. Reaching twice the current rating is that kind of
undertaking, not a tuning exercise.


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

The depth-to-Elo curve flattens hard, which is the same finding measured
a third way:

| step | Elo per ply | how measured |
|---|---|---|
| depth 2 -> 5 | ~320 | round-robin, 280 games |
| depth 5 -> 6 | **+14 +/- 27** | head-to-head, 600 games |

An extra ply is worth a fifth of nothing once past depth 5, while costing
3.8x the time. A healthy engine gets 50-70 Elo per ply well past this
depth. The search is not broken (see the blunder analysis below: no
blunder in the sample was a hung piece); it has simply run out of things
its evaluation can tell apart.

## The network is better at depth 6 than at depth 4

Same network, same blend, two search depths:

| depth | Elo vs the hand-written evaluation | games |
|---|---|---|
| 4 | -31 +/- 24 | 800 |
| 6 | **+18 +/- 34** | 400 |

A 49 Elo swing and a change of sign. Every network measurement in this
project had been taken at depth 4, chosen because it is four times
cheaper, while the engine that plays runs at depth 5 and calibration at
5 or 6. The cheap measurement was answering a different question from the
one that matters, and it had been answering it for the whole session.

It is also the central diagnosis running backwards. More depth stopped
paying because the evaluation could not tell positions apart, so a deeper
search had nothing extra to find. An evaluation that discriminates better
should convert depth into strength again, and this is what that looks
like.

**Measure networks at the depth the engine plays at, not at the depth
that is cheap to measure.**

The effect is specific to the network, not to evaluation terms in
general. A passed-pawn bonus measures +7 +/- 12 at depth 4 and +9 +/- 24
at depth 6: the same small unconfirmed positive at both. That makes
sense: one term shifts the evaluation slightly, while the network
replaces it wholesale, so only the network's interaction with search
depth is large enough to change a sign.

Confirmed at power, and it half survived:

| depth | Elo | games |
|---|---|---|
| 4 | -31 +/- 24 | 800 |
| 6 | **+6 +/- 19** | 1200 |

The +18 regressed toward zero like every other small-sample result here,
so the network is *not* an improvement even at depth 6. But the signs
still differ and the two are 37 Elo apart, so the depth effect is real
even though the level is not yet positive: the network is roughly neutral
at the depth the engine plays and clearly negative at the depth that was
cheap to measure.

The training loop now runs its test match at depth 6 for this reason. It
costs four times as much per game and is worth it, because a measurement
at the wrong depth was answering the wrong question for the whole
session.

## Confirmed improvements

| change | measured | games |
|---|---|---|
| mobility + attacker-counting king safety | **+16 +/- 12** | 3000 |

That is the whole list. It is 0.85% on a base of about 1879.

Both terms measured inside their margins individually (+9 +/- 34 and
+13 +/- 34 over 400 games each) and were correctly not adopted then. The
method that worked is: implement several cheap terms, stack them, and
measure the stack at 3000 games. A term worth 5 to 15 Elo cannot be
confirmed alone at any affordable sample size, because 400 games resolve
+/- 34.

## Promising results that did not survive more games

Every one of these looked worth adopting at small sample size and shrank
toward zero when measured properly. This is the single most useful table
in the file.

| change | small sample | at power |
|---|---|---|
| disable LMR | +23 +/- 39 (300) | +9 +/- 17 stacked (1500) |
| quiescence cap 4 -> 12 | +23 +/- 39 (300) | as above |
| passed pawn bonus 0.06 | +24 +/- 24 (800) | **+7 +/- 12 (3000)** |
| rook on 7th + doubled rooks + tempo | (not tested small) | +3 +/- 17 (1500) |
| outposts, connected/backward pawns, bad bishops | (not tested small) | -11 +/- 17 (1500) |
| piece-square tables scaled 1.3x | (single parameter) | -10 +/- 24 (800) |
| scaled LMR + passed pawns, at depth 6 | +14 +/- 28 and +9 +/- 24 apart | **+7 +/- 17 together (1600)** |
| futility pruning | +55 +/- 63 (120) | kept for the 25% node reduction, not for Elo |

Six hand-weighted evaluation terms have now been added and measured in
two batches, and both batches came back at zero or below. Against that,
the one batch that worked (mobility and king safety, +16 +/- 12) was also
hand-weighted. So the failure is not "hand-picked weights never work",
it is that the hit rate is low and only a 3000-game match can tell which
batch is which.

SPSA was pointed at the five newest weights and abandoned after six
iterations: at a gain that keeps the run stable, it moves a weight of
0.18 by about 0.002 per iteration, so it needs hundreds of iterations to
say anything. That is hours of the machine for a term worth perhaps 10
Elo, against a network that is currently at parity and improving with
every generation of data. The cores went to the network.

The pattern is consistent enough to be a rule: a result whose margin
exceeds its estimate is not evidence, however encouraging it looks, and
roughly three quarters of them evaporate.

## What has been tried, and what it measured

| Change | Result | Verdict |
|---|---|---|
| Futility pruning (Heinz 1998) | +55 +/- 63, 25% fewer nodes | kept as a speed win |
| Texel tuning on game outcomes | -16 +/- 48 (200 games) | rejected |
| Texel tuning on Stockfish scores | +20 +/- 26 (700 games) | unconfirmed, not default |
| NNUE, full replacement, sigmoid target | -700 +/- 112 | rejected |
| NNUE, residual on depth-12 search | -338 +/- 76 | rejected |
| NNUE, residual on static eval | -308 +/- 70 | rejected |
| Castling | +19 +/- 39 (300 games) | kept, it is a rule |
| Disabling LMR | +23 +/- 39 | worth retesting at power |
| Quiescence cap 4 -> 12 | +23 +/- 39 | worth retesting at power |
| Quiescence cap 4 -> 24 | +6 +/- 39 | no |
| Disabling null-move | -13 +/- 39 | no |
| Mobility term, hand-picked weights | +9 +/- 34 (400 games) | neutral, off by default |
| Mobility term, fitted weights | -73 +/- 35 (400 games) | rejected |
| Full fitted set incl. mobility | -55 +/- 34 (400 games) | rejected |
| Fused evaluation, one pass not seven | 1.20x faster, identical output | kept |
| Table reuse per game, 24-byte entries | ~1.5x faster in matches | kept |
| Occupied list as indices, no board clone | 1.28x total at depth 7 | kept |

Nothing here is confirmed at 1%. Several land around +20, which is
exactly the size that 300 games cannot resolve.

## Do these next, in this order

### 1. Confirm the +20s at power, then stack them

`-no-lmr` and `-qply 12` each measured +23 +/- 39. If both are real, the
pair is worth ~45 Elo, which 1500 games can see. Run each alone at 1500
games, then together. This is the cheapest available Elo in the
repository right now and needs no new code.

### 2. Tune against game results (SPSA), not against another engine

This is the biggest lesson of the session and it invalidates most of what
was tried. Four fits, three targets, all of them improved the objective
they were given, none improved play:

| target | fit improvement (train / held out) | measured Elo |
|---|---|---|
| self-play game results | 1.8% / 2.1% | -16 +/- 48 |
| Stockfish depth-12 search | 11.5% / 10.8% | +20 +/- 26 |
| Stockfish static evaluation | 14.0% / 14.4% | -55 +/- 34 |
| mobility weights alone, fitted | 14.0% / 14.4% | **-73 +/- 35** |

The mobility row is the cleanest evidence, because it is one isolated
term: hand-picked weights measured +9 +/- 34, the fitted ones measured
-73 +/- 35. An 82 Elo swing in the wrong direction, bought with a 14%
better fit.

The reason is that squared prediction error is not playing strength.
Stockfish's static evaluation is an NNUE designed to be corrected by a
twenty-ply search, so it can afford to say almost nothing about king
placement: its search sees the attack coming. A five-ply search cannot,
and needs an evaluation that overstates precisely what a deep search
would find for itself. Fitting one to the other strips out the terms
this engine most depends on, visibly: the fit drove the king table to
zero and the queen table to 0.40.

So stop fitting to another engine's opinion and optimise the actual
objective. SPSA (Simultaneous Perturbation Stochastic Approximation) is
what engines use for this: perturb every parameter at once, play a match,
step in the direction that won. It needs a few hundred games per
iteration, which is affordable now that a 400-game match at depth 4 takes
about two minutes.

Start with fewer than ten parameters (piece values and the mobility
weights), because the number of games needed grows with the parameter
count, not with how much each one matters.

### 3. The neural evaluation does not have enough data, at any size

Settled with a measurement, after three bugs were fixed to get an honest
one. Held-out error, split **by game**, against the hand-written
evaluation on the same positions:

| hidden units | parameters | net held-out | hand eval | ratio |
|---|---|---|---|---|
| 4 | 3k | 15.99 | 7.86 | 2.0x worse |
| 16 | 12k | 13.64 | 5.07 | 2.7x worse |
| 64 | 49k | 18.40 | 2.67 | 6.9x worse |
| 256 | 197k | 16.87 | 8.13 | 2.1x worse |

The network is worse than the hand-written evaluation at every capacity,
including one small enough that it cannot overfit. So this is not a
capacity problem and not a regularisation problem: a few hundred
self-play games do not contain enough information to learn an evaluation
from scratch, and twenty hand-set parameters informed by chess knowledge
beat anything learnable from that much data.

Real NNUE is trained on billions of positions from millions of games.
Generating that here at a useful label depth would take days, not the
minutes the rest of this loop takes.

**Three bugs had to be fixed before that measurement meant anything**,
and each one had produced a confident wrong conclusion:

1. The network was a residual (its output added to the hand evaluation)
   but trained on the full search score, so the engine counted the
   evaluation twice. -211 +/- 41.
2. It trained on all positions, including tactical ones where the gap
   between static and search score *is* the tactic and no static function
   can predict it. Filtering to quiet positions cut held-out error from
   3.30 to 1.35.
3. **The held-out split was by position, not by game.** Consecutive
   positions in a game differ by one move, so every held-out position had
   near-copies in the training set. This reported "explains 96% of
   variance, 7x better than the hand evaluation" for a network that lost
   0-0-60. Splitting by game turned that same number into "explains 31%,
   2.7x worse".

Number 3 is the one to remember. It is not specific to chess: any
sequential data split at random will do this, and the symptom is exactly
what happened here, an excellent validation score attached to a model
that fails completely in use.

### 4. If the self-play loop is resumed

Running now (`trainloop`). The engine plays itself, learns to predict what
its own deeper search concludes, and each network must win a match
against the champion (elo > margin, not elo > 0) to be adopted.

Two bugs found and fixed while getting it working, both worth remembering:

- The network is a **residual**: at play time its output is added to the
  hand-written evaluation. It was being trained on the full search score,
  so the engine counted its evaluation twice. That was -211 +/- 41.
- It was training on **all** positions. Where a capture sequence is
  pending, the gap between static and search score *is* the tactic, and
  no function of piece placement can predict it. Filtering to quiet
  positions (not in check, quiescence within 0.35 pawns of static) cut
  held-out error from 3.30 to 1.35.

Progression so far: -211, -80, -67, -50, all rejected. The remaining
constraint is data: training error 0.55 against held-out 1.35 is a
shortage of positions, not of capacity. The pool grows 26k quiet
positions per generation toward a 400k window.

If it is still negative at generation 15, the thing to change is the
network input, not the amount of data. 768 binary features cannot express
"this knight is defended", and a piece-square-only input is close to what
the hand-written evaluation already computes, so there may be little left
for it to add. Real NNUE uses king-relative features (HalfKP), where every
piece feature is paired with the king's square, which is what lets it
learn king safety at all.

### 5. Fix why the offline network failed, rather than abandoning it

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

### 6. En passant

The only chess rule still missing. Worth little Elo directly, but it is a
rule, and its absence means FEN cannot describe some positions the
opponent can reach.

### 7. Tune the tables entry by entry

The tuner currently fits 6 piece-square scalars and could fit all 768
entries given enough positions. Do this only after item 2: fitting more
parameters against the same wrong objective will just find a worse
engine faster. If the tables are fitted at all, verify the result in
games before believing it, the way the mobility weights were.

### 8. King safety that counts attackers

Implemented behind `-king-safety <weight>`, counting attackers on the
squares around the king and scaling superlinearly with how many. A first
scan measured +13 +/- 34 at weight 0.01 and was interrupted before the
higher weights finished. Worth completing at 1500 games.

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

## What the engine actually gets wrong

`analyze` plays Stockfish and has Stockfish score every position before
and after each of this engine's moves. First run, two games, blunder
threshold 0.8 pawns:

- **Not one blunder was a hung piece.** The search is tactically sound.
- Losses are positional and concentrated while most pieces are still on
  the board: 14.7 of 18.3 pawns given away with a full board.
- The engine visibly shuffles: e3-c1, c1-g5, g5-c1 with a bishop.

The shuffling is not the random tie-break, which was the obvious suspect.
Measured over 60 positions, only 1.4 moves tie for best out of 32.9 legal,
so the engine genuinely prefers those moves. That makes it an evaluation
problem, and it is the clearest description yet of which part: nothing in
the evaluation rewards making progress, so a move that returns a piece to
where it came from can score the same as a useful one.

Run a longer analysis (20+ games) before acting on this: two games is a
small sample for a claim about where the Elo goes.

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

## Label depth, redone on the fixed engine, 2026-09-08

The earlier depth-3 against depth-5 comparison ran through a transposition
table that keyed every node on the root's side to move, so it measured a
broken search and was void. Redone.

Design: one PGN, read from the start with the same skip, so all arms label
exactly the same positions and differ only in the depth of the search that
scores them. 60000 positions per arm, which is what depth 8 can reach in a
night at 4 positions per second. Each arm trains alone on its own pool with
the same recipe, then plays 1000 games at depth 4.

  depth 3 labels   79.8% explained   jump 0.438   reference
  depth 5 labels   77.8% explained   jump 0.445   -21 +/- 22

Shallower labels are more accurate, smoother, and no worse over the board,
which is the opposite of what the pre-fix run reported. The engine plays at
depth 4, so depth 3 is a ply below what it can already see and depth 5 a
ply above, and neither position in that range makes a difference.

Two guards went in with this, because a dead flag has already cost this
project a whole ladder. TestPGNLabelDepthAloneChangesTheLabels holds the
labeller fixed and varies only the depth argument; the older test varied
both and would have stayed green if the argument were ignored.
TestPGNLabelDepthKeepsTheSamePositions checks the arms are paired, so an
Elo difference cannot be a different sample.

Depth 8 is generating, four plies above play depth. If it also lands on
zero, label depth is not the lever and the limit is elsewhere: capacity,
volume, or the distillation ceiling that caps any network trained to
predict its own teacher.

### Depth 8, four plies above play depth: also nothing

  depth 3 labels   79.8% explained   jump 0.438   reference     ~2000 pos/s
  depth 5 labels   77.8% explained   jump 0.445   -21 +/- 22       186 pos/s
  depth 8 labels   76.5% explained   jump 0.430   -15 +/- 22         4 pos/s

Three arms, the same 60000 positions, the same recipe, 1000 games each at
play depth 4. Every interval contains zero and none of them beats the
cheapest arm. Labelling four plies deeper than the engine plays costs 500
times more per position and buys nothing measurable.

Worth noting against our own heuristic: depth 8 has the best smoothness of
the three, 0.430, and still does not win. Smoothness predicted Elo well
across earlier networks; at this scale it does not decide anything.

The honest limit of this result. At 60000 positions all three networks are
data-starved, and volume has already been shown to dominate here: 400k
clean labels measured -61 against the champion where 7.5M mixed ones
measured -20. So this cannot rule out that depth-8 labels pay off at a
volume where the network is not starved. What it does settle is that they
are unaffordable at that volume: matching the 7.5M corpus at 4 positions
per second is about 21 days of labelling, against a few hours at depth 3.

So the deeper-label idea for rescuing the ladder is not supported. The
distillation ceiling stands: a network trained to predict its own teacher's
score cannot pass the teacher, and changing how deep the teacher looks does
not change that.

## Plan toward Deep Blue 1997 (~2700), set 2026-09-08

Standing at 2465 on the Stockfish ladder at fixed depth 6. Ordered by
expected yield per hour; gains are this session's own yardsticks (a ply
measured +130, a speed doubling is about a ply), not promises. Each has
the measurement that closes it. Adoption rule everywhere: past the margin,
same clock both sides, or it did not happen.

 1. Trust the ruler: two timed calibrations, same clock both sides, agree
    within +/- 50 on the 3000-ladder. First one running at 1s/move.
 2. Play on the clock (time_ms in champion.json) if the timed number beats
    fixed depth 6. Expected +100-200.
 3. Time management: iteration-cost prediction, use of the whole budget.
    Exit: mean depth reached per second, up. Expected +20-40.
 4. Re-tune pruning on the corrected search: null-move reduction, LMR
    scaling, futility margins were all tuned through the broken table.
    1500 games each. Expected +30-80.
 5. Move ordering: countermoves, history aging, SEE-ordered captures.
    Exit: fewer nodes to the same depth, then Elo. Expected +20-50.
 6. Endgames: re-measure the tablebase probe now that it indexes the right
    colour (it read -13 +/- 18 through the bug). Expected +10-20.
 7. Opening book from the PGN database (match history is allowed, scores
    are not). Exit: 1000 games past the margin. Expected +10-30.
 8. Evaluation on clean labels, then the lambda (game-outcome) sweep whose
    400k pools exist (lam_*.bin). Honest expectation 0-30.
 9. Speed I: pin-based legality, no make/unmake per candidate when in
    check or pinned. Exit: identical node count, less time. ~10%.
10. Speed II: bitboards. 1.6-1.8x nodes/s, +50-70. Days; last.

## Stockfish-ideas campaign and the 100% coverage requirement, 2026-09-08 evening

Screen: feature on against off, both on a 200ms clock, 400 paired games
(scripts/screen.sh). Adoption only on the slow ruler: 1s/move against
Stockfish 2400, 300 games, against a 300-game baseline (queued).

  late move pruning       -57 +/- 35   rejected: needs ordering we lack
  logarithmic LMR         +23 +/- 34   survivor, to the slow ruler
  deep reverse futility   +40 +/- 34   past the margin on the screen
  null-move gate          +31 +/- 49   at 200 games, running
  countermoves, IIR, SEE  implemented, screening next

Coverage: board, moves, game at 100%; pytorch at 100% (scripts/
pycoverage.sh); engine and nnue in progress (scripts/coverage.sh). Two
defects found by the coverage tests: a corrupt checkpoint crashed the
trainer, and evalnet read a network's shape outside its skip guard.

### Measurement design: pair the arms, do not calibrate them separately

Reverse futility screened at +40 +/- 34 (400 paired games, 200ms) and
then, measured the way the campaign specified, came out inconclusive:

  baseline                300 games vs SF 2400 at 1s: 64-81-155, 0.348 -> 2291
  with reverse futility   300 games vs SF 2400 at 1s: 78-76-146, 0.387 -> 2320
  difference: +29 +/- 59, which contains zero

Not a contradiction, a design flaw. Two independent samples against a
third party have errors that add, and nothing cancels: 600 games bought
+/- 59. Four hundred paired games head to head, the same feature against
the same engine without it from the same openings with colours reversed,
buy +/- 34 for two thirds of the cost, because the opening and the
opponent are held fixed and only the feature varies.

So: screen and confirm head to head, and calibrate only to place the
adopted result on the Stockfish scale. A calibration is an instrument for
absolute position, not for comparing two builds.

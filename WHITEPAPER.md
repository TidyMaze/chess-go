# Measurement Discipline in Chess Engine Development: A Case Study in Negative Results

**A technical report on the construction and evaluation of a classical alpha-beta chess engine, and on the measurement failures encountered while attempting to improve it.**

Date: 2026-09-05
Codebase: `chess-go`, Go 1.x, no external dependencies
Hardware: Apple Silicon, 10 cores
Reference engine: Stockfish 17 (Homebrew build)

---

## Abstract

We describe the construction of a classical chess engine in Go and a
systematic attempt to raise its playing strength beyond approximately
1850 Elo on a Stockfish-calibrated scale. Nineteen distinct interventions
were implemented and measured, spanning search extensions, evaluation
terms, three families of supervised evaluation tuning, seven neural
network configurations, and one stochastic optimiser operating directly
on game results. **None produced a confirmed improvement exceeding 1%
(19 Elo).**

The principal contribution of this report is not the engine but the
measurement methodology, and specifically three classes of measurement
error that each produced a confident but false conclusion before being
detected:

1. **Leaky validation splits.** Holding out positions rather than whole
   games inflated a network's apparent quality by a factor of seven and
   reported "explains 96% of variance" for a model that subsequently lost
   60 games out of 60.
2. **Objective mismatch.** Four supervised fits improved their stated
   objective (squared prediction error) by 1.8% to 14.0% while reducing
   playing strength, in one isolated case by 82 Elo.
3. **Contaminated benchmarks.** A CPU profile attributing 38.6% of
   runtime to one function was an artefact of a concurrent background
   job; on an idle machine the true figure was 19.5%.

We further report a quantitative diagnosis of the engine's strength
ceiling: agreement with a depth-14 reference engine plateaus at search
depth 4, and the marginal value of one additional ply falls from
approximately 98 Elo (depth 2 to 4) to 14 +/- 27 Elo (depth 5 to 6). We
argue that this pattern identifies the evaluation function, not the
search, as the binding constraint, and we show that this constraint is
not removable by the methods available at our data scale.

---

## 1. Introduction

Chess engine development has an unusually favourable property as an
engineering discipline: the objective is directly measurable. Two
versions of a program can play each other, and the result is a
quantitative statement about which is better. This report is an account
of what happens when that measurement is taken seriously.

The engine described here was built test-first, validated against
Stockfish for rule correctness, and improved incrementally. It reached
approximately 1850 Elo and stopped. This report documents the attempts to
move it further, nearly all of which failed, and argues that the failures
are more informative than the successes would have been.

### 1.1 Scope and honesty of reporting

Every number in this report is measured, not estimated. Where a result is
within its own confidence interval it is reported as inconclusive rather
than as a small positive. Where an earlier conclusion in the project was
subsequently found to be wrong, both the wrong conclusion and its
correction are reported, because the mechanism of the error is the
transferable part.

---

## 2. System architecture

### 2.1 Board representation

The board is a 12x12 padded array of single-byte cells (0 = off-board,
1 = empty, 2+ = colour x 6 + piece type + 2). Padding removes bounds
checking from sliding-piece move generation, which would otherwise
dominate. A separate list of occupied squares, held as 0-63 indices,
allows iteration over pieces without scanning 64 cells.

Moves are applied by make/unmake against a single board rather than by
copying, with an undo record capturing the changed state. Castling rights
are board state, since they are lost by moves and by rook captures, and
are therefore saved and restored with the pieces.

### 2.2 Search

The search is a negamax-style alpha-beta with the following techniques,
each of which was implemented separately and measured:

| Technique | Reference |
|---|---|
| Alpha-beta pruning | Knuth and Moore (1975) |
| Iterative deepening | Slate and Atkin (1977) |
| Killer move heuristic | Akl and Newborn (1977) |
| Principal variation search | Reinefeld (1983) |
| Null-move pruning | Beal (1989) |
| History heuristic | Schaeffer (1989) |
| Futility pruning | Heinz (1998) |
| Late move reductions | folklore, 1990s |
| Transposition table | Zobrist (1970) |
| Quiescence search with static exchange evaluation | standard |
| Aspiration windows | standard |
| Check extensions | standard |

Move ordering is transposition-table move first, then captures by
most-valuable-victim / least-valuable-attacker, then killers, then the
history table.

### 2.3 Evaluation

A linear evaluation over: material, tapered piece-square tables
interpolated between middlegame and endgame by remaining material, bishop
pair, pawn structure (isolated, doubled, passed), rook placement on open
and semi-open files, king pawn shelter, an attacker-counting king danger
term, a mobility term, and an endgame king-driving term for converting
won positions.

### 2.4 Correctness validation

Move generation is validated against Stockfish by comparing full legal
move lists across 200 positions drawn from self-play, using Stockfish's
`go perft 1`. The engine implements a rule subset (no en passant, queen
promotion only), so its FEN output declares en passant unavailable and
the reference engine is asked about exactly the position under test.
Agreement is exact.

This validation caught two defects that unit tests did not: pawns walking
off the board through an unimplemented promotion rule, and an inverted
sign in terminal scoring that caused the engine to avoid delivering
checkmate while every narrower test passed.

---

## 3. Measurement methodology

### 3.1 The resolution problem

The central practical difficulty is that the interventions under test are
small relative to the noise of the measurement. We characterise the
available instruments:

| Method | Cost | 95% resolution | Appropriate use |
|---|---|---|---|
| Stockfish calibration | ~140 s | +/- 130 Elo | absolute placement |
| Head-to-head, 300 games | ~2 min | +/- 39 Elo | rejecting bad ideas |
| Head-to-head, 1500 games | ~10 min | +/- 17 Elo | confirming a change |
| Head-to-head, 5000 games | ~35 min | +/- 10 Elo | a 1% change |
| Reference-agreement probe | ~1 min | not an Elo | search vs evaluation |

Six calibration runs of near-identical builds returned 1892, 1909, 1840,
1823, 1866 and 1786 Elo. The standard deviation of that set is
approximately 45 Elo, which is the measurement, not the engine. **A
calibration run cannot detect a 1% improvement.** This single fact
invalidates a large class of naive development loops.

### 3.2 Harness validation

Before trusting any result, the harness itself was validated in two ways.

First, self-consistency: two identical engines were played against each
other over 300 games and scored 0.500 exactly (+0 +/- 39 Elo), confirming
no colour or ordering bias.

Second, sensitivity: a known-large difference (search depth 4 against
depth 2) was measured at +196 +/- 57 Elo with randomised openings and
+198 +/- 57 without. The harness detects a genuine two-ply difference
easily, and randomised openings do not suppress signal.

### 3.3 Paired randomised openings

Matches begin from six randomly chosen plies. Without this, two engines
differing in one parameter play nearly identical games and 40% of results
are draws, wasting most of the sample. Games 2k and 2k+1 share an opening
with colours reversed, so neither engine is systematically handed the
better half of the opening distribution. This reduced the draw rate to
31%.

### 3.4 Calibration against an external scale

Absolute rating is established by playing Stockfish at five Elo-limited
settings (1600 to 2400) and combining the per-level estimates weighted by
closeness to an even match, since a lopsided result constrains the
estimate only weakly. A preliminary probe identifies the levels nearest
parity and concentrates the game budget there.

A caveat must be stated: the per-level estimates within a single build
disagree by up to 350 Elo, far exceeding the binomial sampling error of
approximately 63 Elo at 30 games per level. The disagreement is therefore
systematic, and no single Elo value describes this engine against every
opponent. The figure quoted throughout is a weighted mean and should be
read as approximate.

---

## 4. Results

### 4.1 Interventions and measured effects

| Intervention | Effect (Elo) | Games | Outcome |
|---|---|---|---|
| Futility pruning | +55 +/- 63 | 120 | kept (25% node reduction) |
| Castling implementation | +19 +/- 39 | 300 | kept (rule correctness) |
| Repetition detection in search | +1 +/- 28 | 600 | kept (rule correctness) |
| Mobility term, hand-weighted | +9 +/- 34 | 400 | inconclusive |
| King safety, attacker counting | +13 +/- 34 | 400 | inconclusive |
| Disable late move reductions | +23 +/- 39 | 300 | not replicated |
| Quiescence cap 4 to 12 | +23 +/- 39 | 300 | not replicated |
| Both of the above, stacked | +9 +/- 17 | 1500 | rejected |
| One additional ply (5 to 6) | +14 +/- 27 | 600 | see 4.3 |
| Texel tuning on game outcomes | -16 +/- 48 | 200 | rejected |
| Texel tuning on search scores | +20 +/- 26 | 700 | not adopted |
| Texel tuning on static scores | -55 +/- 34 | 400 | rejected |
| Mobility weights, fitted | -73 +/- 35 | 400 | rejected |
| Neural evaluation (7 variants) | -50 to -798 | 400 each | rejected |
| Search optimisation (1.28x faster) | ~0 at fixed depth | n/a | kept |

The two entries marked "not replicated" are instructive. Both measured
+23 +/- 39 over 300 games and appeared worth combining. Stacked and
measured over 1500 games, the pair returned +9 +/- 17. The original
estimates were noise of the size that a 300-game match cannot distinguish
from signal.

### 4.2 The evaluation ceiling

We measured how often the engine selects the same move as a depth-14
Stockfish, over an identical set of 300 self-play positions, varying only
the engine's own search depth:

| Engine depth | Full evaluation | Material only |
|---|---|---|
| 1 | 45.0% | 34.9% |
| 2 | 48.7% | 37.6% |
| 3 | 54.7% | 37.2% |
| 4 | 60.4% | 45.6% |
| 5 | 59.7% | 43.6% |
| 6 | 61.4% | 41.6% |

Two observations. First, agreement ceases to improve beyond depth 4:
depth 6 is worth one percentage point over depth 4 for six times the
computation. Second, and diagnostically more important, with a
material-only evaluation agreement *declines* beyond depth 4. A deeper
search optimises more effectively for whatever the evaluation asserts is
good; a weaker evaluation therefore becomes actively more harmful with
depth. The contrast between the two columns identifies the evaluation,
not the search, as the limiting component.

### 4.3 The depth-to-Elo curve

The same conclusion is reachable from game results alone:

| Depth increment | Elo per ply | Method |
|---|---|---|
| 2 to 4 | ~98 | head-to-head, 200 games |
| 4 to 5 | +60 +/- 70 | head-to-head, 100 games |
| 5 to 6 | +14 +/- 27 | head-to-head, 600 games |

A healthy engine obtains 50 to 70 Elo per ply well beyond this depth. The
collapse observed here is not attributable to the search being defective:
the effective branching factor was measured at 1.9 to 4.6 per ply
(mean approximately 2.4), which is normal for well-ordered alpha-beta,
and blunder analysis (Section 4.4) found no tactical failures. The search
is functioning and has exhausted what its evaluation can discriminate.

### 4.4 Blunder analysis

Playing Stockfish while a separate strong Stockfish instance scores every
position before and after each of the engine's moves permits direct
attribution of losses. Over an initial sample, using a threshold of 0.8
pawns:

- **No blunder involved a hung piece.** The search is tactically sound.
- Losses are positional and concentrated in positions with most pieces
  still on the board (14.7 of 18.3 pawns conceded).
- The engine was observed repeating a bishop manoeuvre (e3-c1, c1-g5,
  g5-c1).

The repetition was initially attributed to the random tie-break among
equally-scored root moves. This hypothesis was tested and rejected: over
60 positions, a mean of only 1.4 moves out of 32.9 legal moves tied for
best. The engine genuinely preferred those moves, making it an evaluation
defect rather than a tie-breaking artefact.

This analysis did, however, expose a genuine omission: repetition was
detected only as a root tie-break and not within the search tree. A test
position with White a queen ahead scored a repetition at 9.67 rather than
0. This was corrected, though it measured only +1 +/- 28 Elo, since
between engines of equal strength repetitions rarely decide games.

---

## 5. Three classes of measurement failure

This section is the substance of the report.

### 5.1 Leaky validation splits

A neural evaluation was trained on positions from self-play games, with a
random 15% of *positions* held out for validation. It reported a held-out
squared error of 1.36 against the hand-written evaluation's 5.89 on the
same positions, an apparent fourfold improvement, and "explains 96% of
target variance".

The same network lost 60 games out of 60.

The cause is that consecutive positions within a chess game differ by one
move. A randomly held-out position therefore has near-duplicates in the
training set, and a model with sufficient capacity scores well by
recognising neighbours rather than by generalising. Re-splitting by
*game* rather than by position changed the same measurement to a held-out
error of 10.05 against the hand evaluation's 3.78: not four times better,
but 2.7 times worse.

This failure mode is not specific to chess. Any sequential or
autocorrelated data source split at random will produce it, and the
symptom is characteristic: an excellent validation score attached to a
model that fails completely in deployment.

### 5.2 Objective mismatch

Four supervised fits were performed against three targets. All four
improved the objective they were given. None improved playing strength:

| Target | Fit improvement (train / held out) | Elo |
|---|---|---|
| Self-play game outcomes | 1.8% / 2.1% | -16 +/- 48 |
| Stockfish depth-12 search score | 11.5% / 10.8% | +20 +/- 26 |
| Stockfish static evaluation | 14.0% / 14.4% | -55 +/- 34 |
| Mobility weights alone, fitted | 14.0% / 14.4% | **-73 +/- 35** |

The final row is the cleanest evidence because it isolates a single term
with four parameters. Hand-chosen weights measured +9 +/- 34 Elo. Weights
fitted to Stockfish's static evaluation, improving squared error by 14%,
measured -73 +/- 35. An 82 Elo movement in the wrong direction, purchased
with a better fit.

None of this is overfitting: the held-out slice improved as much as the
training slice in every case. The fits are genuine. They optimise the
wrong quantity.

The mechanism is instructive. Stockfish's static evaluation is a neural
network designed to be corrected by a twenty-ply search. It can afford to
say very little about king placement, because its search observes the
attack arriving. A five-ply search cannot, and requires an evaluation
that *overstates* precisely those features a deep search would discover
independently. Fitting the former to the latter removes the terms the
shallow engine most depends upon, which is directly visible in the fitted
parameters: the king piece-square table was driven to zero and the queen
table to 0.40 of its original scale.

Squared prediction error and playing strength are different objectives.
Beyond a point they diverge, and the divergence is not small.

### 5.3 Contaminated benchmarks

A CPU profile attributed 38.6% of runtime at search depth 7 to the
transposition table probe, and a further 13.1% to the store: over half of
all computation in a direct-mapped array lookup. This motivated packing
the table entry from 64 bytes to 16.

The profile had been taken while a 1500-game match occupied the machine.
On an idle machine the probe accounts for 19.5%. The packing, measured
correctly by alternating between configurations on an idle machine, was
speed-neutral (96.4 ms against 97.2 ms per operation).

A second error compounded it: packing the score field from float64 to
float32 appeared free, but increased the searched tree by 18% (80,704
nodes against 68,277 at depth 6). Principal variation search probes with
zero-width windows of 1e-6, and float32 quantises a mate score near 1000
more coarsely than that window, so scores returned from the table failed
their windows and forced re-searches.

Both errors were detected only by re-measuring on an idle machine with
alternating configurations. Neither would have been visible in a single
before-and-after comparison.

---

## 6. The neural evaluation, and why it fails here

Seven configurations of a neural evaluation were implemented and
measured: a 768-input, one-hidden-layer network with clipped ReLU
activation, trained by stochastic gradient descent written directly
against the same inference code used in play.

Three implementation defects were found and corrected in sequence, each
having produced a confident wrong conclusion:

1. **Target and use mismatched.** The network was applied as a residual
   (its output added to the hand evaluation) but trained on the full
   search score, causing the engine to count its evaluation twice.
   Measured -211 +/- 41.
2. **Unfiltered training positions.** Where a capture sequence is
   pending, the difference between static and search score *is* the
   tactic, and no function of piece placement can predict it. Filtering
   to quiet positions reduced held-out error from 3.30 to 1.35.
3. **Leaky validation** (Section 5.1).

With those corrected, capacity was varied to isolate the constraint:

| Hidden units | Parameters | Held-out error (by game) | Hand evaluation | Ratio |
|---|---|---|---|---|
| 4 | 3k | 15.99 | 7.86 | 2.0x worse |
| 16 | 12k | 13.64 | 5.07 | 2.7x worse |
| 64 | 49k | 18.40 | 2.67 | 6.9x worse |
| 256 | 197k | 16.87 | 8.13 | 2.1x worse |

The network is worse than the hand-written evaluation at every capacity,
including capacities far too small to overfit. This excludes both
overfitting and regularisation as explanations. The remaining explanation
is data volume: a few hundred self-play games do not contain sufficient
information to learn an evaluation from scratch, and approximately twenty
hand-set parameters encoding established chess knowledge outperform
anything learnable at this scale.

For calibration: production NNUE implementations train on the order of
10^9 positions. At the generation rate achieved here (approximately 100
labelled quiet positions per second, limited by the depth-5 search used
to label them), 10^7 positions requires 28 hours and 10^8 requires 12
days on this hardware.

A second and more fundamental limit applies. The training labels are
produced by the engine's own search, which is an approximately 1850-Elo
player. Supervised imitation of an 1850 evaluator converges toward 1850.
Bootstrapping can exceed the teacher, but slowly, and not by the factor
of two that would be required here.

---

## 7. Direct optimisation of game results

The one method that targets the correct objective is stochastic
optimisation against match outcomes. We implemented SPSA (Spall, 1992):
perturb all parameters simultaneously by random signs, play the two
perturbed configurations against each other, and step in the direction
that won. Its cost is two matches per iteration independent of parameter
count, which is why it is the standard method in engine development.

At the time of writing, 21 iterations of 200 games have been completed
over seven parameters. Parameter drift is modest and the mean score of
the perturbed-positive side is 0.4989, consistent with the iteration
signal being dominated by noise at this sample size, as expected: a
200-game match resolves approximately +/- 48 Elo, so no single iteration
distinguishes a good step from a bad one. SPSA does not require it to;
it requires the steps to be correct slightly more often than chance.

The run concludes with a 1200-game comparison of the tuned vector against
the starting vector, adopted only if the result exceeds its own
confidence margin. **This result is not yet available and is therefore
not claimed.**

An implementation defect is worth recording: the initial step-size gain
produced updates of approximately 7e-5 against parameter step sizes of
0.012. The optimiser ran for five iterations without altering any
parameter in the third decimal place, which is externally
indistinguishable from an optimiser that has converged.

---

## 8. Discussion

### 8.1 What limits this engine

The evidence converges from three independent directions on the same
conclusion. Reference-agreement plateaus at depth 4. The marginal Elo
value of a ply collapses from 98 to 14 between depth 4 and depth 6.
Blunder analysis finds no tactical errors and only positional drift. The
search is correct, well-ordered, and adequately fast; the evaluation is
the binding constraint.

The difficulty is that this diagnosis, while correct, has proven
non-actionable at this scale. Every method of improving the evaluation
available to us either optimises a proxy objective that diverges from
playing strength (Section 5.2) or requires orders of magnitude more data
than we can generate (Section 6).

### 8.2 On negative results

Nineteen interventions produced no confirmed improvement. It would have
been straightforward to report several of them as successes: +55 +/- 63,
+23 +/- 39 and +20 +/- 26 are all positive point estimates, and each was
initially encouraging. The stacked replication of two such results
(+9 +/- 17 over 1500 games, against +23 +/- 39 each over 300) demonstrates
what those point estimates were worth.

The discipline that produced this report is simply the refusal to adopt a
result inside its own confidence interval. Applied consistently, it
converts a project that appears to be improving into one that is
demonstrably not, which is less satisfying and considerably more useful.

### 8.3 Threats to validity

- Calibration disagrees across Stockfish levels by up to 350 Elo,
  substantially exceeding sampling error. The absolute figure of ~1850 is
  therefore approximate, though the relative comparisons that carry the
  argument are unaffected.
- The engine implements a rule subset (no en passant, queen promotion
  only). Its opponents do not. The effect is small but not zero and is
  unmeasured.
- Blunder analysis is reported over a small sample and should be
  replicated over 20 or more games before its phase attribution is
  relied upon.
- The SPSA result is incomplete.

---

## 9. Conclusion

We constructed a classical chess engine reaching approximately 1850 Elo
on a Stockfish-calibrated scale, corresponding roughly to a strong club
player, and attempted nineteen interventions to improve it. None achieved
a confirmed improvement of 1% or more.

The measured diagnosis is unambiguous: the evaluation function limits
strength, the search does not. The methods available for improving a
hand-written evaluation at this data scale are either misdirected
(supervised fitting to a proxy objective) or under-resourced (neural
evaluation), and the one correctly-directed method is noise-limited at
the sample sizes affordable here.

The transferable results are methodological. Split validation data by
game and not by observation when observations are autocorrelated. Verify
that the quantity being optimised is the quantity that matters, because a
14% better fit purchased an 82 Elo loss. Re-measure benchmarks on an idle
machine with alternating configurations, because a contaminated profile
misdirected an entire optimisation effort. And decline to adopt results
inside their own error bars, however many of them are positive.

---

## References

Akl, S. G. and Newborn, M. M. (1977). The principal continuation and the
killer heuristic. *Proceedings of the ACM Annual Conference*.

Beal, D. F. (1989). Experiments with the null move. *Advances in Computer
Chess 5*.

Heinz, E. A. (1998). Extended futility pruning. *ICCA Journal*, 21(2).

Knuth, D. E. and Moore, R. W. (1975). An analysis of alpha-beta pruning.
*Artificial Intelligence*, 6(4).

Nasu, Y. (2018). Efficiently updatable neural-network-based evaluation
functions for computer shogi.

Osterlund, P. (2014). Texel tuning method. *Computer Chess Club forums*.

Reinefeld, A. (1983). An improvement to the Scout tree search algorithm.
*ICCA Journal*, 6(4).

Schaeffer, J. (1989). The history heuristic and alpha-beta search
enhancements in practice. *IEEE Transactions on Pattern Analysis and
Machine Intelligence*, 11(11).

Slate, D. J. and Atkin, L. R. (1977). CHESS 4.5: The Northwestern
University chess program. In *Chess Skill in Man and Machine*.

Spall, J. C. (1992). Multivariate stochastic approximation using a
simultaneous perturbation gradient approximation. *IEEE Transactions on
Automatic Control*, 37(3).

Zobrist, A. L. (1970). A new hashing method with application for game
playing. Technical Report 88, University of Wisconsin.

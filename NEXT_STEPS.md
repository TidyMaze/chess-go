# Next steps to improve the engine

## Toward 2700 on the same-clock ladder, 2026-09-11 evening

Standing at 2347 at 1 s/move with Stockfish on the same clock. The gap is
about 350, and today's numbers say it comes from the search: the search
gave +168 (root window), +346 (playing on the clock) and +37 (using the
whole iteration), the evaluation ladder gave +11 and then proved saturated.

- [x] **Lazy SMP built**, test first, race-detector clean. Helpers share the
  transposition table (stripe-locked only once shared, so one thread pays
  nothing), each with its own game, Eval and context, copied before the
  main search starts. Four threads on one table stay exact against plain
  minimax. Commit d896f6e.
- [x] **First version gained nothing**: 5.6x the nodes, same depth, because
  helpers iterated in lockstep with the main thread over the same tree.
  Fixed by having each helper target the main thread's published depth
  plus one or two and shuffle its root order. Mean depth in 1 s over five
  positions: 10.6 / 11.0 / 11.8 / 11.8 for 1 / 2 / 4 / 8 threads. Four is
  the number of performance cores. Commit 562cf6c.
- [x] **The SMP gain does not depend on the clock.** Mean depth over five
  positions, 1 / 2 / 4 / 8 threads: 6.2 / 6.4 / 7.2 / 7.4 at 100 ms, 7.8 /
  8.2 / 8.8 / 9.0 at 300 ms, 10.6 / 11.0 / 11.8 / 11.8 at 1 s. About one ply
  from four threads at every budget. So a 100 ms race measures it ten times
  cheaper than a 1 s one, and the 1 s race was stopped 25 minutes in.
- [x] **Every measurement now runs at 10 ms a move, by instruction** (and 1 ms
  is measured too). The 100 ms SMP race was stopped at 80 games (-13 +/- 77,
  too few to see the ~+60 that 0.6 plies under load predicts). champion.json
  carries time_ms 10 and depth 1; scripts/screen.sh defaults to 10 ms.
- [x] **BUG, found by the first 10 ms test**: the search read the clock every
  2048 nodes, three to five milliseconds of work, so a 10 ms move took 21 ms
  (213%) against a Stockfish that keeps to its movetime. The mask now follows
  the budget (2047 / 511 / 63 / 15); 10 ms moves take 109%, 1 ms moves 119%.
  The first 10 ms calibration ran on the old binary and was discarded.
  Commit 7c5a1ab.
- [x] **10 ms calibrations landed** (fixed binary, 30 games a level,
  Stockfish on the same 10 ms): one thread 1928, four threads 1993 (one
  game at a time, so the threads had cores), and 1 ms one thread 1852. The
  engine that reads 2347 at 1 s reads 1928 at 10 ms: three clocks, three
  rulers, no conversion between them. champion.json now carries 1928.
- [x] **The 10 ms ruler is soft.** The levels disagree far beyond their
  noise: 1612 against SF 1600, 1892 against SF 2000, 2080 against SF 2200
  with one thread; 1670 / 1788 / 1839 on 1600 / 1800 / 2000 with four.
  Stockfish's UCI_Elo is calibrated for long clocks, so at 10 ms its upper
  levels are weaker than their label and every game against them flatters.
  Same-engine races at 10 ms compare like with like and stay sound; the
  absolute figure only compares to other 10 ms calibrations on the same
  levels. At 1 ms the spread is 1505 to 2359.
- [x] **DECIDED (taken here, owner may overrule).** The goal keeps its 1 s
  wording, "2700 on the same-clock ladder at 1 s/move". Every screen, race
  and feature decision runs at 10 ms, and one 1 s calibration gets paid per
  adopted champion. The alternative, restating the goal as +350 over the
  1928 baseline, was rejected: a target on a ruler whose levels disagree by
  470 is a target on the ruler's error.
- [ ] Calibrate four threads at 1 s, the goal's own number, about an hour;
  run it when the machine is otherwise idle.
- [x] **SMP at 10 ms: four threads beat one by +70 +/- 39** (160-64-96,
  320 games, SPRT settled "better" at llr +3.24), both behind UCI, two
  concurrent games, openings 145000+. First Elo figure for the parallel
  search; the depth measurements predicted about +60.
- [x] **BUG in the harness, fixed**: the gauntlet caps concurrent games with
  GOMAXPROCS and exec handed that variable to every UCI child, so a
  four-thread engine under GOMAXPROCS=2 ran four searchers on two
  processors. The 100 ms race (-13 +/- 77) was measured that way. The child
  now gets the environment without it; test starts a fake engine under
  GOMAXPROCS=2 and reads what it saw. Commit dc5c8a6.
- [x] **SMP confirmed and adopted**: on openings no race had used (147000+)
  four threads beat one by +147 +/- 69 (73-22-25, SPRT settled at 120
  games). champion.json now carries threads 4 and elo 1993, the four-thread
  10 ms calibration. Safe for the ladder: generate() builds its player from
  Strong(0) and the labeller scores through PlayerScoreWith, which reads
  Depth only, so neither the clock nor the threads in champion.json reach
  training. The UI server on 8765 was left running (a game may be on); a
  four-thread server is up on 8766.
- [x] **Clocked pruning screens at 10 ms, 200 games each**, both sides single
  threaded: rfp +2 +/- 48, scaledlmr +24 +/- 49, nullgate -10 +/- 48,
  iir -28 +/- 49, countermove +5 +/- 48. None clear of zero; at 10 ms a
  200-game screen costs 30 s and resolves +/- 48, so the cap, not the SPRT,
  stopped every one of them. Only scaledlmr is worth more games.
- [x] scaledlmr at 10 ms with more games: -21 +/- 26 (242-155-283), SPRT
  settled "worse" at 680 games. Rejected; the +24 at 200 games was noise.
  All five clocked pruning screens are closed at 10 ms.
- [x] **Second hidden layer built** (delegated, then verified here: go test
  and pytest green, tree clean): 128 -> 32 -> 1 behind `--hidden2 N`, the
  JSON carries h2 / wh2 / bh2, single-layer nets serialise and evaluate
  bit-for-bit as before (seven positions recorded before the change, and
  the test goes red on a mere reordering of the float sums), Go-agreement
  test covers the new layer, coverage 100% on every touched function, Python
  100%. Commits c97fcde, 94e5de9, 8a58bd7, aca7410. Cost: head() is 2H*H2
  multiply-adds, 4096 against 128; the single-layer path is unchanged at
  85-92 ns/op.
- [x] **Trained and raced: the layer does not help.** On clean_r9.bin with
  the ladder's flags plus --hidden2 32, cold start, best epoch 53 of 75:
  explains 89.3% against 90.2% for the single-layer rung 9 net. At 10 ms,
  both sides single-threaded, openings 110000+: **-90 +/- 46** (73-33-134,
  SPRT settled "worse" at 240 games). Not adopted. The residual is not head
  capacity; see the rung 2 table below for the same wall from nine angles.
- [x] **BUG on the way, fixed with a test**: the first race of that net ran
  on a gauntlet-bin built before the second-layer loader and reported
  "chunk failed" as the screen. Third stale binary of the evening. The
  match driver now builds gauntlet-bin like it built sprtcheck-bin; the
  test runs it on zero games and checks the binary is fresher than the
  run (red on the old script: 22:00:28 against a run at 22:00:29).
  Commit 3b739a1. Both ladders now build nnue-bin first as well, and stop
  on zero rungs (BSD `seq 1 0` prints 1 and 0, so zero rungs used to mean
  two); test red at 21:57:23 against 22:22:42, then green. Commit e81f808.
  Still by hand, in scratch scripts only: uci-bin behind the UCI wrappers
  and calibrate-bin in the calibration scripts (`go build -o uci-bin ./uci`,
  `go build -o calibrate-bin ./calibrate` before a race or a calibration).
- [~] **Coverage to 100%** in engine and nnue, delegated: tests with teeth
  only, no production change beyond deleting provably dead statements,
  timing tests gated as the existing ones are. Last tally engine 190/212
  functions, nnue 28/47, total 97.2%.
- [x] **Same net at a learning rate ten times smaller** (0.0005 against the
  ladder's 0.005), by request, cold start, same flags otherwise: 168 epochs
  to the same 89.3% explained, and **-106 +/- 51** at 10 ms (53-35-112,
  SPRT "worse" at 200 games, openings 114000+). Not adopted. The smaller
  step reaches the same floor more slowly; the floor is the data, not the
  optimiser.
- [x] The layer did not help, so no spread() short-circuit and no ladder
  retrain with it: the network is not the ceiling. What is left on the
  evaluation side is the training signal itself (deeper labels, more
  tactical positions kept), not the head.
- [x] **A second hidden layer.** Built, trained and raced above: 89.3%
  explained, -90 +/- 46 at 10 ms, not adopted. The lr 0.0005 rerun is the
  last word on it. The reasoning that motivated it, kept for the record:
  the one architecture change not yet tried, and the only one that changes
  what the network can express. Width 64 to
  128, king buckets 8 to 32 and averaging two nets all stayed at ~91%
  explained and level in Elo, because each keeps the network a single
  clipped-linear layer that can only add up one opinion per piece. A second
  layer represents interactions between pieces, which is the shape tactics
  take, and is what real NNUE uses (256x2 -> 32 -> 32 -> 1). Go needs the
  layer in output() only; the incremental accumulator is untouched. Trainer,
  export and load carry it; the Go-agreement test extends to it. About two
  hours. Measured first as explains against the 91% ceiling, then as Elo.
- [~] **First search result: the quiescence ply cap.** The capture search
  stops at four plies (`const maxQuiescePly = 4`) and returns the stand-pat
  score mid-exchange, in check included. It already has a `-qply` flag, so
  it screened with no code change, 400 games each at 10 ms on four bands,
  after a self-test at +14 +/- 49. Caps 6 / 8 / 12 / 16 read -10 / **+30** /
  +10 / +21, all +/- 34. A signal, not a result: rerun 8 and 16 at 2000
  games on unused bands before adopting anything. The gain does not grow
  with the cap, so what helps is resolving the exchange at all, not chasing
  it far.
- [ ] The three quiescence findings that SAVE nodes should screen better
  at 10 ms than the cap did, since the engine only reaches four plies here:
  probe and store the table inside quiescence (28% of quiescence nodes are
  claimed re-visits), use the real `see()` in `engine/see.go` instead of the
  hand-rolled most-valuable-victim heuristic in the capture loop, and delta
  pruning. A test for the first is written and parked in the session
  scratchpad as `quiescett_test.go.prepared`: the score must not move and
  quiescence nodes must drop at least 5%.
- [~] **The search is now the only lever left, so it gets a proper survey**:
  five parallel investigations (move ordering, pruning parameterisation,
  quiescence and the horizon, extensions plus table plus time management,
  and whether the harness itself is hiding gains), each proposal then given
  to an independent agent told to refute it. What survives becomes the
  measurement queue. No races or trainings run during it.
- [ ] Then the tablebase re-measure, pin-based legality and bitboards.

## Rung 2 against rung 1, 2026-09-11: nine approaches, one wall

Goal: a rung 2 that beats rung 1. Every race below is the rung-2 candidate
against rung 1 at fixed depth 2, paired openings, on bands no screen used.

| approach | mechanism it tests | result |
|---|---|---|
| cold start, all pools | baseline | -13 +/- 22 |
| own labels only | stale-pool dilution | -29 +/- 31 |
| warm start from rung 1 | preserve what rung 1 knew | -2 +/- 14, and it converged to the same 91.1% |
| blend 0.15 / 0.30 / 0.60 | hand evaluation damping | -33 / -6 / -3 |
| warm start, 5000 games | resolution | +2 +/- 10 |
| depth-5 labels (90 min) | more new information per rung | -6 +/- 10, explains fell to 90.1% |
| 128 hidden | capacity | -6 +/- 14, 90.3% against 90.1% |
| 32 king buckets (3M relabelled) | feature resolution | +1 +/- 10 |
| rung 1 + rung 2 averaged as one net | independent errors | -0 +/- 10 |

**What the ninth row proves.** If two independently trained networks made
independent mistakes, averaging them would have measured. It did not, so both
fail on the same positions: the 9% a network cannot express is tactical
content that no static function of piece placement can represent. That is
why data (flat from 400k to 3M), capacity, features and initialisation all
left the ceiling at about 91%, and why a teacher that is better by +17 hands
its student nothing: the +17 lives mostly in exactly that content.

**Why rung 1 worked and rung 2 cannot.** Rung 1 replaced a hand-written
evaluation with a network, a change of function class, and gained +17 +/- 12.
Rung 2 replaces a network with another network of the same class. There is no
upgrade left to make, so the ladder saturates after one rung with this
architecture, as the production ladder did at +11.

**What would be a change of kind rather than another knob.** A deeper network
(a second hidden layer, which the Go accumulator does not have), inputs that
are not static (attack maps, which cost search time at every leaf), or a
different loop entirely. None of those is an afternoon, and none was
measured, so this is where the arbitration belongs with the owner.

## The ladder from scratch, 2026-09-11: one step, then flat

Started from the hand-written evaluation with no network at all, to see the
loop work where the gains are large enough to measure cheaply.

| rung | trained on | result |
|---|---|---|
| 1 | 3M positions labelled by the hand evaluation at depth 3 | **+17 +/- 12** over 3500 games, adopted |
| 2 | rung 1's labels plus rung 1's pools | +5 +/- 18 screen, **-13 +/- 22** confirm, rejected |
| 2 | rung 1's labels alone, no stale pool | -29 +/- 31 screen, rejected |

**The first rung is real and the second is not.** The first converts three
plies of search into the static evaluation, which is a one-off gain. The
second would have to convert three more, and cannot: a network explains about
91% of its teacher whatever you do, and the 9% it cannot represent costs more
than the extra plies are worth.

Three explanations were tested and died, so none of them needs testing again:

- **Volume.** 400k, 1M and 3M positions explain 91.5%, 90.3% and 91.3%. Flat.
  The missing 9% is tactics a static function cannot represent, not missing
  data.
- **Speed.** At 100ms the engine reaches the same depth with the network and
  without it (6/5/9 plies on three test positions), so evaluating a network
  costs nothing measurable.
- **Dilution.** Rung 2 trained on its own labels alone, with no hand-evaluation
  pool, read -29 +/- 31 and explained 85.1% against 91.0%. Dropping the stale
  pool made it worse.

**What did decide it was the depth the labels came from.** A network taught by
a depth-3 search helps a shallower search and hurts a deeper one, because a
deeper search already computes what the network was taught and the network can
then only add its own error. One network, one opponent:

| playing search | result |
|---|---|
| fixed depth 1 | +14 +/- 40 |
| fixed depth 2 | +17 +/- 12 (3500 games) |
| fixed depth 4 | +16 +/- 40 |
| 100ms, which reaches 5 to 9 plies | -36 to -66 |

The first attempt at this ladder labelled at depth 3 and raced at 100ms, so it
asked a seven-ply search to learn from a three-ply teacher and read -57.
`scripts/ladder_scratch.sh` now refuses to start when the label depth is not
deeper than the play depth.

**To climb past one rung** the labels must get deeper every time, so that what
is new exceeds the 9% lost in translation. Labelling cost grows with depth far
faster than the gain does: depth 5 labels run at about a fiftieth of depth 3.
That is the wall, and it is the same one the production ladder hit at +11.

## Open, as of 2026-09-11 10:30

Ordered by expected yield per hour. Everything above this line is closed.

- [x] BUG: every reference-side switch (-futility, -ref-no-castle, -ref-no-repetition, -ref-nullmove-ep-bug) was assigned before the -ref-champion replacement wiped it, so three flags that exist only to configure the reference did nothing in the one mode the ladder runs in. Now applied in one place after the replacements. Commit a4940c9.
- [x] BUG: deploying the clock put time_ms in champion.json, and the reference inherited it while the challenger ran at fixed depth. Same net both sides read -552 +/- 149; -2 +/- 28 after. The harness now owns the clock the way it already owned the depth. Commit b76b15f. Live forty minutes, touched only the self-test that found it; no published number is affected.
- [ ] **Run rung 10.** `scripts/ladder.sh` is fixed (pure search labels, screen
  then confirm on unused openings). Rung 9 is the teacher now, and the clean
  corpus recipe is the one that worked.
- [ ] **Screens must move to the clock.** The engine ships at 1s/move and every
  screen in this file was run at fixed depth 4. A pruning rule that costs
  accuracy to save nodes is free at a fixed depth and valuable under a clock,
  so some of the rejections above may not survive the change of question.
- [ ] **Re-tune the pruning fitted through the broken table**: null-move
  reduction (fixed at 3 plies, never verified), LMR scaling, futility margins.
- [ ] **Move ordering**: countermoves, history aging, SEE-ordered captures.
  Exit: fewer nodes to the same depth, then Elo.
- [ ] **Endgames**: re-measure the tablebase probe, which read -13 +/- 18
  through the colour-indexing bug.
- [ ] **Opening book from the PGN database** (match history is allowed, scores
  are not).
- [ ] **Speed I**: pin-based legality, no make/unmake per candidate when in
  check or pinned. Exit: identical node count, less time.
- [ ] **Speed II**: bitboards. Days of work, last.
- [ ] **Coverage to 100%** (engine 190/212 functions, nnue 28/47; Python is
  there already).

Deep Blue is about 350 points away on the instrument that now ships.

## Making the self-play ladder climb, 2026-09-11

**The ladder was never broken. The ruler was.** engine.Strong sets
Mobility true. The gauntlet declared -mobility false, and the
-ref-champion reference is rebuilt after the challenger is configured, so
it kept Strong's true while every challenger played without the mobility
term. The same network on both sides read **-18 +/- 22** over 1000 games;
with the default taken from engine.Strong it reads **-2 +/- 16** over
1750.

That bias is the whole of what seven rungs measured as being worse than
the champion that taught them. Six networks from six recipes raced the
champion tonight and read -20, -2, -9, -15, -27 and -40, mean -19, one of
them trained on the champion's exact corpus with the champion's exact
recipe. And the 18 +/- 16 gap between two identically-trained networks,
which repeated on openings the first race never used and looked like real
run-to-run variance, was this same bias: one of them was the challenger
and the other the reference.

Run the self-test before trusting any race: the champion against itself
must read zero, and 300 games (+/- 40) is not enough to see an 18-Elo
handicap.

On the fixed ruler, over 2000 games at depth 4 against the champion:

| net | before the fix | after |
|---|---|---|
| r9_mse | -20 +/- 25 | **+7 +/- 15** |
| r9_twin | -2 +/- 22 | **+2 +/- 15** |
| r9_avg (averaged weights) | -15 +/- 25 | -16 +/- 25 |

A rung is now level with the champion that taught it, or a little ahead,
where it used to be reproducibly twenty behind. Neither clears its own
margin yet, so nothing is adopted.

Averaging the best eight epochs is a separate finding and a negative one:
it improves the held-out loss, 1.4776 against 1.4821, and loses games,
-16 +/- 18 pooled over 1500. Held-out squared error is fitted on one
number per position and a game depends on the ranking of moves, so a
smoother function can predict better and choose worse. The option stays
in the trainer; the ladder does not use it.

Two levers closed by measurement, one opened.

**The game-outcome term was noise, and the ladder had been using it.**
Every pool this project owns was imported with the default lambda 0.8, so
a fifth of every target was the game result rather than the search score.
The sweep (1000 games each at depth 4, paired openings, SPRT early stop):

| labels | vs lambda 1.0 | vs champion |
|---|---|---|
| lambda 0.5 | -52 +/- 44 | -79 +/- 44 |
| lambda 0.8 (the default) | -29 +/- 31 | -43 +/- 31 |
| lambda 1.0 | reference | -31 +/- 31 |

Monotone: every part of the game result mixed in costs Elo. Between
engines this strong the outcome of a game is too noisy a statement about
a position to be worth the variance it adds. Ladder rungs must be built
with `-lambda 1`. A guard test now proves lambda reaches the label as the
game's actual result, since a dead flag has already cost this project
seven rungs.

**The 0.45 hand blend is not diluting the network.** The champion's own
net raced against the champion at other blends: 0.0 read -55 +/- 44, 0.2
and 0.3 both -33 +/- 31, 0.6 read -17 +/- 25. Nothing beats 0.45, so the
blend is not where the ladder loses.

**What is left is the fit itself.** A rung is a network fitted to its
teacher's search score; at 85.8% of variance explained the residual is
roughly half a pawn at every leaf, and search amplifies it by taking
maxima over noisy leaves. So the lever is the residual where it matters,
and squared error in pawns spends the network on positions decided by
eight pawns. Training now compares win probabilities instead
(`--loss sigmoid`), which is what Stockfish does. The Go trainer had
measured the idea correct and dropped it because its hand-written
optimiser could not follow a gradient twenty times smaller; Adam divides
by that gradient's own magnitude, so the objection does not carry.
The first attempt at that comparison measured the wrong thing, and the
way it failed is worth keeping. The win-probability run stopped at epoch
24 where its control ran to 62, and read -26 +/- 25 against it. The cause
was `--min-delta`: an absolute improvement threshold, against a
constant-predictor loss of 15.099 for pawns and 0.0446 for win
probability. The same number was 340 times stricter on one arm than the
other, so the threshold decided how long each network trained. It is now
a fraction of that loss, the rule is one named function, and its test is
the scale invariance the absolute form fails. Both arms are being
retrained under it.

Retrained under the scale-free rule, the loss is the only difference and
the answer is no. The control reproduced itself exactly, which is what
makes the rest of the row trustworthy:

| net | explains | jump | vs the mse arm | vs champion |
|---|---|---|---|---|
| r9_mse (squared error in pawns) | 90.2% | 0.399 | reference | -20 +/- 25 |
| r9_sig (win probability) | 88.4% | 0.411 | -37 +/- 31 | -40 +/- 31 |
| r9_h128 (128 hidden units) | see below | | -27 +/- 25 | -27 +/- 31 |

r9_mse lands on -20 +/- 25 against the champion, against ladder rung 8's
-20 +/- 18 on the same pools: the ladder is reproducibly stuck at minus
twenty, and it is not the loss that puts it there. The win-probability
arm also plateaus at epoch 26 against 60 under a threshold that is now
fair to both, so it converges sooner and to a worse network.

Doubling the network closed the same way: 128 hidden units, the most the
Go accumulator allows without a change, read -27 +/- 25 against the
64-unit arm and -27 +/- 31 against the champion.

So five levers are now closed by measurement: label depth, the
game-outcome term, the hand blend, the loss, and capacity. Every arm
lands between -20 and -40 against the champion whatever is changed, which
is the signature of the champion being special rather than of the changes
mattering. champion_net.json is byte-for-byte nets_torch/ladder_r6.json,
so it is an ordinary product of this recipe on strictly less data than
the arms that lose to it. Two candidates remain: the two newest pools
hurt, or rung 6 was adopted because it won a race and sits in the upper
tail of the training distribution, in which case every rung since has
been asked to beat a lucky draw. Running now: the same recipe on exactly
the champion's corpus, and a second draw of the same recipe to measure
what the trainer's run-to-run spread is worth in Elo.

## Task list, 2026-09-10 evening: measurement fixed, engine profiled

- [x] Draw adjudication calibrated to the engine's scale (band 0.35, 10 plies): red on 0.10/16, green now, 25% plies saved, score unchanged. Commit a2dac9f.
- [x] screen.sh runs 50-game chunks, so a screen prints every ~8 min instead of going dark.
- [x] Harness self-test: champion vs itself, 300 games at depth 4, reads -13 +/- 40. Zero is inside, the ladder numbers stand.
- [x] The 178-minute screen explained: load 21 on 10 cores from my concurrent test runs. Alone, a 50-game chunk at 1 s takes 7m41s, so 200 games is ~31 min.
- [x] Profiled depth 5: 55% of CPU in legal-move generation, quiesce 50% cumulative, decodePiece 9% on a division.
- [x] Quiescence generates only captures, en passant, promotions (evasions in check) and still detects stalemate; decodePiece is a table. Commit 535072d.
- [x] Quiesce captures ordered MVV-LVA: 403138 -> 348397 fixed-depth nodes (-13.6%). Commit 5d0408f.
- [x] Screen `lmp over see`, 200 games at 1 s: **-70 +/- 50** (55-50-95). LMP on top of SEE ordering hurts under the clock; SEE ordering stays alone. Log /tmp/chesslogs/screen_lmp_over_see_1000ms.log.
- [x] UCI front-end (engine/uciserver.go, uci/, gauntlet -uci and -ref-uci): two builds can now meet head-to-head. Commit b8c65f8.
- [x] Speed, near-idle machine, depth-5 bench alternating old/new three times: old 133.1/133.4/133.3 ms, new 119.9/119.6/121.3 ms, **1.11x**. At EBF ~7 that is +0.05 ply, so about +7 Elo expected: below what 200 games (+/- 48) can resolve.
- [x] First race attempt ran one move at a time: one UCI process per side shared by ten workers behind a mutex (two processes at 99% CPU, load 4 on 10 cores). UCIEngine is now a pool, one process per concurrent caller; four callers on a 1 s fake finish in 2.4 s. Commits c6f9207, 7aa6069.
- [x] Elo of the speedups (answered on the next line, +5 +/- 48): new build vs a2dac9f, both behind UCI, 200 games at 1 s, paired openings. Relaunched 21:57 with 19 engine processes, load 11-20. Expected about +7, below the +/- 48 a 200-game race resolves; the race is mostly the pipeline's proof. Log /tmp/chesslogs/race_new_vs_old_1000ms.log.
- [x] Race 1 final (speedups vs a2dac9f, 1 s, 200 games): 9-185-6, +5 +/- 48. Inside the margin, as predicted.
- [x] Root loop raised alpha after each move (it never did; the tree below always has): fixed-depth-5 nodes 348397 -> 84416, 4.1x. Naive-minimax reference tests unchanged. Commit bccf92f.
- [x] Races 1 and 2 were INVALID: UCI players returned score 0, and the new draw rule read 0 as level, so every game reaching ply 70 was drawn (185/200, 139/150). Found because HEAD reaches 9.4 plies in 1 s against 6.8 for the old root and still "drew". Fixed: the server sends info score, the client carries it, unscored players return NaN, NaN is no opinion. Commit 4621b48.
- [x] Race 2 redo, real scores: root-window build vs the build before it at 1 s: **60-25-15, +168 +/- 78**, SPRT settled after 100 games. Mean depth in 1 s on the same load: 6.8 -> 9.4 plies. Log /tmp/chesslogs/race_root_vs_prev_1000ms_v2.log.
- [x] Aborted iteration no longer thrown away: a move completed one ply deeper that beat the standing choice is played. Node-count abort hook, 190 cut-off points, 31 justified switches. Commit follows bccf92f.
- [x] The 55% early-stop rule dropped: budget use 87% -> 107% (deadline at 105%). Commit follows 792dc1f.
- [x] Race 3 redo: partial-iteration + full-budget build vs root-window build at 1 s: 200 games 81-60-59 (+38 +/- 49), 200 more on fresh openings 73-75-52 (+37 +/- 49); **pooled 400: 154-135-111, +37 +/- 34**. Clears its own margin and the lower bound is +3, so it stays. Log /tmp/chesslogs/race_partial_vs_root_1000ms_v2.log.
- [x] Calibration of HEAD at 1 s/move, Stockfish on the same clock: **2281** (weighted; rungs read 2147 at SF 2000, 2235 at 2200, 2480 at 2600, so the instrument itself spreads +/- 170). Not comparable with the 2465 of champion.json: that ladder ran Stockfish at a fixed shallow depth per rung (4 to 11), which is far below its UCI_Elo label. The movetime instrument is the honest one for "how strong at 1 s". Log /tmp/chesslogs/calib_1000ms.log, appended to calibrations.json.
- [x] DECISION closed 2026-09-11 by measurement: the champion plays on a clock (+346 over fixed depth 6), so the same-clock ladder is the instrument and champion.json carries 2347. Original note: Recommendation: the movetime ladder (both sides on the same clock), i.e. champion.json gets time_ms 1000 and elo 2281 with the instrument named. Log /tmp/chesslogs/race_partial_vs_root_1000ms.log.
- [x] Merged into master (fast-forward, 94 commits), pushed, master is the GitHub default branch. Public at https://github.com/TidyMaze/chess-go under MIT.
- [x] r9_all (clean corpus plus every older pool, 15M positions) vs rung 9: **-5 +/- 19**, screen rejected, no confirmation spent. Adding the older lambda-0.8 pools to the clean corpus does not help, so label quality beats volume here. That reverses the old "volume dominates" finding (400k at -61 against 7.5M at -20), which was measured when every pool carried the same contaminated labels.
- [x] Blend re-measured on the fixed harness: 0.55 reads **-9 +/- 18**, 0.65 reads **-18 +/- 25**. 0.45 stands. Question closed.
- [x] The other pre-fix rejections do not need re-racing: capacity is settled by the training-side 90.3% against 90.2% explained, which no harness bias touches, and lambda is confirmed twice over by r9_all losing to the clean corpus. Magnitudes stay flagged in place.
- [x] Roadmap item 2, the clock: **+346 +/- 115** (80-16-4, SPRT accepted at 100 games) for 1s/move over fixed depth 6, same network both sides. The champion was deployed at depth 6, so the UI had been giving away about 350 Elo. champion.json now carries time_ms 1000 with depth 6 as the floor.
- [x] DECISION closed by measurement, not preference: the same-clock ladder is the instrument, because the deployed champion now plays on a clock. elo is 2281 with margin 170 (that ladder's own spread), replacing the 2476 from the fixed-depth ladder that flattered us.
- [x] Deployed file re-calibrated at 1s/move with Stockfish on the same clock: **2347** (2281 for rung 6 on the same instrument). Heaviest weighted rung is SF 2400, where it scored 0.53 over 30 games and read 2423. champion.json and the README badge now carry it.
- [ ] Next on the Deep Blue roadmap: re-tune the pruning that was fitted through the broken table (null-move reduction, LMR scaling, futility margins), then move ordering, then the two speed items (pin-based legality, bitboards). Deep Blue is about 350 away on this instrument.
- [x] The blend re-measured on the fixed harness: 0.55 reads -9 +/- 18, 0.65 reads -18 +/- 25, so 0.45 stands. Original note: The pre-fix sweep put everything below 0.45, but it handicapped the challenger by about 19 points and 0.6 read -17 +/- 25, which corrects to roughly zero.
- [x] Deliberately not re-raced, with the reason recorded: capacity is settled by the training-side 90.3% against 90.2% explained, which no harness bias touches, and lambda is confirmed twice over by r9_all losing to the clean corpus. Original note: the lambda sweep, capacity at 128, and LMP over SEE. Directions survive the 19-point correction; the sizes in this file do not.
- [ ] Coverage remainder toward 100% (engine 190/212 functions, nnue 28/47).


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
- [x] **Sweep the blend**: done twice, 0.0/0.2/0.3/0.6 and then 0.55/0.65 on the fixed harness. 0.45 stands.
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
| Delta pruning in quiescence search | +49 +/- 32 (460 games SPRT: 215-94-151) | adopted |
| Fused evaluation, one pass not seven | 1.20x faster, identical output | kept |
| Table reuse per game, 24-byte entries | ~1.5x faster in matches | kept |
| Occupied list as indices, no board clone | 1.28x total at depth 7 | kept |

Nothing here is confirmed at 1%. Several land around +20, which is
exactly the size that 300 games cannot resolve. Delta pruning is adopted (+49 Elo).

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

## The harness is sound, but not at zero opening plies, 2026-09-13

Item 7 wants the opening book measured over 1000 games. A book only
applies from the start of the game, so the obvious setup is
`-opening-plies 0`. That setup turns out to bias the harness by about as
much as the book could ever be worth, which was found by racing the
champion against itself and expecting nothing:

| opening plies | hand blend | games | Elo, same player both sides |
|---|---|---|---|
| 0 | 0 | 120 | -41 +/- 63 |
| 0 | 0 | 200 | -44 +/- 49 |
| 0 | 0.45 | 200 | -38 +/- 49 |
| 6 (default) | 0.45 | 200 | **-10 +/- 48** |

At the default the null control sits on zero, so **the harness is sound as
normally used and every race run through it stands**, including this
session's depth-8 and depth-3 numbers, which used the default. At zero
plies it reads about forty Elo against a player that is literally itself.
Starting every game from the same position leaves the games correlated and
the colour advantage uncancelled, which is exactly what the random plies
are there to do.

The blend was the first suspect and it is not the cause: `-blend` defaults
to 0 while the champion carries `hand_blend: 0.45`, so a challenger given
`-halfkp` really does evaluate differently from a `-ref-champion`
reference. Matching it moved the number by six Elo, inside the noise. It
is still worth passing, but it is not this.

**So item 7 cannot be closed the obvious way.** The artifact is larger
than the +10 to +30 the book is expected to be worth. A sound version
needs openings that are themselves in the book and varied, through
`-match-openings`, rather than no openings at all. The book's measured
case remains what it always was: clock, 16 plies played instantly, and now
also move quality, where `cmd/bookcheck` says the rebuilt book loses 0.103
pawns a move against 0.116.

## Deep labels at 400k: the caveat is closed, 2026-09-13

The depth-8 result at 60000 positions left one thing open in its own
words: at that size every network is data-starved, so it could not rule
out that depth-8 labels pay off at a volume where the network is not
starved. This session paid for that volume, 400153 positions at depth 8,
about two hours of labelling at 32 positions a second.

The first race was against the champion, which confounds label depth with
volume, since the champion learned from millions of positions and this
network from 400k. So a control was labelled at depth 3 to exactly the
same size and trained and raced identically. Both at 200 ms a move, single
threaded, SPRT stopped each after one chunk:

| labels, 400k | labelling time | explains | W-D-L | Elo vs champion |
|---|---|---|---|---|
| depth 8 | ~2 hours (32 pos/s) | 80.7% | 17-13-70 | -205 +/- 83 |
| depth 3 | ~3.5 min (1876 pos/s) | 85.1% | 13-19-68 | -215 +/- 85 |

Ten Elo apart with margins of eighty odd: indistinguishable. **At equal
volume the depth of the label makes no measurable difference, and depth 8
costs about sixty times more to produce.** That is the clean answer the
60000 position run could not give, and it closes the caveat rather than
leaving it open on a technicality.

Two things worth keeping from it. The binding constraint is volume, not
label quality: both networks land near -210 because 400k is a small
corpus, whatever is written in it. And held-out variance did not predict
strength again, the depth-3 network explains 4.4 points more and plays ten
Elo worse, well inside the noise; the same thing was already observed with
smoothness at 60000 positions.

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

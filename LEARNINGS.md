# What this engine has learned

Written 2026-09-12. This is the distilled version: what was measured, what it
cost to find out, and what follows. `NEXT_STEPS.md` is the raw chronological
log and keeps every detail this file compresses.

Every number below came out of a command. Nothing here is an estimate unless
it says so.

## Where the engine stands

| ruler | figure | what it means |
|---|---|---|
| Same-clock Stockfish, 1 s a move | 2347 | the goal's own instrument, 2700 is the target |
| Same-clock Stockfish, 10 ms, four threads | 1993 | the working instrument, minutes instead of hours |
| Same-clock Stockfish, 10 ms, one thread | 1928 | what four threads are measured against |
| Same-clock Stockfish, 1 ms, one thread | 1852 | the floor, kept for the record |
| Fixed-depth Stockfish ladder | 2465 | retired, it flatters by about 180 |

The deployed champion is a HalfKP network blended `0.45 * hand + 0.55 * net`,
playing 10 ms a move on four threads (`champion.json`).

Mean completed depth over five middlegame positions, by clock and thread
count. This is the number that matters, not nodes:

| clock | 1 thread | 2 | 4 | 8 |
|---|---|---|---|---|
| 1 ms | 1.8 | 2.0 | 2.2 | 2.2 |
| 10 ms | 4.0 | 4.2 | 4.6 | 4.8 |
| 100 ms | 6.2 | 6.4 | 7.2 | 7.4 |
| 300 ms | 7.8 | 8.2 | 8.8 | 9.0 |
| 1 s | 10.6 | 11.0 | 11.8 | 11.8 |

Four threads buy about one ply at every clock, which is why short clocks are
sound for relative questions. Eight threads add nothing on a machine with
four performance cores.

## The central finding: the evaluation is the ceiling and cannot be raised

A network fitted to the engine's own search beat the hand evaluation it
learned from by **+17 +/- 12**. That was the whole gain. Every attempt at a
second rung landed level, from nine different directions:

| approach | what it tested | result |
|---|---|---|
| cold start, all pools | baseline | -13 +/- 22 |
| own labels only | stale-pool dilution | -29 +/- 31 |
| warm start from rung 1 | keep what rung 1 knew | -2 +/- 14 |
| blend 0.15 / 0.30 / 0.60 | hand evaluation damping | -33 / -6 / -3 |
| depth-5 labels | teacher quality | level |
| 128 hidden units | width | level (re-tested 2026-09-17, -28 +/- 20) |
| 32 king buckets | input resolution | level |
| averaging two nets | noise reduction | -0 |
| second hidden layer, 128 to 32 to 1 | function class | -90 +/- 46 |
| same layer at a tenth the learning rate | optimiser step | -106 +/- 51 |

The averaging result is the proof. Independent errors average away; these did
not. So the roughly 9% of the teacher's score a network cannot reproduce is
**systematic tactics, not noise**, and a teacher better by +17 hands its
student nothing. Rung 1 changed function class, from a hand-written sum to a
learned one. Rung 2 has no function class left to change, and the second
hidden layer proved that adding one does not help: it reached 89.3% explained
against 90.2% for the single layer and lost Elo at two learning rates.

"Explains" is R squared on held-out games, `100 * (1 - test / baseline)`
(`pytorch/train.py:659`), where the baseline is the error of predicting the
mean for every position. It only compares within one loss function. Elo is
the cross-loss yardstick.

**What follows:** the remaining Elo is in the search. Not in the network, not
in its architecture, and not in its optimiser.

**Corrected 2026-09-16, with a measurement this section never had.** Play
both engines at the *same fixed depth*, no clock, so speed cannot enter:

| depth | against Stockfish |
|---|---|
| 4 | -168 +/- 54 |
| 6 | -215 +/- 58 |

At equal nominal depth we are about 200 Elo behind and the gap widens with
depth. Against the -343 +/- 33 measured at 10 ms a move, that splits the
deficit into roughly 200 Elo of quality per node and 140 of plies not
reached. The nine closed experiments above are still closed, but what they
closed is *this way of training*: every one of them fits the network to the
engine's own search score, so the teacher is the ceiling and a wider student
cannot pass it. The quality per node is not a search problem.

Supporting numbers, same position, one thread:

    ours       2922605 nodes to depth 13, branching factor 3.14, ~4M nps
    stockfish    76043 nodes to depth 14, branching factor 2.23, ~200k nps

We are roughly twenty times faster per node and spend thirty-eight times
more nodes to see no further. And ordering is not the cause: 90.3% of beta
cutoffs already land on the first move searched, 96.9% by the second.

## What actually won Elo

| change | measured | note |
|---|---|---|
| Playing on a clock instead of a fixed depth | +346 +/- 115 | the single biggest change ever made here |
| Raising alpha at the root after each move | +168 +/- 78 | 4.1x fewer nodes, invisible to every fixed-depth test |
| Four search threads (Lazy SMP) | +70 +/- 39, then +147 +/- 69 | screen then confirm on unused openings |
| Delta pruning in quiescence search | +49 +/- 32 | SPRT 460 games, adopted |
| Keeping the iteration the clock cut off | +37 +/- 34 | when it beat the standing move on an exact score |
| The first network rung | +17 +/- 12 | and the last one |

Two of the top three were bugs, not features. That is the pattern worth
remembering.

## What did not win Elo

Screened at 10 ms, 200 to 680 games each, both sides single-threaded:

| feature | result |
|---|---|
| reverse futility pruning | +2 +/- 48 |
| scaled late move reductions | -21 +/- 26, sequential test settled "worse" |
| null-move gating | -10 +/- 48 |
| internal iterative reductions | -28 +/- 49 |
| countermoves | +5 +/- 48 |

Also rejected earlier: late move pruning chosen over static exchange
evaluation (-70 +/- 50), tuning against another engine rather than game
results, and every evaluation rung past the first.

A caveat that has not been settled: a 200-game screen at 10 ms resolves about
+/- 48, so none of those five was measured well enough to be called worthless.
They were measured well enough to be called "not worth more games yet".

**Quiet checks at the quiescence root, made and unmade from the full legal
list: -113 +/- 51 at 10 ms, SPRT settled "worse" after 200 games.** The
loss audit pointed straight at it (261 of 267 decided games lost to a
tactic one ply past the horizon, mostly after a quiet move), and the
generator's cost still outweighed what it found. The bitboard version,
direct checks only from the king's attack set, was the second attempt;
it read -24 +/- 24 over 800 games, SPRT "worse" again. At 10 ms nothing
added at the quiescence root has paid; the feature was removed.

**Continuation history, one ply (quiet moves ranked by how they did after
the opponent's previous move): +3 +/- 15 over 2000 games at 10 ms, SPRT
never settled.** Neutral. The flag stays off; a two-ply follow-up table is
the only untested variant and the one-ply result makes it unlikely.

**Depth-preferred transposition table with aging: +35 +/- 23 at 100 ms a
move, SPRT settled better over 900 games.** put was always-replace, and
quiescence stores at depth 0 while being about half the nodes searched, so
any leaf colliding with a deep entry threw it away. The table is now
depth-preferred within a search and always-replace across searches, aged
by a generation counter that advances once per move.

This also explains a null result recorded earlier: shrinking the table to
2^18 and 2^16 read -5 and +1, which looked like "table size does not
matter". The cost was never the size, it was the policy, and a smaller
table with a broken policy is broken at both ends.

Measured as two binaries, current against a build of the parent commit.
An earlier attempt raced champion_bot.json against a copy of itself, which
is a null control and reads whatever the noise is: a configuration file
cannot A/B a code change.

## The bugs, and what each one cost

These are the ones worth never repeating.

**The clock was read every 2048 nodes.** Three to five milliseconds of work,
half a 10 ms budget. A 10 ms move took 21 ms, so every short-clock race and
calibration measured the overrun as much as the engine. The check interval now
follows the budget (2047 / 511 / 63 / 15 nodes): 109% at 10 ms, 119% at 1 ms.

**`GOMAXPROCS` was inherited by child engines.** The harness caps concurrent
games with it, and `exec` passed it to every UCI child, so a four-thread
engine under `GOMAXPROCS=2` ran four searchers on two processors. The 100 ms
threading race (-13 +/- 77) was measured that way. The child now gets the
environment without it.

**Stale binaries, three times in one evening.** Two races gave identical
depths for one and four threads; a third raced a second-layer network through
a loader that could not read it and reported "chunk failed" as the result of a
400-game screen. The match driver and both ladders now build what they run,
with a test that checks the binary is fresher than the run.

**The reference and the challenger were not the same engine.** `-mobility`
defaulted false on one side while the engine's own constructor set it true:
the same network on both sides read -18 +/- 22 over 1000 games, and seven
ladder rungs were measured through that. Later the reference switches were
assigned before `-ref-champion` replaced the reference wholesale, so they were
silently discarded. Later still the reference inherited `time_ms` from the
champion file and played a second a move against a fixed-depth challenger:
-552 +/- 149.

**Opening offsets wrap.** The file holds 150,000 openings, so offset 600,000
is offset 0. Several "fresh" bands were contaminated before anyone checked.

**Labels must be deeper than play.** The same depth-3 labels gave +17 when the
engine played at depth 2 and -57 when it played at 100 ms, about seven plies.
A network fitted to a shallower search than the one that will use it can only
add error. The ladder script now refuses the configuration.

**Draw adjudication never fired**, then fired on every game. The first rule
(0.10 pawns for 16 plies) matched nothing; recalibrated from the measured
score distribution to 0.35 for 10 plies. Then UCI players, which had no
opinion to report, scored 0.0 and every game was adjudicated a draw at ply 70.
No opinion is now `NaN`, not zero.

**`seq 1 0` prints "1 0" on BSD.** Asking either ladder for zero rungs ran
rungs 1 and 0.

**A pgrep pattern inside a monitor script matches itself.** The script's own
command line contains the pattern, so `pgrep -f 'train.py --pool x'` never
returns empty and the wait never ends. Write `train[.]py`, or wait on a log
line.

## Probability-space outcome blending is worse, not better

Claimed here earlier the same day, and wrong. The argument was that
blendedTarget caps a win at +/-4 pawns through resultPawns, so a position
the search calls +8 in a won game is labelled +6.8, and that this scale
error was why the lambda sweep read negative rather than the technique
failing. blendedTargetProb blends in probability space instead, where a
win can only raise a target.

Two fresh networks, identical settings, 900 self-play games per
generation, lambda 0.6, differing only in the blend space:

| target | generations | pool | explains | head to head at 100 ms |
|---|---|---|---|---|
| pawn space (blend-k 0) | 29 | 1392460 | 92.1% | reference |
| probability space (blend-k 0.30) | 20 | 1565660 | 87.9% | **-124 +/- 42** |

SPRT settled worse after 300 games. The mechanism is visible in the
numbers the fix itself printed: a position scored +0.3 in a won game
becomes +1.41 in pawn space and +2.30 in probability space. Probability
space moves targets *more*, not less, because at k 0.30 an outcome of 1.0
is an enormous statement, equivalent to a score far past anything the
search reports. At lambda 0.6 that puts 40% weight on a near-saturated
target, and the +/-4 cap that looked like the bug was doing useful work as
a regulariser.

"Explains" also is not comparable across the two: the pawn-space target is
lower variance and easier to fit, which is most of the 92.1% against
87.9%. Only the head to head settles it.

**What stands.** The lambda sweep's conclusion is unchanged and was not
closed on a scale bug: more outcome weight is worse here, in either space.
The -blend-k flag stays at its default of 0, so nothing shipped on the
wrong side of this. What is genuinely still untried is outcome learning
that does not blend a per-game constant into every position at all.

## The gap to Stockfish narrows with the clock

Same binaries, same reference, one thread, 2026-09-17:

| control | gap to Stockfish at 2800 | games |
|---|---|---|
| 10 ms | -354 +/- 34 | 1000 |
| 100 ms | -326 +/- 32 | 1000 |
| 1 s | -216 +/- 41 | 400 |

Roughly +70 Elo for every tenfold increase in the clock, so this engine
converts time into strength faster than Stockfish does when its strength
is capped by UCI_LimitStrength. That limiter is not time-independent, as
the 10 ms to 100 ms pair alone might suggest, but it does flatten.

Two consequences. Any absolute claim has to name its control, since the
same engine reads 2446 and 2584 depending on it. And a measurement taken
at 10 ms understates what a change is worth at the controls the bot
actually plays, which makes 10 ms a screening instrument and not a
verdict.

**Two-entry transposition buckets: rejected on tree size, 833367 nodes
against 733290.** A probe pays a cache miss for a 64-byte line and reads
24 bytes of it, so pairing the slots looked free: the same miss brings two
candidates, and depth-preferred picks which a third evicts. Built it, and
the deterministic fixed-depth tree grew 13.6%.

Halving the number of sets to double the associativity is not free here.
The single-slot depth-preferred table already keeps what matters, and the
second slot mostly delays eviction of entries that were not worth keeping
while costing a bit of index. Reverted rather than raced: a deterministic
instrument that says worse does not need twenty minutes of games to
confirm it.

**Width re-tested in the net-only era, and still dead: -28 +/- 20 over
1200 games.** The original "128 hidden units: level" was measured when the
network carried 55% of the evaluation and the hand terms carried the rest,
so it was worth asking again now that the network carries all of it.

Two nets trained on the same pool, clean_r9.bin, 8000203 positions, 14
epochs, same everything but width:

| width | held-out MSE | explains | head to head at 100 ms |
|---|---|---|---|
| 64 | 1.0785 | 89.1% | reference |
| 128 | 1.0844 | 89.0% | **-28 +/- 20**, SPRT settled worse |

The wider net does not even fit better, which is the part worth keeping:
the network is not capacity-limited, so the residual is not something more
parameters can absorb. It then pays double the accumulator cost per node
for that non-improvement, and the race charges it.

**Check extensions: -2 +/- 16 over 1800 games at 100 ms, neutral.** The
flag existed and had never been in the champion's feature list, and it was
the right shape of thing by the finding below: it spends nodes to see
further along forcing lines rather than making nodes cheaper. It still
reads zero. Extending on every check is too blunt a criterion; what strong
engines extend on is singularity, which this engine does not implement.

## Speed wins vanish as the clock grows, and do not transfer to a stronger opponent

The complete before and after for one session of search and evaluation
work, same reference, both sides on the same clock, one thread:

| control | before | after | gain |
|---|---|---|---|
| 10 ms | -401 +/- 38 | -343 +/- 33 | +58 |
| 100 ms | -329 +/- 32 | -326 +/- 32 | +3 |
| 1 s | -216 +/- 41 | -216 +/- 41 | 0 |

The 1 s pair is 50-79-271 against 51-77-272 over 400 games each: the same
games. And the same two binaries, head to head at 100 ms, read +110 +/- 42
with SPRT settling better.

Two things are being measured and neither is wrong.

**Every win this session was a speed win.** Bitboard feature deltas,
dropping the hand evaluation, a transposition table that stops re-searching
what it threw away: all of them buy nodes, and nodes buy depth. Depth is
worth most when the engine is depth-starved, which at 10 ms it is, reaching
about 6 plies. At 1 s it reaches 13 and another 15% of nodes is worth a
fraction of a ply. So a speed win decays as the clock grows, and this one
decayed to nothing by 1 s.

**Elo is not transitive across dissimilar opponents.** +110 against our own
previous build and +3 against Stockfish, at the same control, on the same
binaries. The games we lose to Stockfish are lost to tactics several plies
past our horizon, and none of these changes moved that horizon far enough
to change their outcome. Against an opponent that shares our blind spots,
the same changes decide many games.

**What follows.** A speed optimisation must be priced at the control it is
meant for, and a head-to-head A/B is an adoption test, not a strength
claim. Anything aimed at closing the distance to a stronger engine has to
change what a node is worth, not how many there are.

## The two rulers disagree, and the strong one has no resolution

This session's search and evaluation work, measured three ways on the same
binaries:

| against | gain |
|---|---|
| the engine as it stood before the session, head to head at 100 ms | +110 +/- 42 |
| Stockfish limited to 2800, 100 ms | +3 +/- 45 |
| Stockfish limited to 2800, 10 ms | +58 +/- 50 |

The head to head is SPRT settled better over 300 games, 165-62-73. The
individual A/Bs that make it up sum to about +80, so they are consistent
with each other and not with the Stockfish number.

Elo is supposed to be transitive, so this is worth stating plainly rather
than averaging away. Against a reference we score 0.13 against, a real
+110 should move the score to about 0.25; it moved from 0.131 to 0.133.
Two things are true at once and both matter:

  - a ruler where the score is 0.13 has almost no resolution, because
    improvements have to change the result of games that are lost to
    tactics far past our horizon, and they do not.
  - gains measured against a near-identical opponent are partly specific
    to that opponent: the same evaluation, the same book, the same blind
    spots on both sides.

**What follows for measurement.** A change is adopted on a head-to-head
A/B, which is sensitive. But the *absolute* claim must come from a
reference near our own strength, where the score sits near 0.5, and
"how far from Stockfish at 2800" is a progress report, not an instrument.
Quoting a sum of A/B wins as though it were absolute Elo is what this
section exists to stop.

Two harness mistakes cost real machine time proving that:

  - racing champion_bot.json against a copy of itself to test a *code*
    change, which is a null control and reads noise.
  - a wrapper script written by a careless `sd` substitution that left
    literal backslashes in `cd dir \&\& exec binary`, so the reference
    never started. The harness scored 300 games as 298 draws and reported
    +2 +/- 39 rather than failing. A reference that never moves must look
    like an error, not like a drawn match.

## How to measure anything here

This section is the expensive part. Ignore it and the numbers lie.

1. **Paired openings, colours reversed.** Without them two engines differing
   in one flag play nearly the same game and 40% of results are draws.
2. **The harness owns the clock, the depth and the thread count.** Never the
   champion file. Every asymmetry bug above came from a side picking up a
   setting the other did not have.
3. **Self-test first.** The same network on both sides must read 0. Three
   separate bugs were caught only by that.
4. **Screen, then confirm on openings the screen never touched.** A change
   adopted on the race that measured it is adopted on a lucky draw.
5. **Sequential testing with bounds [0, 15]** stops a settled race early. A
   feature at -70 crosses the lower bound in a hundred games.
6. **Measure at the depth the engine plays.** A whole-evaluation change read
   -31 +/- 24 at depth 4 and +6 +/- 19 at depth 6. Different signs, same
   network.
7. **Calibrate only for the absolute number, and say how the reference was
   throttled.** At 10 ms the Stockfish ladder is soft: the same engine reads
   1612 against level 1600, 1892 against 2000 and 2080 against 2200, because
   `UCI_Elo` is calibrated for long clocks. Races between two versions of this
   engine at 10 ms are sound; the absolute figure only compares to another
   10 ms figure on the same levels.
8. **Build what you race.**

## Rules that come from the owner, not from measurement

- **No externally computed position scores, ever.** Game databases are fine.
  Stockfish is a ruler, never a teacher. A network trained on another engine's
  evaluations is not this engine learning.
- **Every measurement runs at 10 ms a move**, including the deployed champion
  in the browser UI. Stockfish gets the same 10 ms.
- **Test first, always**, and prove the test red before the fix.
- **Commit after each improvement.**

## Where the goal stands

The goal is **2700 on the same-clock Stockfish ladder at 1 s a move**, from
2347. The gap is about 350.

Decision taken 2026-09-11 and open to being overruled: the goal keeps its 1 s
wording, every screen and race runs at 10 ms, and one 1 s calibration gets
paid per adopted champion. Restating the goal on the 10 ms ruler was rejected
because that ruler's own levels disagree by 470, so the target would be a
target on the ruler's error.

## Next steps

**The first search result, 2026-09-12.** The survey's quiescence dimension
reported that the capture search stops at four plies and returns a static
evaluation mid-exchange. Verified by reading the code: `const maxQuiescePly
= 4` in `engine/search.go`, and the cap returns the stand-pat score even
while in check. The cap already has a harness flag, so it screened with no
code change, 400 games each at 10 ms on four separate opening bands, after a
self-test that read +14 +/- 49 with the same network and cap on both sides.

| cap | result at 10 ms |
|---|---|
| 6 | -10 +/- 34 |
| 8 | **+30 +/- 34** |
| 12 | +10 +/- 34 |
| 16 | +21 +/- 34 |

Three of four are positive and none is clear of zero, so this is a signal,
not a result. Pooled over all 1600 games the four deeper caps score above
the default, which is worth confirming properly: rerun 8 and 16 at 2000
games each on unused bands. Note the shape, though: the gain does not grow
with the cap, so what helps is probably resolving the exchange at all rather
than following it far.

A caveat this raises about the 10 ms instrument. The engine reaches four
plies at 10 ms and 10.6 at 1 s, so a change that spends nodes to buy accuracy
is measured at its worst here, and one that saves nodes at its best. The three other quiescence findings were screened: probe the table
inside quiescence (QuiesceTT: -37 +/- 49, rejected), use the real static
exchange evaluation (QuiesceSEE: +10 +/- 48, neutral), and delta pruning
(+49 +/- 32, SPRT accepted at 460 games: 215-94-151, adopted).

**In progress.**

1. **A five-way survey of the search**, since it is the only lever left:
   move ordering and its cutoff rate, why all five screened pruning features
   read zero, quiescence and the horizon, extensions with the transposition
   table and time management, and whether the harness is still hiding gains.
   Each proposal goes to an independent agent told to refute it. What survives
   becomes the measurement queue.
2. **Coverage to 100%** in `engine` and `nnue` (last tally 190/212 and 28/47
   functions, total 97.2%). A first pass left `engine/coverage100_test.go`
   untracked and unverified; it needs running before it is kept or dropped.

**Queued.**

3. Calibrate four threads at 1 s, the goal's own number, about an hour on an
   otherwise idle machine.
4. The tablebase re-measure, pin-based legality, bitboards. Speed is plies and
   plies are Elo, but only after the search itself is right.

**Closed, do not reopen without new evidence.**

- Wider networks, more king buckets, averaging, ensembling, a second hidden
  layer, and lower learning rates. Nine approaches plus two, all level or
  worse.
- Eight search threads on this machine.
- The five pruning features as currently parameterised, unless the survey says
  a parameter was wrong rather than the idea.

## How to run things

```bash
# Screen a feature against the champion at 10 ms
./scripts/screen.sh <feature> 200 10

# A race with pooling, early stopping and fresh openings
START_OFFSET=110000 ./scripts/chunked_match.sh 400 40 -depth 1 -time-ms 10 \
  -halfkp <net.json> -blend 0.45 -ref-champion champion.json -match-openings openings.txt

# Depth per thread count at a given clock
MEASURE=1 SMP_MS=10 go test -count=1 -run TestParallelDepthInOneSecond -v ./engine/

# Coverage, the project's own gate
./scripts/coverage.sh
```

## The champion on lichess, 2026-09-13

A bot bridge (`lichessbot/`, command `cmd/lichessbot`) runs the deployed
champion against real opponents on lichess, as **TidyMazeBot**, a fresh
BOT account made for this purpose (not TidyMaze, the maintainer's own
account with 267 human games). It reads `LICHESS_BOT_TOKEN`, accepts
standard-chess challenges, and answers every move with `PlayerPickWith`
on the champion, the same call every other measurement in this repo
makes. TDD throughout, 100% coverage on the new package, race-clean.

**First results**, unrated and rated 10+5 games against online lichess
bots: beat maia5 (1655), sargon-3ply (1545) and GarboBot (1988), all by
checkmate. lichess's own rapid rating for the account read 3004 after
five games. A game against Lynx_BOT (2671) ran deep into a real
middlegame before this note was written.

**A real bug found live, twice.** Lichess echoes a bot's own outgoing
challenge on the same account event stream used for incoming ones, and
the first fix assumed a `direction` field that does not exist there (it
only appears on the POST response that creates the challenge). The bot
tried to accept its own challenges and logged a 404 for each one. Fixed
by matching `challenger.id` against the bot's own username, the only
signal the stream event actually carries; caught and confirmed against
the real API before committing (commits a7a3dd8, 8435e54 partial fix,
59af7a9 real fix).

**Strong bots (Boris-Trapsky, Elmichess, Lynx_BOT, simpleEval, raspfish,
all 2100 to 3000 rated) mostly declined direct challenges** from a
brand-new provisional account, rated or not; that is their own
acceptance policy, not a bug here. Lynx_BOT was the exception.

**How to run it:**
```bash
LICHESS_BOT_TOKEN=<token with bot:play scope> go run ./cmd/lichessbot \
  -champion champion.json -username tidymazebot
```
The token lives only in the process environment, never in a file in
this repo. Regenerate it at Preferences -> API access tokens on the
TidyMazeBot account if it is ever lost or revoked.

**Building a real ranking, 2026-09-13 afternoon.** 3004 after five games
is provisional and not a ranking; lichess needs a real sample across a
spread of opponent strengths for the number to settle. Queued 23 rated
10+5 challenges across ratings 1200 to 2400 (sargon/bernstein/turochamp
engines, the maia family, davidsguterbot, schnecken_bot, darkonweakbot,
turkjs, charibot, halcyonbot, fathzer-jchess, bottios, croco_little_bot,
and others), sequentially with a few seconds between each so as not to
spam lichess's challenge endpoint. Live status:
`https://lichess.org/@/TidyMazeBot`.

## Endgame technique is the next real gap, 2026-09-13

A live lichess game ([96FKJUSy](https://lichess.org/96FKJUSy)) reached king
and two bishops against king and bishop, a full piece up, and repeated
moves for twenty moves with the halfmove clock climbing from 32 toward
the fifty-move draw. Investigating it turned up something broader than
that one game.

**The engine cannot reliably convert basic won endgames.** Replaying the
champion against itself from textbook positions, at 50 ms a move:

| ending | result |
|---|---|
| king and queen against king | mates |
| king and rook against king | mates |
| king and two bishops against king | never mates, 120 plies |

A first version of this table reported the rook mate as erratic. That was
the harness, not the engine: it built the position with `game.ParseFEN`,
which deliberately leaves repetition tracking off, and the move picker's
only anti-shuffle rule (prefer a move that does not return to a position
already stood in) is dead without it. With tracking on, queen and rook
both mate cleanly and only the two bishops fail. The same gap was real in
the bot bridge and is fixed there.

`engine/endgame_mate_test.go` is that check, gated behind `ENDGAME=1`
because it currently fails. It is a documenting test, not a passing one.

**The mechanism, confirmed by reading the code.** `kingDrivingBonus` is
the one term that drives a won endgame toward mate, and it measures the
bare king's distance from the centre with `centerDistance`, a Chebyshev
distance. That saturates along an entire edge: a1 and a4 both score 3.5.
So once the bare king reaches an edge the term is flat and the search has
no gradient left pointing at a corner, which is exactly where a
two-bishop mate has to deliver. The king sits on the edge and shuffles.

**A second, smaller finding.** The same term only engages once the score
clears 4 pawns. A lone extra minor piece is 3, so a two-bishops-against-
bishop ending (+3) never engaged it at all, which is why that specific
game had no king-approach signal whatsoever.

**What was tried and deliberately not kept.** Replacing the Chebyshev
term with a Manhattan centre distance (which keeps rising toward a
corner) made the two-bishop mate work. Judging whether it hurt anything
else was impossible at the time because the harness had repetition
tracking off, so every measurement it produced moved around between time
budgets. None of it was measured head-to-head, and per this file's own
rules that is not something to adopt, so the evaluation is unchanged and
only the failing test and this note were committed. With a correct
harness the experiment is worth redoing, gated on `gamePhase` so it
cannot touch the middlegame.

**The instrument had to be fixed before any of this could be trusted.**
The first version of the mate check used a wall-clock budget and an
unseeded engine, and it was run while the lichess bot had six games in
flight. Same code, three consecutive runs: pass, fail, pass. Two separate
causes. A time budget makes the depth reached depend on what else the
machine is doing, and the move picker breaks ties between equally-scored
moves at random. The check now uses a fixed depth and `SeedRandom(1)`,
and repeats identically. Anything measured before that was noise,
including two rounds of weight tuning that looked like progress.

**With a trustworthy instrument, the corner term does not survive.**
Sweeping the corner weight at two depths, counting how many of the three
basic mates fail:

| corner weight | depth 4 | depth 6 |
|---|---|---|
| 0.02 | 0 fail | 1 fail |
| 0.20 | 1 fail | 0 fail |
| 0.60 | 1 fail | 1 fail |

No weight works at both depths. A single seeded self-play line is one
sample, and whether a 120-ply shuffle stumbles into mate is close to a
coin flip, so this is a fragile mechanism rather than technique. Nothing
was adopted; the evaluation is unchanged.

**Why it is hard here specifically.** The deployed champion blends
`0.45 * hand + 0.55 * net`, and the network was trained on positions that
are almost never near mate, so in a bare endgame its output is close to
noise. The king-driving term maxes out around 0.85 pawns and has to
compete with that. Fixing endgames properly probably means either a real
mop-up evaluation that overrides the blend once material is nearly gone,
or extending the tablebases past their current `TablebaseMaxPieces = 4`
so these endings are exact rather than estimated. The champion does not
even load tablebases today: `champion.json` has no `syzygy` field.

## An opening book from games, 2026-09-13

The bot's record splits by time control: classical 2W 0L, rapid 7W 1D 7L,
blitz 1W 1D 5L, bullet 2W 0D 4L. Every loss is by checkmate and none on
time, so the cause is search depth, not clock mismanagement. Less time
means shallower search means worse play, and the first move alone was
costing about four percent of a bullet clock.

The deployed bot now plays `champion_bot.json`, the champion plus a book
of 9227 positions. Measured from the start position:

| | opening move | first 24 plies |
|---|---|---|
| without the book | 2.1 s of search | 24 searched |
| with the book | 0 s | 16 from book, 8 searched |

At a 500 ms budget that is 8 seconds of clock handed back per game, spent
in the middlegame instead of on theory thousands of games already agree
about.

**Where the book may and may not come from.** `-book-from-pgn` takes the
most played move per position from the PGN game database: match history,
which the rules allow. `-extract-book` reads the Lichess *evaluation*
dump and takes each move from the deepest engine principal variation.
That is externally computed position scores, which the rules forbid, and
a book built from it would be another engine's opinion wearing this
one's name. The function now carries a warning saying so.

`champion_bot.json` is kept separate from `champion.json` so the
calibration that file carries is not disturbed, and the book is generated
rather than versioned.

## What a move actually costs the clock, settled 2026-09-14

The per move accounting now closes, measured in a rated bullet game at
120+1 with the deployed build:

```
f2g3 in 1.808s search + 22ms post, 1.829s total (budget 1.717s)
```

Budget 1.72 s, search 1.81 s, post 0.02 s, total 1.83 s, and the clock
audit of the whole game puts the median move 0.1 s over its budget. Every
one of those agrees with the others, which answers the thing that was left
open when the overhead reservation was written: whether lichess reaching us
costs anything outside the window this process can time. It does not
measurably. The search overrun, about six percent, is the whole of it.

That also confirms the 550 ms reservation was wrong and specific to games
against the lichess AI, where posting really did take 530 to 680 ms.
Against real opponents a post is 15 to 36 ms whatever the search just did,
including straight after a 15.8 s one. The reservation is 150 ms, which is
several times the real cost and small enough not to matter.

Four consecutive rated games audited clean across three time controls
after the rewrite: 60+3 overhead 0.0 s, 180+2 0.1 s, 600+0 0.3 s, 120+1
0.1 s, none below its floor, none lost on time.

## The book was popular, not good, 2026-09-13

Popularity and quality are not the same thing, and nothing had checked
which one the book had. The case on record for it was entirely clock: 16
plies played instantly instead of searched, about 8 s a game handed back.
Move quality was assumed.

`cmd/bookcheck` asks this engine what it thinks of its own book, never an
outside evaluation: for each sampled position it searches to a fixed
depth, then plays the book move and searches the reply, and the gap is
what the book move costs. Measured at depth 7 over 200 positions:

| source | positions | mean loss | 90th pct | losing >0.5 pawns |
|---|---|---|---|---|
| most played, any rating | 37323 | +0.116 | +0.473 | 8.5% |
| rated 2000+, 20 games | 1847 | +0.112 | +0.357 | 4.0% |
| rated 1800+, 10 games | 16438 | +0.103 | +0.355 | 5.5% |

The median book move costs nothing in all three, so most of the book was
always fine. What a rating floor removes is the tail. The builder now
ignores games below the floor and, among the moves left, prefers the one
that actually scored rather than the one played most often, shrunk toward
a draw by twenty games of prior so a move played the bare minimum of times
cannot win on a lucky run. The deployed book is the 1800 one: 2000 buys
almost nothing more and costs nine tenths of the coverage, and coverage is
the point of a book.

**A correction, since it is the kind of mistake worth keeping.** The worst
entry in the old book looked like it hung a queen for a bishop. It does
not. The bishop pinned that queen to the king, so the queen was lost
whatever White played, and the engine scores the book move a tenth of a
pawn behind its own choice. Reading a FEN and inferring a blunder is not
measuring one.

**Two things that measuring found on the way.** The search picks at random
among moves it scores equally, so two identical searches of the start
position answer g1f3 and then b1c3; any test that pins agreement needs a
position with one legal move. And the book is derived data, 1.5 MB from a
286 MB database, so it is not versioned. It had no recorded build command
either, which made `champion_bot.json` depend on a file that existed on
one machine; `scripts/build_book.sh` is now that recipe.

## Where the bot actually loses, 2026-09-13

Material balance from our own side, measured by replaying all 26 lost
lichess games and scoring the board at fixed checkpoints:

| checkpoint | mean material | games already behind |
|---|---|---|
| ply 20 | +0.65 | 0 of 26 |
| ply 40 | +0.69 | 3 of 26 |
| ply 60 | -2.04 | 14 of 24 |
| ply 80 | -2.82 | 12 of 17 |

Every lost game was still level or better at ply 20, and all but three at
ply 40. The collapse is between ply 40 and ply 60: about three pawns
swing away in twenty plies and the count of games already behind goes
from 3 to 14.

So the losses are not opening preparation, not the clock (all 26 are
mate, none on time), and not endgame conversion, which the engine rarely
gets far enough ahead to attempt. They are the late middlegame, around
moves 20 to 30, where the position is most complex and the piece count is
still high. That is a depth and tactics problem, which is where
LEARNINGS already says the remaining Elo lives, and it is the phase any
further engine work should target. Endgame technique, by contrast, is
worth little here: the engine seldom reaches a won endgame to convert.

**One blunder, not drift.** Taking the same lost games and re-examining
our own moves between ply 36 and 64, with the reference being this same
engine given 700 ms instead of the game's budget, so no outside
evaluation is involved: 2 of 86 moves (2%) are ones where the deeper
search holds a pawn or more, and both are worth about three pawns.

    pOuBop87 ply 37: played Rad8 (-2.0), deeper search plays c7d6 (+1.0)
    fnFTZCQo ply 62: played Ke2 (-1.0), deeper search plays e4g2 (+2.0)

So the three-pawn swing in the aggregate is not many small errors
accumulating, it is roughly one decisive tactical miss every three games.
That is a search depth and pruning-safety question in complex positions,
not an evaluation-weights question. The proxy here is material one ply
after the move, which is crude and misses deeper tactics, so treat the
2% as indicative rather than exact; the shape of the answer, rare and
large rather than frequent and small, is the part worth acting on.

## A lead: the parallel search sometimes plays the blunder, 2026-09-13

Taking the first of the two blunders found above, from a real bullet loss,
position `r4rk1/1pp2ppp/1nqB4/p7/P2QN3/1B4Pb/1PP2P2/R3R1K1 b - -`, where
the game move Rad8 drops about three pawns and c7d6 does not:

| search | result |
|---|---|
| fixed depth 2, 4, 6, 8, 10 | c7d6 every time |
| timed, one thread, 80 ms and 150 ms, three runs each | c7d6 every time |
| timed, eight threads, same budgets, three runs each | Rad8 in 2 of 6 |

So the move is not beyond the engine's depth: a two-ply fixed search
already finds it. A single-threaded timed search finds it too, and
repeats. Only the eight-thread search plays the blunder, and it does so
non-deterministically, which is what helper threads sharing a
transposition table would look like.

Over six positions sampled from the plies-30-to-60 window of five lost
games, at 120 ms: the eight-thread search gave up material against the
single-thread one once and kept more never. Six positions is far too few
to call, and this is recorded as a lead, not a conclusion. What makes it
worth chasing is that it matches the shape of the losses exactly: rare,
large, and in the complex middlegame.

Two things to weigh against each other before acting. LEARNINGS records
Lazy SMP as +70 +/- 39 and then +147 +/- 69 at 10 ms, which is real. But
today's measurement under the load the bot actually runs at, two
concurrent games, gave mean depth 9.0 at both four and eight threads, so
the eight-thread setting buys nothing measurable there while carrying
whatever this is. The next step is a proper head-to-head at equal cores,
not a config change on six positions.

## The parallel blunder is variance, not a defect, 2026-09-13

Chasing the eight-thread blunder above to a cause, four hypotheses were
tested and all four came back negative:

- **Aborted searches poisoning the table.** Already guarded: the search
  returns before storing when it was cut off, with a comment saying that
  a timed search used to leave zeros in the table for the next move to
  believe.
- **A helper's table move displacing the main thread's own principal
  variation at the root.** `orderMoves` takes the preferred move as a
  parameter, which each thread passes its own `best`, and does not probe
  the table itself.
- **Shared evaluation accumulators.** `hev := *ev` does copy a pointer to
  the accumulator stack, but each thread's `searchIterative` re-points its
  own Eval copy at its own context array, so nothing is shared.
- **A data race.** The reproduction, with the real network and eight
  threads, is clean under `-race`.

So it is ordinary Lazy SMP non-determinism: the helpers fill the shared
table, the main thread's search takes a different path each run, and
under a tight clock it occasionally lands on the worse move. That is the
design, not a fault in it.

**And the feature pays for itself.** Eight threads against one, 200 ms a
move, paired openings: **+182 +/- 117 over the first 50 games**
(31-12-7). Which corrects something recorded earlier the same day: the
measurement that four and eight threads both reach mean depth 9.0 under
the bot's two-game load was read as the extra threads buying nothing.
Depth parity is not strength parity. Helpers improve the table and the
move ordering the main thread searches with, so the same nominal depth is
searched better, and a depth count cannot see that. Measure Elo, not
plies, when judging SMP.

## What 75 lichess games say, 2026-09-13 evening

39 wins, 7 draws, 29 losses. Studied properly, three of the obvious
readings turn out to be wrong.

**The mode records are opponent strength, not mode weakness.**

| mode | mean opponent | median | W-D-L |
|---|---|---|---|
| rapid | 1923 | 1829 | 26-2-12 |
| bullet | 2239 | 2124 | 6-3-6 |
| classical | 2226 | 2252 | 5-0-3 |
| blitz | 2404 | 2362 | 2-2-8 |

Blitz looked like the weakest mode by a distance and is simply the
hardest schedule: its opponents average 480 points above rapid's. The
ratings all land near 2200 regardless, which is the consistent reading.
Rapid's good record is easy opposition, blitz's poor one is hard
opposition, and neither is a defect.

**"Every loss came after being two pawns up" was a measurement
artifact.** Scoring material after every ply counts the spike between a
capture and its recapture, which made 29 of 29 losses look like
squandered wins. Counting only an advantage held for ten plies or more
gives 7 of 36 not-won games. Conversion failure is real and is not the
main way games are lost.

**The fifty move rule cuts both ways.** Of the seven draws, two were wins
thrown away at a spent clock, and one was a draw rescued from six pawns
down at a spent clock. So the fix for it has to be symmetric: a losing
side must see the clock running as good news exactly as a winning side
sees it as bad. `TestFiftyMoveFadeHelpsTheLosingSideToo` holds that.

**The clock rule was checked against every control the bot plays** and
none of them flags: bullet 2+1 and 1+0, blitz 5+3, 3+2 and 3+0. Blitz
5+3 over eighty moves ends with 5.9 s in hand. The weak blitz record is
not a time management problem.

## Duplicates in the training corpus cost more than the positions they add

Three networks, same architecture (64 hidden, 8 king buckets), same trainer, raced
against the shipped champion at 100 ms a move:

| corpus | positions | held out | Elo |
| --- | --- | --- | --- |
| all five pools, duplicates kept | 15,755,343 | 1.3707 | -19 +/- 20, rejected |
| self-play only | 4,866,000 | (89.8% explained) | -26 +/- 20, rejected |
| all five pools, deduplicated | 10,957,828 | 1.3618 | +15 +/- 11, adopted |

30.5% of the merged corpus was exact repeats (4,797,515 positions). Dropping them
improved the held-out fit on 44% fewer rows, and only the deduplicated net beat the
incumbent. So the answer to "do the human-game pools help" is yes, but only once the
repeats are gone: the same pools with their duplicates made the network worse than
having no new data at all.

It also settles a cheaper question. The held-out number tracked the race here
(1.3618 beat 1.3707 and won the match), which it did not do for the 128-unit net or
the probability-space blend. A held-out number is worth trusting only when the
corpora being compared are the same shape.

### A race is only as clean as the machine under it

The first block of the adopted net's race read +22 +/- 18 over 1,500 games; a
confirmation block on an idle machine read +1 +/- 23 over 900. The difference was
the box, not the network: swap sat at 6,636 MB of 7,168 MB with 72 MB of free
pages, and the gauntlet's own 2.8 GB (tt_bits 22, ten workers, two engines each)
was what tipped it. The sixth chunk ran 25 minutes against 8 for each clean one.
Pooled over 3,600 games across three blocks at different openings the answer settled
at +15 +/- 11, so check `vm.swapusage` before believing a single block.

## The generator rewrites a file the test suite reads

`nnue/main.go:1249` saves `halfkp_latest.json` after every generation, and
`TestHalfKPEvaluatesInPawnsNotProbability` asserted on whatever it found there.
An eight-game smoke run of the generator therefore turned the suite red while
nothing in the engine had changed: the test was reading a network trained on 743
positions. It now reads `champion_net.json`, which is tracked and does not move
under it.

The same run made `TestTenMillisecondBudgetIsRespected` fail at 142% of a 10 ms
budget, which is the ten generator workers, not the clock code. Both reds cost a
four-minute suite run each. Before believing a red in this repo, check `uptime`
and whether a generator is running.

## Width stays dead on the clean corpus

The 128-unit rejection was worth re-asking once the corpus was deduplicated: it
had been measured on `clean_r9.bin` alone, 8.0M positions from one pool, and the
engine loses -168 at equal depth 4 and -215 at depth 6 against Stockfish, which
is an evaluation gap, not a search one.

Trained on the same 10,957,828 distinct positions, same trainer, same schedule,
only the width differing:

| width | best held out | explains |
| --- | --- | --- |
| 64 | 1.3618 (epoch 24) | 91.3% |
| 128 | 1.3705 (epoch 16) | 91.2% |

The wider net fits worse and stops improving eight epochs earlier. No race was
run: the corpora being compared are the same shape, which is the one case where
the held-out number has tracked the race here, and the earlier head to head
already read -28 +/- 20. Capacity is not what the evaluation is short of.

## Razoring pays, and the champion files had drifted apart

Razoring adopted at +14 +/- 12 over 3,000 games at 100 ms, SPRT[0,10] settled
better at llr +3.26. The node measurement predicted it: 94.7% of the plain node
count over six positions at depth 7, and the Elo came in where a 5% node saving
should land.

The race needed 3,000 games rather than the 2,400 budgeted. It read +15 +/- 14 at
2,400 with llr +2.84 against a +2.94 bound, which is the one case worth paying
another block for: a test one block short of settling is not a result.

Adopting it exposed something else. `champion_ui.json` still had no `improving`
flag, adopted days earlier at +12 +/- 11 over 4,000 games, so the champion the UI
runs at 8 threads and 3 s a move has been playing weaker than the one being
raced. The three champion files carry their feature lists independently and
nothing checks that they agree.

## Labelling is the whole cost of generating data, not playing

Positions per second, 40 games each, measured 2026-09-18:

| play depth | label depth | positions/s | per hour |
| --- | --- | --- | --- |
| 6 | 8 | 11.3 | 40k |
| 3 | 8 | 16.1 | 58k |
| 3 | 6 | 132 | 476k |

Halving the play depth bought 1.4x. Dropping the label search two plies bought
8.2x, because every stored position pays for its own search and the games
themselves are nearly free. A night at the first setting adds 4% to an 11M
corpus; at the last it adds about 50%.

Depth 6 labels are not a compromise here: `clean_r9.bin`, the 8M pool the
champion actually learned from, is labelled at depth 3. Count has beaten label
depth every time it has been measured in this project.

## Where the champion stands, all three rulers on the same engine

Measured 2026-09-18 after the deduplicated network and razoring, against
Stockfish limited to UCI_Elo 2800, one thread, idle machine:

| clock | games | W-D-L | Elo against SF@2800 | champion on that ruler |
| --- | --- | --- | --- | --- |
| 10 ms | 600 | 64-47-489 | -307 +/- 40 | 2,493 +/- 40 |
| 100 ms | 600 | 42-89-469 | -309 +/- 40 | 2,491 +/- 40 |
| 1 s | 200 | 38-51-111 | -133 +/- 52 | 2,667 +/- 52 |

10 ms and 100 ms agree inside their intervals, and on those two alone this
section originally concluded that the deficit had stopped shrinking with the
clock. The 1 s measurement refutes that: 176 Elo better than 100 ms, well
outside both intervals. Two points were not a trend.

What the shape does say is that the engine gains more from a longer clock than
Stockfish does at this rating, and that nothing is gained between 10 ms and
100 ms, which is where a search that deepens too slowly would show. The first
100 ms figure of the evening, -325 +/- 41, predates razoring and describes a
different engine; the three above were all taken after it.

## The Stockfish ruler is not the same opponent at every clock

The three measurements above look like the engine gains nothing between 10 ms
and 100 ms and a great deal by 1 s. The first reading of that was that the
search converts time into depth badly at short clocks. Measured, it does not:

| clock | mean depth | mean nodes |
| --- | --- | --- |
| 10 ms | 3.7 | 2,026 |
| 100 ms | 7.7 | 50,858 |
| 1 s | 12.0 | 557,738 |

Four plies per tenfold increase, twice over, which by the ~150 Elo per ply ladder
is worth several hundred Elo of self-improvement across each step. The engine is
converting time into depth exactly as it should.

So the flat stretch is the opponent's. Stockfish under `UCI_Elo 2800` is not a
fixed-strength player across time controls: it improves with the clock alongside
us at 10 ms and 100 ms, then its limiter saturates and it stops, which is why the
gap collapses from -309 to -133 by 1 s. An Elo read off this ruler is only
comparable to another Elo read at the same clock, and "the engine is 2,493" is
meaningless without saying at what time control it was measured.

TestDepthReachedAcrossClocks keeps the measurement, and fails if a tenfold clock
increase ever buys less than two plies.

## The engine is rated about 1,985 at 10 ms, not 2,493

Every absolute rating this project has quoted was read off a single Stockfish
rung at `UCI_Elo 2800`. Measured against five rungs, the same champion, 400
games each at 10 ms on an idle machine:

| opponent | W-D-L | Elo difference | implied rating |
| --- | --- | --- | --- |
| SF@2000 | 162-59-179 | -15 +/- 34 | 1,985 |
| SF@2200 | 105-67-228 | -110 +/- 36 | 2,090 |
| SF@2400 | 69-53-278 | -201 +/- 40 | 2,199 |
| SF@2600 | 49-48-303 | -260 +/- 45 | 2,340 |
| SF@2800 | 64-47-489 | -307 +/- 40 | 2,493 |

A consistent ruler gives the same answer from every rung. This one climbs 508
points across the range: 800 nominal points of opponent strength cost only 292
points of measured result, so `UCI_Elo` spacing is compressed by about 2.7x at
this clock. Reading a rating off a rung 800 points away therefore inflates it by
roughly 500.

The only rung where a rating can be read directly is the one where the engine
scores near 50%, and that is SF@2000: 47.9%, so the engine is about 1,985 +/- 34
at 10 ms. Chart and numbers: `analyses/elo-calibration/report.html`.

This does not change any A/B result in this file. Those are all head to head
against the previous champion at a fixed clock, which is a difference and needs
no absolute scale. It does invalidate every absolute claim, including the 2,710
carried in `champion.json` and the 2,281 lichess calibration, and it means a
target expressed as a percentage of rating was resting on a number 500 points
too high.

## A judge built with skill 0 is the weakest Stockfish, not the strongest

`engine.NewStockfish(path, skill, elo)` sends `setoption name Skill Level
value <skill>` unconditionally, so `NewStockfish(sf, 0, 0)` is not "no limit
set", it is Stockfish at its weakest setting. Its search scores stay honest
while its move choices are deliberately degraded, which is a peculiarly
misleading combination for an oracle.

Signature of the bug: the engine under audit appeared to choose better moves
than the judge, by the judge's own evaluation, averaging -1.41 pawns across 319
disagreements. A negative mean cost is impossible against a real oracle and is
what gave it away. Two rounds of fixing the cost arithmetic came first and
neither helped, because the arithmetic was never the problem.

Corrected, with skill 20, over 400 positions at depth 10:

| phase | agreed | mean cost when we differ |
| --- | --- | --- |
| opening | 56.2% | 0.34 pawns |
| middlegame | 47.4% | 0.36 pawns |
| endgame | 40.8% | 0.22 pawns |
| all | 45.8% | 0.29 pawns |

Every other caller in the repo already passed 20. The gauntlet builds its
reference as `NewStockfish(*refUCI, 20, *refUCIElo)`, so no race or calibration
was affected, only the new audit.

Agreement is lowest in the endgame but costs least there, which fits: an endgame
offers many moves that all keep the result. The middlegame is where a
disagreement is most expensive, so that is where an evaluation defect is worth
hunting.

## RETRACTED: the evaluation does not prefer pieces over pawns

This section reported that the oracle plays twice as many pawn moves as this
engine (51 against 24, then 24 against 11), called checks and king moves the
symmetric control, and treated it as the first concrete lead on the evaluation.
It was an artifact of the measurement, and the sections below record the chase
it set off. They are kept because the refutations are worth having, not because
the premise was.

The audit played its games with one search and audited the position with
another, both sharing a transposition table. The audited search's entries
therefore changed which moves the game-playing search found, so the positions
themselves differed with the audited depth, and the two "independent runs" that
agreed shared the same flaw.

With a table each and the judge held at depth 12:

| our depth | agreement | pawn push, judge against ours | score error |
| --- | --- | --- | --- |
| 4 | 41.6% | 21 against 18 | +0.04, absolute 0.81 |
| 8 | 44.4% | 12 against 14 | +0.29, absolute 1.20 |
| 12 | 45.6% | 15 against 16 | +0.03, absolute 1.07 |

Symmetric at every depth. What the clean sweep does show is agreement rising
monotonically with depth against a fixed judge, which is what a working search
looks like, and a score error near zero with about a pawn of spread.

Two runs agreeing proved nothing here: both carried the same confound, and the
counts were small enough that Poisson noise alone spans the ratios claimed.

## A judge built with skill 0 is the weakest Stockfish, not the strongest

`engine.NewStockfish(path, skill, elo)` sends `setoption name Skill Level
value <skill>` unconditionally, so `NewStockfish(sf, 0, 0)` is not "no limit
set", it is Stockfish at its weakest setting. Its search scores stay honest
while its move choices are deliberately degraded, which is a peculiarly
misleading combination for an oracle.

Signature of the bug: the engine under audit appeared to choose better moves
than the judge, by the judge's own evaluation, averaging -1.41 pawns across 319
disagreements. A negative mean cost is impossible against a real oracle and is
what gave it away. Two rounds of fixing the cost arithmetic came first and
neither helped, because the arithmetic was never the problem.

Corrected, with skill 20, over 400 positions at depth 10:

| phase | agreed | mean cost when we differ |
| --- | --- | --- |
| opening | 56.2% | 0.34 pawns |
| middlegame | 47.4% | 0.36 pawns |
| endgame | 40.8% | 0.22 pawns |
| all | 45.8% | 0.29 pawns |

Every other caller in the repo already passed 20. The gauntlet builds its
reference as `NewStockfish(*refUCI, 20, *refUCIElo)`, so no race or calibration
was affected, only the new audit.

Agreement is lowest in the endgame but costs least there, which fits: an endgame
offers many moves that all keep the result. The middlegame is where a
disagreement is most expensive, so that is where an evaluation defect is worth
hunting.

## The evaluation prefers pieces where the oracle prefers pawns

Over 500 positions at depth 10, counting for each kind of move how often the
oracle played one and we did not, against the reverse:

| kind | judge only | ours only |
| --- | --- | --- |
| pawn push | 51 | 24 |
| develops | 11 | 28 |
| capture | 21 | 10 |
| check | 18 | 16 |
| king move | 26 | 28 |
| retreat | 35 | 34 |

Checks, king moves and retreats come out symmetric, which is what no signal
looks like and is the control that makes the rest worth reading. Pawn moves and
development do not: the oracle plays a pawn move twice as often as we do, and we
take a piece off its home rank two and a half times as often as it would.

Agreement itself is 47.6%: 57.8% in the opening, 46.8% in the middlegame, 44.3%
in the endgame.

This is a preference difference, not proof of a missing term, but it is the
first concrete lead on the evaluation the project has had. It also fits how the
labels are made: a pawn move pays off over a long horizon, the training labels
come from our own search at depth 6 to 8, and a network cannot learn what its
teacher cannot see. The hand-written evaluation has a pawn `Structure` term,
which `hand_blend 0` switched off entirely when the network took over.

### The network is not pawn-blind, which refutes the obvious reading

The first explanation for that gap was that a 64 unit HalfKP network cannot
represent pawn structure. It can. Same material either side, only the pawns
standing differently:

| pair | score change |
| --- | --- |
| doubling and isolating three White pawns | -0.530 |
| blocking White's passed pawn | -1.164 |
| a pawn chain against scattered pawns | +0.042 |

Doubled, isolated and blocked passed pawns are all penalised, correctly and with
sensible weight. Only the chain against scattered pawns draws no opinion.
`TestNetworkSeesPawnStructure` keeps the first two, so a retrained network cannot
quietly lose the knowledge.

Move ordering was the next suspect and is also innocent: `scoreMove` ranks quiet
moves purely by history with no piece-type term, so a pawn push is ordered like
any other quiet move. What remains is that history is self-reinforcing, and a
move kind that rarely causes early cutoffs stays ranked low, where late move
reduction cuts it hardest. Testing that needs the rank of the best move recorded
by piece type, which is instrumentation this session did not build.

### Exempting advanced pawn pushes from reduction is inert here

The standard fix for an engine that under-plays pawn moves is to stop reducing
and pruning pawn pushes near promotion, which strong engines all do. Implemented
behind `pawnpush` and measured before racing it: over four pawn-heavy positions
at depth 8 with the champion's own feature set, the exemption changed the search
by 16 nodes out of 64,438, or 0.02%.

So move ordering already places an advanced push early enough that late move
reduction and pruning seldom reach it. A race would have spent an hour
confirming zero. The flag and its test stay, off, so the technique is not
implemented a second time by someone reading the same chessprogramming page.

The cost of knowing this was one unit test comparing node counts, which is the
check worth running before every feature race: if a feature cannot move the node
count on positions chosen to favour it, it cannot move the Elo.

### And the same evening, `go test | tail` hid a red again

The commit above went out on a failing test. `go test ./engine/ | tail -1`
printed FAIL, but `tail` exits 0, so the `&&` chain ran on and pushed. This is
the second time that pipeline has masked an exit code in this project.

The test itself was the wrong shape: it asserted the exemption searched MORE
nodes, when the whole finding is that the difference is a handful of nodes in
either direction and the search is not bit-deterministic across runs. It now
asserts the measured claim, that the two searches stay within 1% of each other,
and fails if that ever stops being true, which would mean the skipped race is
worth running after all.

Run `go test ./...` and read `echo $?`, never a pipeline whose last stage is
`tail`, `head` or `grep`.

## The evaluation is within a pawn of the oracle, and four explanations are dead

The audit now also records the signed difference between this engine's search
score and the judge's on the same position. Over 285 positions at depth 10:
mean **-0.28 pawns**, mean absolute **0.92**. So the evaluation is not
systematically optimistic; if anything it is slightly pessimistic, and its
typical error against a depth-10 Stockfish is under a pawn.

That came from chasing one position where the engine gave up 6.89 pawns:
`8/1p4kp/pN4n1/P7/8/1P5R/2r1rp1P/5R1K w - - 0 1`, where it plays h3f3 and holds
a score of -2.00 unchanged from depth 5 to 12 while the judge says -4.37, and
-8.23 after the move. Four candidate explanations, all refuted by measurement:

- the network cannot see pawn structure: it scores that position -4.67, close to
  the judge, and adds 2.3 pawns as a pawn walks from f4 to f2;
- move ordering penalises pawn moves: `scoreMove` has no piece-type term;
- late move reduction buries advanced pushes: exempting them moves 0.02% of nodes;
- quiescence cannot see promotions: `AppendQuiescenceMoves` generates them.

What survives is that the search believes rescue lines the oracle refutes, which
is weakness rather than a bug, and one reproducible asymmetry: across two
independent runs the oracle plays roughly twice as many pawn moves as this
engine does (24 against 11, and 51 against 24), and this engine takes pieces off
the back rank noticeably more often (23 against 16, and 28 against 11). Checks
and king moves stay symmetric in both runs, which is the control.

## Timing tests fail on preemption, not on slowness

Three false reds in one evening on `TestTenMillisecondBudgetIsRespected`, once at
587% of a 10 ms budget, every time because the data generator held all ten
cores. The test was right that the budget was missed and wrong about who missed
it, and each red cost a five minute suite run to diagnose.

The cause is not throughput. Under a load average of 91 a fixed 20M iteration
spin still finished in 11.3 ms, barely slower than idle. What collapses is
holding the processor: the gap between two consecutive `time.Now()` reads
reached 20 to 30 ms. A search descheduled for 30 ms cannot stop at 10 ms however
carefully it checks the clock.

`skipIfMachineBusy` measures that gap over a 20 ms window and skips above 3 ms.
Verified both ways, which a skip guard needs or it is just a disabled test: it
skips with the generator running (9.5 ms gap) and lets the test run and pass
without it.

Two traps came with it. `go test` caches results, so a rerun printed an
identical skip down to the microsecond and looked like fresh evidence; use
`-count=1`. And load average decays over minutes, so a machine is not idle the
moment the process is killed.

Jamf Protect is worth knowing about on this laptop: it sat at 338% CPU scanning
the gigabytes of pool files being written, alongside `mds`. A load average of 50
here is not necessarily my own work.

### The lichess games agree with the corrected rating, which the old one never explained

Saying the calibration invalidates every absolute number was too broad. The
lichess results are an independent instrument, measured against a real
population rather than against Stockfish, and they always fitted the corrected
figure better than the old one: at 10+5 the bot beat maia5 (1655), sargon-3ply
(1545) and GarboBot (1988), and lost to the stronger bots. Beating a 1988 bot is
what an engine around 2,000 does. It is not what a 2,710 engine does, and that
tension sat in this file unremarked.

So two instruments now agree on roughly 2,000 at short controls, and only the
Stockfish `UCI_Elo` readings ever said 2,500 or 2,700. The provisional 3,004
lichess showed after five games is not evidence of anything; a rating over five
games is mostly its own prior.

Move generation was checked at the same time, since an engine searching twelve
plies with an evaluation within a pawn of the oracle ought to be stronger than
2,000. Perft is exact on every position except position 4, where it is short by
exactly the under-promotions: the generator makes queens only. That is worth a
few Elo in rare endings, not hundreds, and it is deliberate and marked
`knownGap` in the test.

## A fresh transposition table distorts every short-budget measurement

The clock sweep read 2,026 nodes at 10 ms against 50,858 at 100 ms, twenty-five
times the nodes for ten times the time, which looks exactly like a fixed
per-move cost eating a short budget. It is not. Each measurement allocated its
own 4M entry table, and the page faults of first-touching 100 MB are charged to
whichever search runs first.

With one table, warmed once, as real play has it:

| budget | nodes | knps |
| --- | --- | --- |
| 10 ms | 3,968 | 373 |
| 40 ms | 13,120 | 311 |
| 320 ms | 92,160 | 270 |

Throughput at 10 ms is the best of the three, so short budgets carry no
handicap. `TestShortBudgetsAreNotHandicapped` keeps that, and fails if the 10 ms
rate ever falls below half the 320 ms rate, which at a 10 ms control would be
worth hundreds of Elo.

Any measurement at a short budget must reuse a warmed table, or it measures
`mmap` rather than the engine.

## The opening book is inert in every race, and live in every real game

`scripts/chunked_match.sh` passes the gauntlet's default `-opening-plies 6`, six
random plies to start each game. Measured against `games_book_v5.txt`:

| opening | book hits |
| --- | --- |
| from the start position | 600 of 600, 100% |
| after 6 random plies | 0 of 60, 0% |

A book built from real games has nothing to say about a position reached by six
random moves, so it never speaks. Every A/B and every calibration this project
has run therefore measured a bookless engine, on both sides.

That makes the A/B results valid, since both sides lost the book equally, and it
makes two other things false:

- Removing the book read -2 +/- 14 over 2,400 games at 10 ms. That is a null
  control, not a result: neither side had a book to remove.
- The engine measured in races is not the engine that plays on lichess or in the
  UI, where games start from the real initial position and the book does fire.
  The book's measured 5.5% bad-move rate is therefore unmeasured risk in exactly
  the games that face real opponents, and the ratings measured against Stockfish
  say nothing about it.

Measuring the book needs openings the book knows: real positions from an opening
set, not random plies, and not `-opening-plies 0`, which repeats identical games
and has fabricated 40 Elo here before.

### And measured properly, the book is worth nothing

Racing the champion against itself with the book removed, starting from 1,500
positions sampled out of `games_book_v5.txt` so the booked side gets hits from
the first move: **+6 +/- 20 over 1,200 games** at 10 ms, llr +0.13, nowhere near
either bound.

So the book neither helps nor hurts. The 5.5% bad-move rate measured earlier is
real but costs nothing that 1,200 games can see, and the unmeasured risk flagged
above is smaller than it looked. It stays because it costs nothing to keep, not
because it earns its place.

The general shape is worth keeping though: a feature that only acts in positions
the harness never visits cannot be measured by the harness's defaults, and
`-match-openings` with positions drawn from the feature's own domain is how to
give it a fair hearing.

## A 5% larger corpus buys about 9 Elo, and SPRT cannot settle it

The loop closed once end to end: generate, merge, train, race. Self-play at play
depth 3 and label depth 6 produced 621,577 positions, of which 569,483 were new
against the existing corpus (91.6%, and only 0.4% duplicates), giving 11,527,328
distinct positions, 5% more than the corpus that won +15.

Retrained on it, the network reads **+9 +/- 10 over 5,100 games** at 100 ms,
with block estimates of +8, +12, +10 and +2. SPRT[0,10] ran from llr +0.89 to
+2.38 and back to +2.04 without settling, which is what happens when the true
effect sits on the threshold the test is asking about: "worth at least 10" and
"worth nothing" both fit.

SPRT[-5,5] settled better at llr +4.89. That is the question worth asking of
more data: not whether it clears a bar, but whether it regresses. It does not,
so it was adopted.

Held-out loss was no help here and would have been misleading: 1.3895 against
the old network's 1.3618 looks worse, but the constant it is measured against
moved too (15.89 against 15.63) because the corpus changed. A held-out number
compares two networks on one corpus, never two corpora.

## Cheap labels win: depth 4 beat depth 6 per hour of machine time

Labelling is the whole cost of generating training data, so the label search
decides how much data an evening buys:

| label depth | positions/s | batch | new positions | Elo from retraining |
| --- | --- | --- | --- | --- |
| 6 | 132 | 621,577 | 569,483 | +9 +/- 10 over 5,100 games, never settled |
| 4 | 1,261 | 1,899,215 | 1,455,021 | +12 +/- 11 over 3,900 games, settled better |

The depth-4 batch took 26 minutes and produced more Elo than the depth-6 batch
did in hours. Nine and a half times the throughput, and the labels are no worse
than `clean_r9.bin`, the 8M pool this engine was built on, which is labelled at
depth 3.

Duplicate rate rose with the shallower play, 3.3% against 0.4%, which is what
happens when weaker play revisits the same positions; poolmerge drops them, and
1,455,021 of 1,899,215 still landed.

So the corpus is now 12,982,332 positions and the lever is clear: more positions,
labelled cheaply, beats fewer positions labelled well. That was not obvious, and
the opposite was assumed when the generator was first set to depth 8.

## Data scaling, four points in one night

Each row is the same architecture and trainer, raced against the champion it
replaced, at 100 ms:

| corpus | new positions | explained | Elo |
| --- | --- | --- | --- |
| 10,957,828 (deduplicated) | - | 91.3% | +15 +/- 11 |
| 11,527,328 | 569,483 at label depth 6 | 91.3% | +9 +/- 10, never settled |
| 12,982,332 | 1,455,021 at label depth 4 | 92.0% | +12 +/- 11 |
| 16,796,968 | 3,814,636 at label depth 4 | 93.0% | +22 +/- 16, settled in 1,800 games |

The gains are not shrinking as the corpus grows, they are growing with the size
of each batch, which is what a data-limited network looks like. Four cycles in
one night, and the only cost is machine hours at 1,261 positions a second.

The duplicate rate is the thing to watch: 0.4%, then 3.3%, then 11.9% as the
batches grew, because self-play at play depth 3 keeps revisiting the same
positions. When it approaches 100% the well is dry and the opening diversity, or
the play depth, has to change before more hours buy anything.

## What one night bought, measured directly

The engine at commit 7098b16, network and feature list included, against the
engine now, 1,500 games at 100 ms: **+65 +/- 18**, W-D-L 718-341-441.

That is close to the +72 the five adopted changes summed to individually
(+15 dedup, +14 razoring, +9 and +12 and +22 from data), so they stack rather
than overlapping, which was not guaranteed: three of the five are the same
lever applied repeatedly.

Against a fixed external opponent the same work reads smaller. At 10 ms, the
SF@2000 rung moved from -15 +/- 34 to +5 +/- 34, and SF@2200 from -110 +/- 36 to
-75 +/- 35: roughly +20 and +35, both far wider than the head-to-head interval
and both below +65. Transfer to an outside opponent is partial and expensive to
measure; 400 games per rung buys +/- 34, while 1,500 head to head buys +/- 18.

So head to head is the instrument for deciding what to keep, and the external
rungs are the instrument for knowing where the engine actually stands. Using
either one for the other's job wastes hours.

## Where self-play starts decides how fast the well runs dry

Self-play defaults to ten random plies before the engine takes over. Measured
against the 16.8M corpus, one generation of 600 games each way:

| start | positions/s | new positions | new/s |
| --- | --- | --- | --- |
| ten random plies | 1,261 | 62.8% | 792 |
| `openings.txt`, 150,000 real positions | 880 | 89.9% | 791 |

The same yield per second today, and a different trajectory: the random-plies
well is already 37% depleted while the book's is 10%, because the corpus is
largely made of random-plies games already. Generation switched to
`-opening-book openings.txt`.

The duplicate rate is the gauge to watch, not the raw position count. It went
0.4%, 3.3%, 11.9%, then 37% on successive random-plies batches, and 10% on the
first book batch.

## The ruler is consistent at 1 s and incoherent at 10 ms

Measured with the current champion, one thread, 200 games at 1 s:

| clock | against SF@2400 | implies | against SF@2800 | implies | spread |
| --- | --- | --- | --- | --- | --- |
| 10 ms | -201 +/- 40 | 2,199 | -307 +/- 40 | 2,493 | 294 |
| 1 s | +191 +/- 56 | 2,591 | -133 +/- 52 | 2,667 | 76 |

At 1 s two rungs 400 points apart agree within 76 points. At 10 ms five rungs
disagree by 508. So `UCI_Elo` is a usable scale at a normal clock and not at a
very fast one, where Stockfish's own limiter and its time handling interact in
ways that compress the scale.

**This corrects the retraction above.** "The engine is about 1,985" is true at
10 ms and says nothing about any other clock; at 1 s the same engine measures
about 2,600 single-threaded, and the two rungs agree on that. So
`champion.json`'s 2,710, measured at 1 s with eight threads, was never the
500-point inflation this file claimed an hour earlier: it is consistent with
2,600 single-threaded plus threads. What was wrong was reading a 10 ms result
off a rung 800 points away, not the 1 s calibration.

The swing itself is the headline: the same engine reads -201 against SF@2400 at
10 ms and +191 at 1 s, a 392 point move bought entirely by the clock. Any Elo
quoted here without its time control is meaningless.

## Rung 12: Continuation History and History-Adjusted LMR (+23 Elo)

Adopted 2026-09-19 into `champion.json` and `champion_bot.json`:
- `conthist`: Quiet move history conditioned on the previous opponent move (`prevMove.To -> piece -> move.To`).
- `histlmr`: Late move reductions shifted by history performance `shift = history / 16384` bounded in `[-2, +2]`.
- Measured: +58 +/- 91 over 60 games and +23 +/- 49 over 200 games at depth 4 vs previous champion.
- Opening book aligned to `games_book_v5.txt` (+9 to +21 Elo over `champion_selfplay_book.txt`).

## 20.06M Deduplicated Positions Milestone

Generated 4,017,623 positions via self-play with `openings.txt` (150k real openings) and depth-4 labeling.
Merged into `merged_v5.bin`:
- 20,814,591 read, 20,058,404 written, 756,187 duplicates dropped (3.6%).
- Reached 20.06M distinct positions with 96.4% novelty yield.

## Rung 13: 20.06M Deduplicated Positions Network (+40 Elo)

Adopted 2026-09-20 into `champion.json`, `champion_bot.json`, and `champion_ui.json`:
- Retrained on `merged_v5.bin` (20,058,404 deduplicated positions).
- Test loss dropped to 1.2691 (explains 93.25% variance), the lowest recorded in this project.
- Measured: +40 +/- 22 Elo over 1,000 games at 10 ms against Rung 12 champion (W-D-L 472-170-358, score 0.557).
- SPRT[0, 10] settled better at llr +3.51 after 1,000 games.
- Champion updated to `champion_net.json` with bit-exact regression tests verified.


## The held-out set was re-drawn every rung

`train.py` split by game with `torch.randperm(len(games))` under a fixed seed.
A permutation over a different count is a different permutation, and every
rung adds games, so every rung held out a fresh random 15%. Checked on the
real counts, 311,619 games at rung 24 and 312,219 at rung 25: of the 46,754
old games rung 25 held out, only 6,963 had been held out at rung 24. The other
85% were training games for the network the fine-tune starts from, and after
four fine-tunes in a row almost nothing in the held-out set was unseen.

That is why rungs 20 to 24 all stopped at epoch 1: the starting weights had
memorised the held-out games, so every epoch of real training looked like a
regression. Early stopping, "best epoch" and every "record test loss" on the
fine-tuned lineage were measured on training data.

The split is now a hash of the game id (`held_out_games`), and poolmerge keeps
the first pool's ids, so an old game stays on the side it was on.
`test_growing_the_pool_leaves_every_old_game_on_its_side` fails against the old
permutation. The current lineage has still seen nearly every old game, so its
held-out numbers stay contaminated until a network is trained from scratch on
the fixed split.

## Rungs 19 to 23 were the harness, not the networks

Rungs 19 to 23 were raced with `gauntlet-bin -halfkp net -ref-champion
champion.json` at depth 4. That puts the network on `engine.Strong`, not on the
champion: none of the fifteen search features, so at a fixed depth the
challenger prunes less, searches a wider tree and wins on that alone. The rungs
claimed +31, +48, +52, +51 and +43.

All three measured the same evening, 2,000 games at depth 4 from `openings.txt`:

| match | W-D-L | Elo |
| --- | --- | --- |
| null control: champion net via `-halfkp` against the champion | 709-800-491 | **+38 +/- 15** |
| net_v24_fine against the champion, both from champion files | 575-846-579 | -1 +/- 15 |
| champion against the champion config carrying the rung 19 net | 575-877-548 | +5 +/- 15 |

So the four fine-tunes since rung 19 are worth +5 +/- 15 together, not +194, and
the internal ladder's 3,163 is about 190 points of harness. Rungs 17 and 18 used
the same flags at 10 ms, where the bias has another size and an unknown sign.
Rungs 13 to 16 used `-champion candidate.json` and stand.

`gauntlet-bin` now refuses `-halfkp` or `-net` against `-ref-champion` unless
`-champion` is given, so the only way to race a network against the champion is
a champion file naming it. `ladder.sh`, `ladder_scratch.sh` and `screen.sh` still
pass the refused flags and stop with that message.

## More data, cleaner splits and deeper labels all read flat

Clean races at depth 4, 2,000 games each, against the rung 23 champion:

| candidate | Elo |
| --- | --- |
| net_v25_fine, 60 epochs on the leaky split | -32 +/- 15 |
| from scratch on merged_v25 (13.67M), fixed split | -18 +/- 15 |
| from scratch on merged_v26 (16.06M, 2.39M new) | -27 +/- 15 |
| champion fine-tuned on 200k PGN positions, labels depth 8 | -27 +/- 15 |
| depth 8 labels against the same positions at depth 4 | -4 +/- 15 |

The newest self-play batch was 41% duplicates of the corpus. A from-scratch
network on this corpus does not reach the champion, whose lineage started on
the 24.58M corpus of rung 19 that no longer exists. Depth 8 labels bought
nothing over depth 4 at 200k positions; labelling ran at 97 positions a second.

Search features never measured before, champion against champion plus one, at
100 ms: `singular` -5 +/- 21 (1,000 games), `historyaging` +4 +/- 21 (1,000),
`histgravity` +7 +/- 21 then +3 +/- 15 (3,000 in all). None adopted.

Width, measured in games this time: a 128-unit network from scratch on
merged_v26 reached the same held-out loss as the 64-unit one (1.1984 against
1.1991) and lost to it by -32 +/- 21 over 1,000 games at 100 ms. The corpus, not
the capacity, is the ceiling, and the wider network only pays its slower
evaluation.

## Against Stockfish 2700, 200 games is one engine's noise

Same engine settings, 1 s a move, one thread each, 200 games per row:

| engine | openings | W-D-L |
| --- | --- | --- |
| rung 23 | offset 70000 | 70-37-93 |
| + nullpieces | offset 70000 | 73-34-93 |
| + drawscale | offset 70000 | 89-39-72 |
| + drawscale | offset 80000 | 85-43-72 |
| without drawscale | offset 80000 | 89-34-77 |

The jump to 89 wins looked like drawscale and was not: on fresh openings the
engine without it scored the same. Four runs spread between 35% and 45% wins,
which is the noise of a 200-game match against a strength-limited Stockfish,
whose UCI_Elo cap plays deliberately imperfect moves. Only a difference of
about 25 wins between two arms on the same openings means anything.

At 100 ms a move the same champion scores 63-64-273 over 400 games, -203 +/-
40. The gap to Stockfish grows as the clock shrinks: it is search efficiency
at low node counts, not only evaluation. Both sides really get the clock: over
30 positions at a 100 ms budget ours took 105.7 ms a move, Stockfish 101.1 ms.

`drive2` (king drive from +2 in endgames) read 93-29-78 on the offset 80000
openings, against 85 and 89 wins for the two arms above: inside the noise, not
adopted. `tt_bits 20` against 22, champion against itself at 100 ms, read
-5 +/- 21 over 1,000 games: the table size is not what costs us at short clocks.

## SPSA on twelve search margins: 6,000 games bought nothing

Against Stockfish 2700 at 100 ms, 1,000 games with five in parallel (so each
engine has its own core): -209 +/- 25. The ten-in-parallel runs had starved our
side of CPU (37% of a core against 50%), yet read the same -203.

`tools/spsa/spsa.py` ran 300 iterations of 20 games at 100 ms on RFP, razoring,
futility, LMR, LMP, null move, aspiration and delta pruning margins. The largest
move was LMPBase 3 to 6.5. The result read +2 +/- 21 against the defaults over
1,000 games and -214 +/- 26 against Stockfish: inside the noise both times. Twelve
parameters need far more than 6,000 games. After 482 iterations the same
comparison read +12 +/- 21, pointing the right way and still unproven.

## A quiescence cap of 16 pays, and how the 100 ms games are lost

`tools/lossaudit/loss_phases.py` over 1,000 games against Stockfish 2700 at
100 ms: 713 losses, 435 of them evaluation lag (Stockfish saw it 10+ plies
first) and 278 sudden, with the sudden ones collapsing at a median ply 24. 141
losses collapsed within 20 plies of the start, and 58 of those began from a
position Stockfish already scored +1 or more: 14% of `openings.txt` starts are
decided before either engine moves, and some are not openings at all (a back
rank of three queens, castling rights with the rook gone). 
`openings_balanced.txt` keeps the 11,189 of the first 20,000 that Stockfish
scores within half a pawn at depth 8.

The capture search stopped at four plies, so a long exchange was scored
mid-sequence. Champion against champion at 100 ms:

| qply | W-D-L | Elo |
| --- | --- | --- |
| 8 | 381-247-372 | +3 +/- 21 |
| 16 | 403-246-351 | +18 +/- 21 |
| 16, replicated on the balanced openings | 387-272-341 | +16 +/- 22 |

Pooled, cap 16 is +17 +/- 15 over 2,000 games and was adopted. It agrees with
the 10 ms screen of 2026-09-12 (+21 +/- 34) that nobody had confirmed.

## One ply is the difference, and depth transfers to Stockfish at about half

The 88 games lost or drawn to Stockfish 2700 at 100 ms
(`analyses/sf2700-losses/study-100ms-88-games.md`): in 44% the decisive move
is avoided by our own depth-14 search. Replayed 10 times each with a seeded
tie-break (`tools/lossaudit/depth_probe.py`), the losing move comes back in
21.1% of searches alone (median depth 9) and 31.7% under match load, 5 engines
at once (depth 8). Cut-off iterations explain 2 of the 465 bad moves.

Adopted on that evidence:

| change | against the previous champion | against Stockfish |
| --- | --- | --- |
| lazy legality, in-check screen, cheaper clip: 15% faster, same tree | +63 +/- 30 (530 games, binary vs binary) | |
| SPSA-482 margins in the champion file | +43 +/- 21 (1,038 games) | |
| both, paired on the same openings, alternated | | old -265, new -207: +58 |

The first unpaired Stockfish check (-216 on openings.txt offset 100000) looked
like no transfer. Paired on the same balanced openings, 460 games each, the new
engine is ahead in both halves. A Stockfish result on a subset of openings is
only comparable to another on the same subset.

## Round 2 of depth: a half-working shortcut and two kernels, gap 265 to 175

Three candidates built in parallel, each screened binary against binary at
100 ms a move:

| change | seeded nodes | speed | Elo | verdict |
| --- | --- | --- | --- | --- |
| NEON kernel for accumulator rows, bit-identical | 719267, same | median 0.89 time ratio, 12 of 12 pairs | +9 +/- 42 (268 games) | adopted on speed: same tree |
| pick the next move on demand, SEE deferred | 719267, same | 10% fewer instructions, wall clock lost in noise | +1 +/- 41 (272) | adopted on speed: same tree |
| table move tried first for either side | 641868 | about 17% faster | +16 +/- 15 (1,306, five screens) | adopted |
| also restore the previous move after every child | 607717 | | -7 +/- 24 (496) | rejected |

The table-move shortcut checked legality with `g.IsLegalMove`, which reads
`g.Turn`, and the search never moves `g.Turn`: the shortcut only ran at nodes
where the root side was to move. The test that pins it
(`TestSearchIgnoresGameTurn`) runs the same search with `g.Turn` flipped and
wants the same score and node count. Restoring `c.prevMove` after the table
move's subtree was needed for the fix to keep countermoves useful; doing the
same after every child of the main loop cut 5% more nodes and lost Elo, so fewer
nodes is not a gain on its own.

Against Stockfish 2700, same balanced openings (offsets 6000 and 6250):

| engine | W-D-L | Elo |
| --- | --- | --- |
| eb8d244, before round 1 | 47-70-343 | -265 +/- 34 |
| round 1: speed + SPSA-482 | 68-79-315 | -207 +/- 31 |
| round 2 (cafe78d) | 93-76-321 | -175 +/- 30 |

The gap shrank by 90 Elo, 34%. The round 2 runs came hours after the old ones,
not alternated with them, and the first ran at load 6.4 against 4 for the
others, which costs our engine depth and not Stockfish (whose strength is set
by `UCI_Elo`, not nodes).

## Won games thrown away on lichess: mates by depth, a sleeping Mac, phantom queens

Two bullet games (lhT8MqvU, hVG5pJcz) were drawn by the fifty-move rule with
Stockfish seeing mate in 3 to 12 the whole time. The engine saw a mate too
(+1005 to +1014) and shuffled: a mate was scored `mateScore + depthLeft`, so a
longer mate found with more depth left scored as high as a short one, and the
mate never came closer. The fifty-move draw was only a leaf fade, so the tree
kept searching past clock 100 and a pawn push found deeper revived the mate.
Mates now count plies from the root (table entries adjusted by ply), a node at
clock 100 is a draw, the table refuses mates the clock cannot reach, and
quiescence counts the clock. From the games' own positions against Stockfish:
9 of 9 winnable starts mated, master 1; won-endgame suite 15 of 20, master 1
(`analyses/lichess-games/lhT8MqvU-fix.md`, `tools/lossaudit/convert_check.py`).

The biggest leak was not chess. 54 of 55 losses on time in 592 games were the
Mac sleeping (`pmset sleep 1`) with 28 s to 605 s on our clock; the log's
monotonic clock shows 3m33s of a game that took 18m29s. The bot now runs under
`caffeinate -i -s` (`analyses/bot-elo/runtime-audit.md`).

A bug hunt with an adversarial check on every finding confirmed 17 more. The
worst: an opponent's underpromotion was replayed as a queen (`game.Move` had no
promotion piece), so the bot's board desynced and it flagged, three times, once
with mate in 6. Fixed with the move carrying its promotion (32 to 40 bytes,
2 to 4% slower search), plus move-post retries, one game loop per game, and
repetition keys that count castling rights and only a usable en passant square.

| fix group | screen at 100 ms | verdict |
| --- | --- | --- |
| draw scaling, fifty-fade that paid for sacrifices, mating corners for KBNK and KBBK | +16 +/- 21 (580 games) | adopted |
| draw bound when a side can step into a repetition, repetitions at the horizon | -11 +/- 20 (606 games) | held |

The repetition fix is sound in tests and lost Elo anyway, probably the cost of
a repetition check at every horizon node; the sweep found no game where a
repetition threw away a +4 position, so it waits.

## Round 3 of depth: 17% faster on the same tree

| change | instructions | why |
| --- | --- | --- |
| `board.Sq` as two `int8`: `game.Move` 40 bytes to 16 | -7.9% | the promotion field had pushed every move list over a cache line |
| table entries 24 to 16 bytes, prefetch of the child's slot | -0.3% (time median -7%) | the probe's samples all sat on the slot load: memory, not branches |
| slider attackers for SEE from bitboards, same pick order | -1.1% | the ray walk was most of SEE's cost |
| all three against master, 5 alternated pairs | -9.1%, time -17.2% | 641868 nodes before and after |

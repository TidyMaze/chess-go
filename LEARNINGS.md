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
| 128 hidden units | width | level |
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

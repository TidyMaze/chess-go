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

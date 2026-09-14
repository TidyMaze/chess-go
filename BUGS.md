# Bug list

Live list for the lichess bot and the engine behind it. A bug leaves the
open section only when it is fixed with a test that failed before the fix,
or when it is disproved with the output that disproves it.

## Open

### 1. King and two bishops cannot mate a lone king
Reproducible and deterministic, gated behind `ENDGAME=1` in
`engine/endgame_mate_test.go`. King and queen and king and rook both
convert; two bishops never do in 120 plies.

The cause is in `kingDrivingBonus`: it measures the bare king's distance
from the centre with a Chebyshev distance, which saturates along an
entire edge and scores a1 and a4 the same, so once the king reaches an
edge nothing points at a corner, which is where a two-bishop mate has to
be delivered. Replacing or supplementing that distance fixed the bishops
and broke the rook mate at every weight tried, so it is not a one line
change.

Measured across depths on the committed code, the problem is wider than
the bishops and the whole area is fragile:

| search depth | mates that fail |
|---|---|
| 3 | two bishops |
| 4 | two bishops |
| 5 | two bishops |
| 6 | king and rook |

The rook mate failing at depth 6 while converting at 3, 4 and 5 is not
something a bishop-specific fix explains. Conversion here depends on the
depth in a way that looks like luck rather than technique.

Three approaches have been tried and all rejected on measurement: a
Manhattan corner distance replacing the Chebyshev one, the two combined
with the corner as a tie-break, and a full mop-up term gated to positions
with no queen or rook. Each fixed one mate and broke another, or moved
with the depth. The last one made depth 6 worse, two failures against
one, and fixed nothing.

Low priority, and the game study says why: of 36 games that were not won,
only 7 involved an advantage of two pawns or more held for ten plies or
longer. The engine rarely survives far enough ahead for endgame technique
to decide the game, so this is not where the Elo is.

### 2. Outgoing challenges are rate limited by lichess
Not an engine bug, a consequence of this session sending far too many
challenges early on: five at a time every minute, and fourteen in a row to
one opponent.

Six attempts spread over three hours were all refused, at 20:14, 20:35,
21:16, 22:18, 23:19 and 23:20. The response carries no Retry-After and no
rate limit headers, only `{"error":"Too many requests. Try again later."}`,
so lichess says nothing about how long it lasts. Hourly retrying for three
hours produced nothing, which is enough to say this is not a short window
even without knowing what it is.

The challenger now waits an hour before its first retry and up to four
between them, rather than hammering hourly for a limit that is clearly
longer than that. Incoming challenges are unaffected and are where every
game of the last few hours came from, so the bot keeps playing, just not
on demand.

Three hypotheses were tested and all are wrong, so nobody needs to test
them again:

| guess | test | result |
|---|---|---|
| a short window | six retries an hour apart | refused every time |
| a daily cap on UTC | one request at 00:05 UTC | refused |
| a cap per opponent | a fresh opponent, leelapieceodds | refused |

So it is account wide, it does not reset at the UTC day boundary, and it
has now outlasted ten hours: refused at 20:14, 20:35, 21:16, 22:18, 23:19,
23:20, 00:05 UTC, 02:24 and 06:26, with a fresh opponent among them. Waiting is the only move; the doubling backoff
is there so the wait is not spent making requests that cannot succeed.

**The AI endpoint shares the same budget, which was assumed and should not
have been.** Challenging `/api/challenge/ai` kept working for hours after
bot to bot challenges were refused, so it looked like a separate limit and
was used to keep the bot playing, about eight games. It is not separate: by
04:44 the AI endpoint was refusing too. Those games probably spent the
allowance that would otherwise have gone to rated ones, which is the worse
trade, since AI games are unrated and move no rating at all.

`scripts/ai_game_keeper.sh` exists for this situation but is stopped for
now: it cannot do its job while the endpoint is refusing, and retrying a
limit is how this started.

## Fixed

- **The clock rule bled itself into a permanent scramble, and lost a game
  on time.** Blitz game hTmspQs0 was forfeited on time with the opponent
  still holding 4:58 of its five minutes: 4:30 left at move 8, 1:32 at move
  27, 0:19 at move 51. Nothing went wrong on one move, the whole game went
  wrong. Spending a twentieth of the clock plus nine tenths of the
  increment solves to a fixed point near seven tenths of the increment, so
  any long game drifted to a two second clock and stayed there. Simulated
  over 120 moves, seven of the eight controls the bot accepts ran to zero,
  so it was never a 5+3 problem.

  Spending now takes half the increment and spreads what sits above a
  reserve over the moves still to come, which settles 5+3 at 47 s instead
  of 0.2. The simulation is no longer gated behind an environment variable
  and asserts a floor and a ceiling per control, because a rule that never
  spends is safe and useless, which is the complaint that produced the old
  one.

  The second cause was contention, and measuring separated it from the
  first. Across eight rated games the bot finished under its budget every
  time, by 82 ms to 4.8 s a move; the lost game ran about a second a move
  over, and it was the one with a correspondence game searching fifteen
  seconds at a time on the same cores. Correspondence is declined now: it
  moves none of the four ratings being chased, holds a game slot for days,
  and starves the games that count. Commit c6bbedd.

- **Telling a deaf bot from an idle one, without guessing.** Both look
  identical from outside: 0% CPU, an empty log, no games, and `lsof`
  reporting the lichess socket as CLOSED, which on macOS it does even for a
  perfectly healthy HTTP/2 connection. A `kill -QUIT` dumps the goroutines
  and settles it in one line, at the cost of restarting the bot:

  | state | goroutine 1 | read loop |
  |---|---|---|
  | healthy, idle | `http2.transportResponseBody.Read` under `bufio.Scanner.Scan` | `clientConnReadLoop.run` alive on `ReadFrameHeader` |
  | deaf | `http2.(*pipe).Read` into `sync.(*Cond).Wait` | gone |

  The second is the bug below. The first was measured on 2026-09-14 after
  39 minutes of silence that looked exactly like a relapse and was not:
  no challenges had arrived. Worth the ten seconds of downtime rather than
  restarting on suspicion, which destroys the evidence either way.

- **The bot went deaf and said nothing, for as long as it was left.**
  Its process was alive at 0% CPU with an empty log while its only
  connection to lichess sat in CLOSED, and a monitor saw no games in flight
  for six checks running. Three separate faults, found in that order. Run
  made a single pass by design, so the first time lichess closed the stream
  the bot stopped playing. There was no way to notice a connection that
  died in the network, since a dead TCP connection never reports anything
  to the reader. And a clean stream end logged nothing at all, which is why
  none of it was visible.

  The first fix was wrong in a way only production showed. It closed the
  response body on a watchdog, and the bot's own goroutine dump had it
  still parked in http2.(*pipe).Read a full minute later: lichess serves
  these streams over HTTP/2, where closing the body does not unblock a read
  already waiting on the stream's pipe. Cancelling the request's context
  does.

  The second fix was wrong in the opposite direction, and measurement
  caught it before it did damage. Treating silence as death assumed
  keepalives that are not there: curl held an idle event stream open for 75
  seconds and received zero bytes, with the connection healthy throughout,
  so the bot had begun reconnecting once a minute for nothing. Judging a
  connection is the transport's job, and it can ask rather than guess: an
  HTTP/2 ping health check, 30s to ping and 15s to answer. Commits 61c32c2,
  6c1855c and 948f7c3.
- **The search ignored the fifty-move clock and drew won games.**
  `IsFiftyMoveDraw` existed and only the training loop called it, so a
  dead drawn position evaluated as won, +2.04 in the position from the
  game that exposed it. Two games were thrown away that way, one of them
  rook and three pawns against rook and one. The evaluation now returns
  the draw once the clock is spent and fades toward it before that, and
  the search tracks the clock per ply, which it had never done: it makes
  moves straight on the board and never advanced `Game.HalfmoveClock`, so
  every node saw the root's value. Commit 7b08545.
- **That fix then paid the engine to sacrifice.** Fading all the way to
  zero means a capture, which resets the clock, restores full scaling: six
  pawns up at a fade of 0.3 scores 1.8, and giving a bishop away to reset
  scores 3.0. The engine handed a lone king a bishop in the mate test and
  lost a rook mate it had been converting. The fade now stops at half.
  Caught by the gated endgame test within the hour. Commit cc3bf1a.
- **Fast games ended with a third of the clock unspent.** Across the 13
  lost bullet and blitz games the bot never came close to flagging,
  ending a 60 second game with 10.4 s in hand. Spending is now a
  twentieth of what is left plus most of the increment, above a reserve,
  and the old cautious rule is kept when there is no increment because a
  twentieth flags over 120 moves of 1+0. Bullet 2+1 goes from 3221 ms a
  move to 3651 ms. Commit 32a5526.
- **Two threads cost a full ply.** Set so five concurrent games would fit
  ten cores, while the rate limit means one or two run. Measured with one
  game in flight: two threads reach mean depth 8.0, four and eight both
  reach 9.0. Commit c6ccaad.
- **Repetition tracking was dead on any game rebuilt from a FEN.**
  `game.New` turns it on, `game.ParseFEN` deliberately does not, and the
  bridge took the ParseFEN branch, so the move picker's only anti-shuffle
  rule was dead in those games. Commit 784b860.
- **Fifty-six search threads on ten cores.** Seven concurrent games at
  eight threads each. The bot declines past a cap now. Commit 95395eb.
- **Our own outgoing challenges were declined, and 404'd.** Lichess echoes
  them on the same stream as incoming ones and has no decline action for
  them. Fixed twice: once for a field that does not exist (59af7a9) and
  again after a check-ordering change reintroduced it (6a802af).
- **The challenger resent to one opponent forever**, fourteen in a row,
  because candidates are sorted by rating distance and an unaccepted
  challenge wins every cycle. Opponents now carry a cooldown, commit
  477115a. Then it hammered a rate limit on the same cadence, which is
  what keeps one alive: one per cycle and exponential backoff now,
  commits 968286e and 7f8c1ec.

## Investigated and not bugs

- **The parallel search plays a blunder the serial one does not.** Four
  hypotheses tested and all negative: aborted searches are already guarded
  from storing, root ordering uses each thread's own best rather than the
  table, the accumulator stack is re-pointed per thread, and the
  reproduction is clean under `-race`. Ordinary Lazy SMP
  non-determinism, and SMP measures +182 +/- 117 over one thread at
  200 ms, so it pays for itself.
- **Searches appeared to use 70% of their budget.** They use 107%. The
  mean was dragged down by book positions, which return in zero
  milliseconds because no search happens.
- **Helper threads appeared to delay each move by unwinding.** One thread
  and eight return in the same time at 30, 60 and 120 ms.
- **Blitz looked like a weak mode** at 2-2-8. Its opponents average 2404
  against rapid's 1923. Every mode's rating lands near 2200, which is the
  consistent reading.
- **"Every loss came after being two pawns up"** was an artifact of
  scoring material after every ply, which counts the spike between a
  capture and its recapture. Advantages held ten plies or more give 7 of
  36, not 29 of 29.
- **Time management in blitz.** Every control the bot plays was simulated
  through the shipped rule and none flags; 5+3 over eighty moves ends with
  5.9 s in hand.

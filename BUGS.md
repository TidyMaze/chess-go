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
challenges. Every challenge has been refused for over half an hour, so
the bot plays only when somebody challenges it. The challenger now backs
off exponentially, which is the right behaviour but does not shorten the
wait.

## Fixed

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

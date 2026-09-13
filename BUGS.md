# Bug list

Live list for the lichess bot and the engine behind it. A bug leaves here
only when it is fixed with a test that failed before the fix, or when it
is disproved with the output that disproves it.

## Open

### 1. Fast games end with a third of the clock unspent
Measured across the 13 lost bullet and blitz games: we never came close
to flagging. Minimum clock remaining, per game, at the end:

| mode | clock | least remaining |
|---|---|---|
| bullet | 60 s | 10.4 s, 14.6 s, 15.1 s |
| bullet | 120 s | 10.6 s, 34.7 s, 39.3 s |
| blitz | 180 s | 14.7 s to 84.5 s |

So a 60 second game ends with a sixth to a quarter of the clock unused,
and a 120 second one with up to a third. Unspent clock is unsearched
depth, and depth is what the losses are short of. The per move budget is
`remaining/30 + 0.8 * increment`, which is too cautious when there is an
increment to lean on.

Not a flagging risk to fix: zero games have ever been lost on time, every
loss is a checkmate.

### 2. King and two bishops cannot mate a lone king
Reproducible, deterministic, gated behind `ENDGAME=1` in
`engine/endgame_mate_test.go`. King and queen and king and rook both
convert. The cause is in `kingDrivingBonus`: it measures the bare king's
distance from the centre with a Chebyshev distance, which saturates along
an entire edge and scores a1 and a4 the same, so once the king reaches an
edge nothing points at a corner.

Low priority despite being real: the loss analysis shows the engine
rarely survives far enough ahead to convert an endgame at all.

## Fixed

- **Repetition tracking was dead on any game rebuilt from a FEN.**
  `game.New` turns it on, `game.ParseFEN` deliberately does not, and the
  bridge took the ParseFEN branch. The move picker's only anti-shuffle
  rule was therefore dead in those games. Commit 784b860.
- **Fifty-six search threads on ten cores.** Seven concurrent games at
  eight threads each. The bot now declines past a cap, default two.
  Commit 95395eb.
- **Our own outgoing challenges were declined, and 404'd.** Lichess
  echoes them on the same stream as incoming ones and has no decline
  action for them. Fixed twice: once for the field that does not exist
  (59af7a9) and once after a check-ordering change reintroduced it
  (6a802af).
- **The challenger resent to one opponent forever.** Candidates are
  sorted by rating distance, so an unaccepted challenge won every cycle:
  fourteen in a row to the same bot. Opponents now carry a cooldown.
  Commit 477115a.

## Investigated and not bugs

- **The parallel search plays a blunder the serial one does not.** Four
  hypotheses tested and all negative: aborted searches are already
  guarded from storing, root ordering uses each thread's own best rather
  than the table, the accumulator stack is re-pointed per thread, and the
  reproduction is clean under `-race`. It is ordinary Lazy SMP
  non-determinism, and SMP measures +182 +/- 117 over one thread at
  200 ms, so it pays for itself.
- **Searches appeared to use only 70% of their budget.** They use 107%.
  The mean was dragged down by book positions, which return in zero
  milliseconds because no search happens at all.
- **Helper threads appeared to delay each move by unwinding.** One thread
  and eight threads return in the same time at 30, 60 and 120 ms.

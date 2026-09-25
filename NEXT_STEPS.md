# Queue

Updated 2026-09-25. Every experiment below fits in 10 minutes (`timeout 600`).

## Where we stand

- Against Stockfish 2700 at 100 ms a move: -209 +/- 25 (1,000 games, rung 23,
  qply 4). Goal: cut the gap by 25%, to about -157.
- Adopted since: `nullpieces`, `drawscale` (both neutral), qply 16 (+17 +/- 15
  champion against champion). Not yet re-measured against Stockfish.
- 61% of losses are evaluation lag (Stockfish sees it 10+ plies first), 39% are
  sudden collapses (`tools/lossaudit/loss_phases.py`).
- Flat, do not retry without a new reason: more self-play data, from-scratch
  nets, depth 8 labels at 200k, 128 hidden units, `singular`, `histgravity`,
  `historyaging`, `drive2`, `tt_bits 20`, quiet checks in quiescence.

## Instruments

| benchmark | games | time | precision |
| --- | --- | --- | --- |
| screen: candidate vs champion, 100 ms, `openings_balanced.txt` | 540 | 10 min | +/- 27 |
| Stockfish 2700, 100 ms, 5 games in parallel | 250 | 10 min | +/- 45 |

A change is adopted when pooled screens put its lower bound above zero. The
Stockfish number is re-measured only once screens have banked about +40, and
pooled over several 10-minute runs.

## Experiments, cheapest and most likely first

1. **Loss function.** Sigmoid (win probability) against squared error, both
   from scratch on the same 3.2M positions. Running. If sigmoid wins, fine-tune
   the champion with it on a 5M slice and screen.
2. **Quiescence cap past 16.** qply 24 and 32 against 16. The cap already paid
   once; find where it stops paying.
3. **The SPSA directions, one at a time.** The 482-round SPSA read +12 +/- 21
   together; screen its three largest moves alone: LMPBase 3 to 6.5, LMRDiv 2.5
   to 2.2, NullBonusPer 1.5 to 1.15. A single parameter moves more Elo per game
   than twelve at once.
4. **SPSA, continued** in 9-minute chunks (`spsa.py JOURNAL N 40 9`) on the
   qply 16 champion, screening the result every few chunks.
5. **Stalemate at the stand-pat cutoff** (`9483013`): a failing test first,
   then the cheap legal-move check, then a screen.
6. **Stockfish check**, pooled 250-game runs, new champion against the qply 4
   control on the same openings.

## Decision still open

Letting Stockfish label training positions is the one lever that targets the
61% evaluation lag directly. The project rule is that Stockfish judges and
never teaches; nothing above breaks it.

## Harness debt

`scripts/chunked_match.sh`, `ladder.sh`, `screen.sh` still write to
`/tmp/chesslogs` (wiped by the 2026-09-25 reboot), and three of them pass the
`-halfkp` flags `gauntlet-bin` now refuses.

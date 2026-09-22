# Why the champion loses and draws against Stockfish 2700

200 games, 1 s per move, one thread each side, `openings.txt` from offset 70000,
2026-09-22. Champion rung 23 (before `nullpieces`). Games in
`/tmp/chesslogs/sf2700_baseline_games.jsonl`.

**W-D-L 70-37-93, score 0.443, -40 +/- 49.** Win rate 35%.

## Losses: the evaluation, not tactics

Every one of the 93 losses was seen coming: our own score fell below -1.5
before the end. 71 collapsed in the middlegame (median ply 48, 20 men on the
board), 15 in the opening, 7 in the endgame.

In 55 of the 93, Stockfish scored the position above +1.5 ten plies or more
before we noticed (median lag 15 plies). When Stockfish first said +1.5, our
own score was +0.5 for us. We walk into lost positions believing they are
fine, which is an evaluation defect, not a missed tactic.

## Draws: wins thrown away

22 of the 37 draws were games Stockfish itself scored at -1.5 or worse for
itself. They ended by the 50-move rule (9), by adjudication after both sides
agreed on zero (10), by repetition (2) and by stalemate (1). Converting them
would take the win count from 70 to about 92.

The move that let each one go is listed in `/tmp/chesslogs/missed_wins.txt`.
Most are endgames where we score +3 to +6 and Stockfish scores a dead draw:
a mad rook sacrificing itself into stalemate, bishop and pawn against a
blocking knight, opposite-coloured bishops, rook and piece against rook.
Only two of the 22 match the simple draw rules (opposite-coloured bishops,
pawnless material edge under a bishop), so hand rules alone do not fix it.

## What was fixed

- `nullpieces` (adopted, `36c3b56`): no null move for a side with under two
  pieces. Null move, razoring and reverse futility all run before any
  legal-move check, so a side with a lone piece "passed" instead of being
  found in zugzwang. It wins the queen-trap ending drawn on lichess
  (UAuzblWw). +2 +/- 15 at depth 4, +9 +/- 21 at 100 ms.

## Where the Elo is

The network imitates a depth-4 search, and that search has the same blind
spots as the network, so self-play at label depth 4 cannot teach what it
cannot see. The next experiment labels the same real games at depth 4 and at
depth 8 and races the two resulting networks.

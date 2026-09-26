# Lost wins sweep, TidyMazeBot lichess games (2026-09-26)

**Outcome:** 15 of our 163 non-won games had a position Stockfish scored at +4 or better (or a mate) for us. 9 of those 15 were thrown away by the bot not moving at all (loss or draw on time with 28 s to 348 s on the clock), 2 by the fifty-move rule, 4 by a blunder. No win was lost to threefold repetition, stalemate or insufficient material as the primary cause. The sweep also turned up two new bugs with failing tests: an opponent's underpromotion desyncs the bot's board (3 games lost on time, 1 of them with mate in 6 on the board), and a game-long transposition table makes the bot skip a mate in 1.

## Data and method

- Games: the 300-game export `bot_games_300.ndjson` (2026-09-16 20:34 to 09-23 11:54, 161 not won) plus the 4 games played since the bot restart on 09-26 (2 not won). The user export `api/games/user/tidymazebot` answered `404 {"error":"Not found"}` today, so those 4 were fetched one by one from `game/export/{id}`, with ids taken from `lichessbot.log`.
- Coarse pass: Stockfish, 0.05 s, 1 thread, on every 4th position with us to move plus our last 3. That came to 3,075 positions, 78 s wall clock (09:40:59 to 09:42:17).
- Dense pass: every one of our moves, before and after, from 4 moves before the first +4 sample to the end (13 games). Near misses (coarse max from +1.50 to +3.99, 23 games) were then re-sampled densely within 8 plies of each hot sample, which flagged 2 more (`NOwauSF0`, `ocqCKkgH`).
- Classification: the key positions were re-checked with Stockfish at 1 s. The "swing" column below gives those 1 s scores, from our side.
- Scripts: [lost_wins_sweep.py](lost_wins_sweep.py) and [lost_wins_report.py](lost_wins_report.py). Raw results: `~/work/analyses/chess-go/logs/lost_wins_{sweep,refine}.jsonl` and `lost_wins_classified.json`.

## Flagged games

| game | date | speed | result | cause | move | eval swing (SF 1 s, us) | our clock |
|---|---|---|---|---|---|---|---|
| [ocqCKkgH](https://lichess.org/ocqCKkgH) | 09-17 | bullet 2+1 | draw, threefold 54 plies later | blunder | 129. Rf3+ | +9.11 → +0.06 | 23.9 s |
| [RvOKnazi](https://lichess.org/RvOKnazi) | 09-18 | bullet 2+1 | loss, mate | blunder, gradual (22. Qe4, 23. Rb8, 24. Qxe5) | 24. Qxe5 | +3.25 → +0.48 (peak +5.40 at move 22) | 64.7 s |
| [0eDUqd2W](https://lichess.org/0eDUqd2W) | 09-19 | bullet 2+1 | loss on time | flag/time forfeit | none, stalled on move 34 | #6 at the stall | 53.7 s |
| [toE5toS5](https://lichess.org/toE5toS5) | 09-19 | rapid 5+7 | loss on time | flag/time forfeit | none, stalled on move 45 | +6.80 at the stall | 170.3 s |
| [0KP46fpN](https://lichess.org/0KP46fpN) | 09-19 | bullet 2+1 | loss on time | flag/time forfeit | none, stalled on move 37 | +8.96 at the stall | 30.6 s |
| [6POlSzc0](https://lichess.org/6POlSzc0) | 09-19 | bullet 2+1 | draw: we flagged, opponent could not mate | flag/time forfeit | none, stalled on move 65 | #5 at the stall | 33.5 s |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) | 09-20 | rapid 15+2 | loss on time | flag/time forfeit | none, stalled on move 39 | +6.99 at the stall | 38.4 s |
| [43PSibRm](https://lichess.org/43PSibRm) | 09-20 | classical 25+2 | loss on time | flag/time forfeit | none, stalled on move 24 | +5.01 at the stall | 282.7 s |
| [5P43FPRa](https://lichess.org/5P43FPRa) | 09-21 | blitz 3+2 | draw, insufficient material | blunder (K+P vs K) | 50. g5 (Kg5 wins) | #19 → 0.00 | 90.7 s |
| [GECAtSBW](https://lichess.org/GECAtSBW) | 09-21 | rapid 10+5 | draw, insufficient material | blunder (pawn race) | 56... g4 | +6.90 → 0.00 | 309.2 s |
| [qaOCjwPE](https://lichess.org/qaOCjwPE) | 09-21 | rapid 10+5 | loss on time | flag/time forfeit | none, stalled on move 36 | +5.60 at the stall | 347.7 s |
| [ymteJEvj](https://lichess.org/ymteJEvj) | 09-22 | blitz 3+1 | loss on time | flag/time forfeit, **after the opponent's 43. f8=N+ (bug 1)** | none, stalled on move 43 | #6 at the stall | 63.0 s |
| [NOwauSF0](https://lichess.org/NOwauSF0) | 09-23 | bullet 1+1 | loss on time | flag/time forfeit, win already faded | none, stalled on move 51 | +1.17 at the stall (peak +5.83 at 44... Re1+) | 28.0 s |
| [sXWkqol4](https://lichess.org/sXWkqol4) | 09-23 | blitz 3+0 | draw, fifty-move | fifty-move (R+N vs K never mated) | mate on the board from move 59; **109... Rf5 with Rf8# available (bug 2)**; last SF win 126... Ng1 | #13 at move 126 → draw at move 136 | 38.4 s at move 126, 45.3 s at move 109 |
| [lhT8MqvU](https://lichess.org/lhT8MqvU) | 09-26 | bullet 2+1 | draw, fifty-move | fifty-move | 105. Bf8 (100th halfmove) | #6 → 0.00 | 26.1 s |

## Counts per cause (15 flagged of 163 non-won)

| cause | games | of which |
|---|---|---|
| flag/time forfeit (bot stopped moving) | 9 | 1 caused by the opponent's underpromotion (`ymteJEvj`); 1 where the win had already faded to +1.17 (`NOwauSF0`) |
| blunder | 4 | 2 ended in insufficient material, 1 in threefold, 1 in mate |
| fifty-move | 2 | both are the mate-distance / fifty-move bugs being fixed in the other worktree |
| threefold repetition | 0 | 27 threefold draws in the export, none after a +4 position we still held |
| stalemate | 0 | the one stalemate draw (`eDC8WcGI`) never went above +0.73 for Stockfish, although we were a piece up |
| insufficient material | 0 as a cause | the 2 games that ended this way were lost to the blunder before |
| abandonment | 0 | |

## What the causes point at

- **Stalls.** I replayed all 56 games we lost on time (all 592 exported games) to their final position, with us to move. Through the bot's own game loop and a fake lichess API, 53 get a legal move. Through the UCI wrapper with the bot's config at 1 s, all 53 answer within 1.18 s. So those stalls are not caused by the position. That fits `analyses/bot-elo/games-audit.md`: the whole process froze. The other 3 are exactly the 3 games where the opponent's last move was an underpromotion (bug 1).
- **Underpromotion (bug 1).** `lichessbot/state.go:84` parses each opponent move with `game.MoveFromUCI`, which drops the promotion letter, and `game/game.go:359` always makes a queen. After `f7f8n` the bot sees a queen on f8, does not see that it is in check, and posts `h3h1`, which lichess rejects. `bot.go:381` logs the error and returns, so the clock runs out. After `g2g1b` the bot sees a stalemate and posts nothing. 13 of the 592 exported games had an opponent underpromotion, and in all 3 where it was the last move before our turn, we lost on time.
- **Mate in 1 skipped (bug 2).** Replaying `sXWkqol4` the way the bot does (one table for the whole game, `bot.go:230`), the engine scores the non-mating `h5f4` at 1012.00 against 1011.00 for `f3f8#`. That is a stale mate score from an earlier move's search. With a fresh table it plays `f3f8`. Same root cause as the `terminalScore` fix in flight, but through the table the bot keeps across moves. The fix is only complete if this replay passes.
- **Blunders.** At the budget the bot would give today, current master avoids 3 of the 4 (checked with `uci-botcfg.sh`). It plays Kg5 in `5P43FPRa` at 1 s and 3 s. In `GECAtSBW` it plays f4e5 at 3 s, where the bot's budget at 309 s + 5 s is about 11.9 s. In `ocqCKkgH` it plays Kd2 at 1 s. `RvOKnazi` is middlegame tactics in bullet and was not chased further.

## Reproduce

```
cd ~/work/analyses/chess-go/logs
taskpolicy -b ~/chess-go/.venv/bin/python ~/chess-go/analyses/lichess-games/lost_wins_sweep.py --games bot_games_300.ndjson bot_games_sweep.ndjson --out lost_wins_sweep.jsonl
taskpolicy -b ~/chess-go/.venv/bin/python ~/chess-go/analyses/lichess-games/lost_wins_sweep.py --games bot_games_300.ndjson bot_games_sweep.ndjson --out lost_wins_refine.jsonl --refine-from lost_wins_sweep.jsonl
cd ~/chess-go/analyses/lichess-games && ~/chess-go/.venv/bin/python lost_wins_report.py --games ~/work/analyses/chess-go/logs/bot_games_300.ndjson ~/work/analyses/chess-go/logs/bot_games_sweep.ndjson --sweep ~/work/analyses/chess-go/logs/lost_wins_sweep.jsonl ~/work/analyses/chess-go/logs/lost_wins_refine.jsonl --json-out ~/work/analyses/chess-go/logs/lost_wins_classified.json
```

`taskpolicy -b` keeps Stockfish on the efficiency cores, so the live bot keeps the performance cores.

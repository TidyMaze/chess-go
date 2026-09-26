# lhT8MqvU: does the mate-distance and fifty-move fix convert wins?

Yes. From the game's own positions the fixed engine mates 6 times out of 6 against Stockfish, master mates once. On the won-endgame suite the fixed engine converts 15 of 20, master 1 of 20. Four of the fixed engine's five misses are positions that are drawn by the fifty-move rule with best defence (tablebase mate distance longer than the 19 plies left at clock 80). The fifth is KBNK from clock 0, which it did not mate in 50 moves.

## Setup

| | master | fixed |
|---|---|---|
| binary | `~/work/analyses/chess-go/logs/uci-master` (sha256 `db96ef5634e6`) | `~/work/analyses/chess-go/logs/uci-fixed` (sha256 `3b94be0f9f03`, copy of the worktree `uci-bin`) |
| built from | `15973a7` (master) | `cc4996f` (branch `worktree-wf_ac630aef-e8d-1`, clean tree) |
| config | `champ_bot_analysis.json` | `champ_bot_analysis_fixed.json` (byte-identical copy) |
| wrapper | `uci-botcfg.sh` | `uci-fixed-bot.sh` |

- Config: `champion_bot.json` with `book ""` and 4 threads. Each wrapper runs `cd /Users/yann.rolland/chess-go` first, because `net_file` is relative.
- Our engine plays the side to move at `go movetime 1000`, with a fresh table (`ucinewgame`) for every game.
- Stockfish 19 (`/opt/homebrew/bin/stockfish`) defends at full strength, 0.2 s a move. The "Stockfish at start" column is its own 1 s analysis of the start position.
- A game ends on mate or on the draws Lichess applies by itself:
  - fifty moves, at halfmove clock 100. A mate delivered by the move that reaches 100 still counts.
  - threefold repetition, stalemate, insufficient material.
- Ply cap: 100 plies for test 1, 200 for the suite.
- Heads up on load: other agents' Go test runs kept the load average between 10 and 17 on 10 cores during the runs. One sample showed our engine at 163% CPU instead of 400%. Both engines ran under the same load, one after the other on each start position, so the comparison stays paired. Absolute depths are lower than on an idle machine.

## Test 1: the game from ply 109, our engine White, real halfmove clock

Every 10th White-to-move position from ply 110, plus the last one (ply 208, clock 99). That gives 6 of the 50.

| start | clock | Stockfish at start | master | fixed | master fifty-move draw | fixed fifty-move draw |
|---|---|---|---|---|---|---|
| ply 110 | 1 | mate 6 | draw: fifty after 99 plies | mate in 6 | yes | no |
| ply 130 | 21 | mate 5 | draw: fifty after 79 plies | mate in 5 | yes | no |
| ply 150 | 41 | mate 4 | mate in 4 | mate in 4 | no | no |
| ply 170 | 61 | mate 4 | draw: threefold after 29 plies | mate in 4 | no | no |
| ply 190 | 81 | mate 3 | draw: fifty after 19 plies | mate in 3 | yes | no |
| ply 208 | 99 | mate 6 | draw: fifty after 1 plies | mate in 6 | yes | no |

- Converted: master 1/6, fixed 6/6.
- The fixed engine mates in exactly Stockfish's announced distance every time.
- Why master wanders, from its own scores at ply 110: `cp 100500, 100700, 101100, 101100, ...` for 50 moves. That is a remaining-depth mate score, flat, never counting down.
- The fixed engine's scores from the same start: `mate 6, mate 5, mate 4, mate 3, mate 2, mate 1`.
- At ply 208 (clock 99), master played a quiet move and the game was drawn on the spot. The fixed engine played `d5d6`, which resets the clock, and mated in 6.

## Test 2: won-endgame suite, halfmove clock 0 and 80

Our engine plays the winning side, Stockfish defends. "Tablebase" is the exact mate distance from Lichess's tablebase (`tablebase.lichess.ovh`), in plies, with best play from both sides. It is only a judge here, never an input to the engine. From clock 80, a position without pawns needs a mate within 19 plies (10 White moves).

| start | clock | tablebase | Stockfish at start | master | fixed | master fifty-move draw | fixed fifty-move draw |
|---|---|---|---|---|---|---|---|
| KQK | 0 | mate 17 plies | mate 9 | draw: threefold after 53 plies | mate in 15 | no | no |
| KQK | 80 | mate 17 plies | mate 9 | draw: fifty after 20 plies | mate in 9 | yes | no |
| KRK | 0 | mate 29 plies | cp 7485 | draw: threefold after 57 plies | mate in 18 | no | no |
| KRK | 80 | drawn (29 > 19) | cp 11 | draw: fifty after 20 plies | draw: fifty after 20 plies | yes | yes |
| KBBK | 0 | mate 33 plies | cp 500 | draw: fifty after 100 plies | mate in 14 | yes | no |
| KBBK | 80 | drawn (33 > 19) | cp 42 | draw: fifty after 20 plies | draw: fifty after 20 plies | yes | yes |
| KBNK | 0 | mate 59 plies | cp 204 | draw: threefold after 69 plies | draw: fifty after 100 plies | no | yes |
| KBNK | 80 | drawn (59 > 19) | cp 44 | draw: fifty after 20 plies | draw: fifty after 20 plies | yes | yes |
| KRBK | 0 | mate 23 plies | mate 15 | draw: fifty after 100 plies | mate in 13 | yes | no |
| KRBK | 80 | drawn (23 > 19) | cp 212 | draw: fifty after 20 plies | draw: fifty after 20 plies | yes | yes |
| KQ+P vs K | 0 | mate 17 plies | cp 2111 | no mate in 200 plies (clock 73) | mate in 8 | no | no |
| KQ+P vs K | 80 | win (pawn resets) | cp 1449 | draw: fifty after 101 plies | mate in 10 | yes | no |
| KR+2P vs KP | 0 | 6 men, not queried | cp 946 | draw: fifty after 113 plies | mate in 13 | yes | no |
| KR+2P vs KP | 80 | 6 men, not queried | cp 777 | draw: fifty after 117 plies | mate in 12 | yes | no |
| game ply 110 | 0 | 9 men | mate 6 | draw: fifty after 100 plies | mate in 6 | yes | no |
| game ply 110 | 80 | 9 men | mate 6 | draw: fifty after 20 plies | mate in 7 | yes | no |
| game ply 150 | 0 | 9 men | mate 4 | mate in 5 | mate in 4 | no | no |
| game ply 150 | 80 | 9 men | mate 4 | draw: fifty after 20 plies | mate in 4 | yes | no |
| game ply 208 | 0 | 9 men | mate 6 | draw: threefold after 37 plies | mate in 6 | no | no |
| game ply 208 | 80 | 9 men | mate 6 | draw: fifty after 101 plies | mate in 6 | yes | no |

| engine | clock | converted | winnable | games |
|---|---|---|---|---|
| master | 0 | 1 | 10 | 10 |
| master | 80 | 0 | 6 | 10 |
| fixed | 0 | 9 | 10 | 10 |
| fixed | 80 | 6 | 6 | 10 |

- "winnable" counts the positions a perfect player converts inside the fifty-move limit. At clock 80, KRK, KBBK, KBNK and KRBK are not winnable: their tablebase mate distance is longer than 19 plies, and Stockfish stops seeing a win there (cp 11 to 212, against mate or much higher scores at clock 0).
- The fixed engine converts every winnable clock-80 position.
- The one real miss left is KBNK from clock 0. The mate is 59 plies away, and the fixed engine ran into the fifty-move draw after 100 plies without mating. I did not investigate the cause. Stockfish at 1 s does not see this mate either (cp 204).

## Other findings

- `engine/uciserver.go` prints a promotion without its piece (`bestmove d7d8`, from `game.Move.UCI()`). python-chess rejects that as illegal, and it crashed the first smoke run at `8/3P1R2/k3K1p1/6P1/1p1B4/1P6/1P6/8 w - - 3 109`.
  - The Lichess bot is not affected: `lichessbot/state.go` `moveUCIForLichess` appends `q` itself.
  - The harness does the same thing in `parse_bestmove`.
  - Any other UCI GUI talking to `uci-bin` would reject the move. Master has the same behaviour.
- Master is not deterministic at 4 threads. From ply 208 (clock 99), a smoke run played `f6g6` (Rxg6, which resets the clock) and then went 100 plies without mating. The campaign run played a quiet move and drew at once.

## Reproduce

```
cd /Users/yann.rolland/chess-go/tools/lossaudit
L=/Users/yann.rolland/work/analyses/chess-go/logs
PY=/Users/yann.rolland/chess-go/.venv/bin/python
timeout 600 $PY convert_check.py game $L/convert_check.jsonl --pgn ../../analyses/lichess-games/lhT8MqvU.pgn \
  --engine master=$L/uci-botcfg.sh --engine fixed=$L/uci-fixed-bot.sh
timeout 600 $PY convert_check.py suite $L/convert_check.jsonl --engine master=$L/uci-botcfg.sh \
  --engine fixed=$L/uci-fixed-bot.sh --clocks 0,80 --max-plies 200   # rerun until "0 games to play"
$PY convert_check.py report $L/convert_check.jsonl
```

- The whole campaign is `$L/convert_check_run.sh`, and its log is `$L/convert_check.log`.
- The run times were:
  - test 1: 3 min (11:06 to 11:09);
  - suite: two runs, 10 min (cut at 600 s, resumed) and 7.7 min.
- Every game is in `$L/convert_check.jsonl`: 52 rows, with moves, per-move scores and depth, the highest clock reached, and wall time.

## Second game: hVG5pJcz (we are Black, rook and a-pawn against king and a-pawn)

Same bug, fifty-move draw at ply 226. `convert_check.py game ... --side black --from-ply 126 --every 20`, 1 s a move against Stockfish:

| start | clock | Stockfish at start | master | fixed |
|---|---|---|---|---|
| ply 127 | 1 | mate 9 | draw: fifty after 99 plies | mate in 8 |
| ply 167 | 41 | mate 8 | no mate in 100 plies | mate in 8 |
| ply 207 | 81 | mate 11 | draw: fifty after 19 plies | mate in 11 |
| ply 225 | 99 | cp 0 | draw | draw |

Master converts 0 of 4, the fixed engine 3 of 4 (the fourth is a draw for Stockfish too).

# tidymazebot: levers ranked by rating gain per hour (2026-09-26)

**Outcome.** Stop the Mac sleeping first: about +24 bullet (+33 overall) for five minutes of work, no restart. Put every other change that needs a restart into one request to the user. Then let the new engine play 150 bullet games before doing anything else on the lichess side. After that, engine strength is the only lever that raises the ceiling. **+25% (2730 bullet) is not feasible**; see the verdict at the end.

Sources: [games-audit.md](games-audit.md), [runtime-audit.md](runtime-audit.md), [reachable-rating.md](reachable-rating.md). Numbers not taken from those files were checked for this plan and are marked "checked today".

**HEADS UP (checked today, 09:38):** the challenger is not running. PID 79952 is gone from `ps`, and the last line in `/tmp/lichess_recent_79952.txt` was written about 09:35. With no challenger, the bot only plays challenges that come to it. The token lives only in the process, so only the user can restart it. Do that together with step 2 below.

## Ranking

| # | lever | expected gain | work | gain per hour | needs a restart |
|---|---|---|---|---|---|
| 1 | Keep the Mac awake while the bot runs | **+24 bullet**, +31 blitz, +45 rapid, +33 overall | 5 min | about +300/h | no (durable version: yes, bundled into 2) |
| 2 | One restart bundle: `-max-games 2`, nice 0, log appended instead of overwritten, launch under caffeinate | ⚠ +0 to +15 bullet, interval includes 0 | 15 min | ⚠ 0 to +60/h | yes, one request to the user |
| 3 | Stop playing FataliiBot | ⚠ +5 to +10 overall | 1 h | ⚠ +5 to +10/h | challenger yes (it is down anyway), bot yes (bundle into 2) |
| 4 | Engine strength rounds | +58 and +90 per good round at 100 ms against Stockfish; ⚠ how much reaches lichess is unmeasured | 4 to 7 h per round | ⚠ about +8 to +20/h when a round lands, near 0 when it doesn't | yes, per adopted champion |
| 5 | Spend more of the clock | ⚠ unmeasured | 2 to 3 h | ⚠ unknown until the slope screen runs | yes |
| 6 | Convert won endgames | ⚠ at most about +5 | several hours | ⚠ about +1/h | yes |

Not worth doing now: tt_bits 23 (rapid only, unmeasured), `time_ms` (never used in a clocked game, bot.go:325-337), lowering the 150 ms overhead floor (p99 post is 359 ms), 8 threads (same depth as 4 with one or two games in flight, BUGS.md and c6ccaad), resigning (a loss costs the same rating), and playing more games (the rating settles within about 150 games, and lichess caps bot-vs-bot games at 100 a day).

## 1. Keep the Mac awake

**Evidence.**
- 54 of the 55 losses on time were stalls: one move ran 28 s to 1028 s past its budget, and at the flag we still had 28 s to 605 s on the clock.
- The two stalls covered by a log are proven sleep. 1q6VdN8S shows 18m29s of wall time against 3m33s on the monotonic clock, and jVzbwbGF shows 18m53s against 2m52s.
- 8 of the 44 stall events froze 2 to 4 games in the same second.
- `pmset` has `sleep 1` on AC and on battery. The machine is on battery (93%, checked today). The only thing holding sleep off is Claude Code's `caffeinate -i -t 300`, which has nothing to do with the bot.
- Rating cost: -256 points across 33 rated games in the last 300 games. Counting each stalled game at its pre-game expected score gives +33 overall.

**Change.**
- Now, with no restart and no signal sent to the bot: `caffeinate -i -s -w 77225 &`. Plug in the charger and keep the lid open, because `-s` only works on AC and a closed lid (clamshell) sleeps regardless.
- Durable fix at the next restart: launch the bot as `caffeinate -i -s ./lichessbot-bin ...` (see step 2). This needs no code.
- Optional, the user's call because it needs sudo: `sudo pmset -c sleep 0`.

**Verify.**
- `pmset -g assertions | grep caffeinate` lists the `-w 77225` holder.
- `pmset -g log | grep -E ' Sleep '` has no entry while games are in flight.
- On the next 100 games, `games_audit.py` counts 0 "hang" flag losses and `runtime_audit.py` counts 0 stalls.

**HEADS UP.** caffeinate cannot block a thermal emergency sleep. There were two at 09-26 01:02. That was during the round-1 engine work (commits at 01:57 and 02:11), but the cause is not established. See the scheduling rule under step 4.

## 2. One restart bundle (ask the user once)

**Evidence.**
- With 3 or more games in flight, bullet scored -0.130 against expectation (n=25, ±0.173, about -91 Elo) and blitz -0.117 (n=24, ±0.175, about -84). With 1 or 2 games in flight there is no difference.
- 3 or more games covered 9.4% of busy time, which puts about 16% of bullet games under that load. If the -91 holds, that is about -15 bullet.
- ⚠ games-audit section 8 sees no concurrency cost on a different split, so the lower bound is 0.
- The bot runs at NI 10, and every job started from the Claude Code shell (NI 5) outranks it.
- The bot log is overwritten on each restart, which is why 52 of the 54 stalls cannot be tied to a cause.

**Change.** One message to the user, with both commands:

```
cd ~/chess-go && LICHESS_BOT_TOKEN=... caffeinate -i -s ./lichessbot-bin -champion champion_bot.json \
  -username tidymazebot -max-games 2 >> ~/work/analyses/chess-go/logs/lichessbot.log 2>&1
LICHESS_BOT_TOKEN=... MODES=bullet scripts/lichess_challenger.sh 2
```

- Start both from a normal terminal (nice 0), not from a Claude Code shell.
- `MODES=bullet` spends the 100-a-day quota on the mode being chased, so the 150-game measurement below takes about 1.5 days (the bullet cap is reached in about 4.7 h a day).
- Until the restart: `sudo renice -n 0 -p 77225` (no signal sent; the user types the sudo password).

**Verify.**
- `ps -o ni -p <pid>` prints 0.
- `/api/account/playing` never shows more than 2 games.
- The log line count only grows across restarts.
- Runtime-audit R2, if wanted: log completed depth and nodes per search, returned by the search rather than read from the shared globals, then compare depth with 1, 2 and 3+ games in flight.

## 3. Stop playing FataliiBot

**Evidence.**
- 36 rated games: 3 wins, 3 draws, 30 losses. That is a 12.5% score against 33.2% expected, a 1997 performance.
- Not counting stalls, those games cost -45 points in the last 300 (-68 over all games).
- Checked today: 10 of our 20 Four Knights games were against FataliiBot, and we lost 8 of them.
- Checked today: all 36 games differ within the first 30 plies, so FataliiBot is not replaying one winning line against a deterministic book.
- The challenger challenged `fataliibot` itself at about 09:35 today (`/tmp/lichess_recent_79952.txt`).

**Change.**
- In `scripts/lichess_challenger.sh`, add a `SKIP` env var whose ids are written into `$CAPPED` at startup. It ships with the challenger restart in step 2.
- In the bot's accept filter (`lichessbot/state.go`, next to the correspondence rule at lines 40-56), decline challenges from ids in a skip list. Write the test in `state_test.go` first and see it red.

**Value.** The ledger gives -45 over 262 rated games, about 0.017 of score per game, or about +12 at equilibrium. ⚠ FataliiBot was picked after the fact as the worst opponent, so expect some regression to the mean: +5 to +10. This buys rating, not strength.

**Verify.**
- The test is red without the filter and green with it.
- The challenger log never prints `challenged fataliibot`.
- The bot log shows its challenges declined.

## 4. Engine strength (the only lever that raises the ceiling)

**Evidence.**
- Round 1 took 3bea0fc about 4 h of commits (09-25 23:19 to 09-26 03:10) for +58. Round 2 took 15973a7 4.4 h (03:10 to 07:34) and moved -265 to -175 on paired openings, +90.
- ⚠ The two days before those rounds (09-23 to 09-25) mostly read neutral: drawscale was noise, SPSA over 6000 games was neutral, data volume was flat, and three search features were neutral. Gains come in lumps.
- Engine gains do show up on lichess: rungs 12 to 23 (09-19 to 09-22) performed at 2287 over 105 games, against 2188 and 2165 before.
- ⚠ The current binary's lichess strength is unmeasured (1 rated game at the time of the snapshot).

**Change.** Next rounds, as in the 3bea0fc and 15973a7 docs.

**Verify, within the offline caps.**
- Race the champion against itself first. Memory: `-opening-plies 0` fabricates 40 Elo.
- Then a paired-openings race against Stockfish `UCI_Elo 2700` at 100 ms a move under `timeout 600`. Adopt a change only if it gains more than 2 SE.
- Ask the user to restart, then read the lichess performance over 150 bullet games.

**Scheduling rule.** A 10-minute, 540-game screen fills every core, and the bot at NI 10 loses those cores to it. Run screens only when no rated game is in flight, for example after the day's bullet cap is reached (about 4.7 h of play at 2 in flight). That leaves about 19 h a day for engine work.

**Gate before any further lichess-side work.** After the step 2 restart, let 150 bullet games run (SE about 26). Then re-run `games_audit.py`, `runtime_audit.py` and `glicko_projection.py` on a fresh export. Their performance number is the new engine's equilibrium, and every later lever is measured against it.

## 5. Spend more of the clock

**Evidence.**
- The rule settles with about 24 s of clock untouched at 120+1.
- Lost bullet games end with a median of 30 s on the clock.
- At our move 40, 40% of the initial time is left.
- Budgets at 120+1 are 4.12, 2.57 and 1.70 s at moves 1, 20 and 40.
- The rule is sound: no flag from time trouble since 09-13, and 0 of 300 games went under 10 s.

**Change.** Make the reserve 2 s + 2×inc instead of 2 s + 5×inc, or use 25 moves to go instead of 30 (`lichessbot/state.go:238-272`). That gives about 15 to 30% more time per move.

**Verify, in this order.**
1. The floors in `lichessbot/clocksim_test.go` still hold.
2. `cmd/clockaudit` replays the exported real games under both rules, and the new rule's lowest clock stays safely above 0.
3. A fixed-time slope screen, 50 ms against 60 ms a move (+20%), paired, under `timeout 600`. Elo per unit of time shrinks with depth, so this gives an upper bound on the gain at 1 to 4 s a move.
4. Ship only if that bound is worth a restart. Then compare 150 lichess bullet games in a window with no engine change, against the gate number.

## 6. Convert won endgames

**Evidence.**
- 5 draws in the last 300 games where we were 2 or more points of material up. Three were fifty-move draws, one a stalemate and one a flag against insufficient material, for example sXWkqol4 (+8, fifty-move rule) and eDC8WcGI (+9, stalemate).
- Turning all 5 into wins would be about 2.5 points over 262 games, roughly +6. Expect less.
- drawscale (03bd53a) already targets endgames the side ahead cannot win. Tablebases are out under the no-precomputed-evaluations rule.

**Verify.** A test suite of these positions, then the step 4 race. Parked behind 1 to 4.

## Feasibility of +25% (2730 bullet from 2183): no

- The rating is already at the engine's strength. The last 100 bullet games show a 2179 performance (45/10/45, opponents averaging 2189) against a 2183 rating. More games do not raise it; only strength does.
- Holding 2730 means scoring 50% against saxtonengine, pawn_git, duchessai and grail-bot (mature NNUE engines), or 95.9% against today's opponents. That needs about **+551 of real strength**. ⚠ The CCRL bridge (self-reported bios, mixed hardware) puts it at about 360 above us at 100 ms.
- Levers 1, 2, 3 and 5 together add roughly +30 to +50 bullet. ⚠ Only lever 1 is measured. That takes the older engine to about 2210 to 2230.
- At +58 to +90 per good round, +551 is 6 to 9 good rounds in a row, with no diminishing returns and every point carried over to lichess. The two days before the last two rounds produced about nothing.
- The number of games is not what limits us: a truly 2780 engine would reach 2730 in 177 games (1.8 days at the cap).
- **Realistic:** 2250 to 2300 bullet within a week if round 2 carries over (⚠ bullet from 09-21 to 09-26 was 2279, n=12, ±93). 2400 (+10%) is a stretch that needs about +220 of real strength, about 1.5 times what the last two rounds gave. 2730 is not a goal we can plan for.
- Side note: rapid is already the strongest mode (2317 performance, 2415 with the time losses removed, an upper bound). If the goal is "a rating", rapid is closer to 2400 than bullet is.

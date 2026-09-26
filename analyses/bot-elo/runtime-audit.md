# Bot runtime audit: clock, cores, stalls

2026-09-26. Read-only audit of how `lichessbot-bin` (PID 77225, `-max-games 5`, champion_bot.json: threads 4, time_ms 1000, tt_bits 22) spends its clock and this machine.

Inputs:
- Code at master 15973a7: `cmd/lichessbot/main.go`, `lichessbot/{bot,state,client,events}.go`, `engine/{champion,elo,search2,tt}.go`, `scripts/lichess_challenger.sh`.
- Logs: `~/chess-go/lichessbot.log` (09-21 23:58 to 09-22 03:38, 256 move lines, 6 games) and `~/work/analyses/chess-go/logs/lichessbot.log` (today 09:19 to 09:28, 183 move lines, 3 games).
- Game export with per-ply clocks: `~/work/analyses/chess-go/logs/bot_games_all.ndjson`, snapshot of 592 games, created 09-13 13:23 to 09-23 11:54 (another agent was still writing it; nothing after 09-23 is covered).
- `pmset -g`, `pmset -g log` (retained from 09-25 09:05 only), `ps`, `top`.
- Script: [runtime_audit.py](runtime_audit.py), full output: [runtime_audit_output.txt](runtime_audit_output.txt). Its budget function is a port of `moveTimeBudget`, validated against the budgets the bot logged: 216/256 exact to 1 ms, 248/256 within 50 ms, max 196 ms (moves after a slow post raised the per-game overhead estimate, which the port holds at 150 ms).

## Verdict

1. **54 of the 55 games lost on time were stalls, not time management.** One move sat for 28 s to 1028 s past its budget, often in 2 to 4 games at the same second. The two stalls a log covers are proven Mac sleep. Valuing each stalled game at its pre-game expected score, they cost **about +33 Elo overall: +24 bullet, +31 blitz, +45 rapid**. The Mac is set to sleep after 1 minute idle on both AC and battery, and right now only Claude Code's own `caffeinate -i -t 300` holds it awake.
2. **The clock rule itself is sound.** No game has bled to a flag since the 09-13 fix; the clock settles well above zero in every control. It under-spends rather than over-spends: bullet games end with about 30 s unused.
3. **Concurrency above 2 games costs strength in fast games.** With 3+ games in flight, bullet and blitz scored about 85 to 90 Elo below expectation (n=24 and 25, wide interval); 1 versus 2 games shows no difference. Recommend `-max-games 2`, threads 4, challenger WANT 2.
4. **The bot runs at nice 10, with no pinning or QoS.** Any other job at nice 0 to 5 on this machine takes cores from it.

## 1. Per-move budget from the lichess clock

Path: every `gameState` carries `wtime/btime/winc/binc`. `effectivePlayer` (bot.go:325-337) replaces `TimeBudget` with `moveTimeBudget(...)` whenever the clock is present, so **`time_ms 1000` from champion_bot.json is never used in a clocked game**. It only survives when lichess sends no clock for a non-correspondence game; correspondence gets 15 s (state.go:128, bot.go:330-335) and is declined anyway (state.go:51-53).

`moveTimeBudget` (state.go:219-300):
- reserve = 2000 ms + 5 x increment, capped at half the clock (state.go:251-265). Never spent.
- budget = 0.5 x increment + (clock - reserve) / 30 - overhead (state.go:238, 241, 272); /80 instead of /30 with no increment (state.go:246, 269-271).
- overhead = per-game EMA of measured move-post latency, floor 150 ms, cap 1500 ms, rises with weight 0.6 and falls with 0.2 (state.go:176-217, fed at bot.go:380).
- ceiling max(15 s, clock/50) (state.go:143-146, 273-279), floor 50 ms (state.go:260, 280), never more than clock - 200 ms (state.go:259, 283-291), absolute cap 30 s (state.go:295-298).
- In the search: iterations stop once the budget is spent (search2.go:1487-1489), and a hard abort fires at 1.05 x budget (search2.go:1443-1448), checked every 2048 nodes for budgets over 250 ms (search2.go:1371-1382, 701-708). The comment says the hard abort "is the usual way a move ends".

Move-1 budgets by formula (match the logged values): 120+1 = 500 + 113000/30 - 150 = **4116 ms**; 300+3 = 1500 + 283000/30 - 150 = **10783 ms**; 600+5 = 2500 + 573000/30 - 150 = 21450, capped to **15000 ms**.

Budgets at move 1/20/40/60, computed from the **median real clock** of games played under the current rule (since 2026-09-14 04:10):

| control | games | move 1 | move 20 | move 40 | move 60 |
|---|---|---|---|---|---|
| 120+1 (challenger bullet) | 117 | 120.0 s -> 4.12 s | 73.7 s -> 2.57 s | 47.5 s -> 1.70 s | 34.8 s -> 1.28 s |
| 300+3 (challenger blitz) | 71 | 300.0 s -> 10.78 s | 186.1 s -> 6.99 s | 121.5 s -> 4.83 s | 90.1 s -> 3.79 s |
| 600+5 (rapid) | 50 | 600.0 s -> 15.00 s (cap) | 498.2 s -> 15.00 s (cap) | 312.0 s -> 11.85 s | 204.0 s -> 8.25 s |
| 180+2 | 31 | 6.45 s | 4.26 s | 3.02 s | 2.41 s |
| 60+1 | 23 | 2.12 s | 1.53 s | 1.20 s | 1.04 s |

The p10 clock sits within 1 to 2 s of the median in bullet and blitz, so these trajectories are very regular. The challenger only sends 120+1 and 300+3 (`MODES` default `bullet,blitz`, lichess_challenger.sh:53, 57).

Spend against opponents (mean lichess-charged time per move, moves 11 to 61): 120+1 us 2.33 s / opponents 2.13 s; 300+3 6.47 / 6.61; 600+5 13.85 / 13.61. The bot thinks about as long as its opponents.

**Lag safety margin.** There are three layers: the per-move overhead reserve (at least 150 ms), the untouched reserve (7 s at 120+1, 17 s at 300+3, 27 s at 600+5) and the 200 ms ceiling margin. Nothing in the rule accounts for the systematic 5 to 7% search overrun (section 3); the reserve absorbs it. Measured on the 6 logged games that are in the export, **lichess charged minus local total** per move: p50 +11 ms, p90 +90 ms, p99 +1027 ms, max +28.5 s (n=250). The max is tXUrJtc9 move 45 (09-22 00:24:20): 35.8 s charged against 7.2 s local, so the opponent's move reached the bot about 28.5 s late. Cause not established. Move posts time out after 30 s (client.go:105).

**Where the rule settles.** Solving spend = increment with the measured 1.07 overrun gives about 24 s on the clock at 120+1, 60 s at 300+3 and 96 s at 600+5. Observed: our lowest clock p50 is 32.1 s in bullet and 73.2 s in blitz. Lost games end with p50 30.1 s (bullet), 63.8 s (blitz) and 143.8 s (rapid) left on the clock. That clock is never turned into depth.

## 2. Threads, cores, transposition table

- **Threads per game.** `p.Threads = c.Threads` = 4 (champion.go:124). Each move runs `chooseMoveIterativeScoredThreads` (elo.go:320, search2.go:1332-1362): 3 helper goroutines plus the main search, sharing one table behind 1024 striped mutexes (tt.go:206).
- **Games compete, they do not share.** Each accepted game gets its own goroutine (bot.go:165), its own table (bot.go:229-231) and its own search contexts. Nothing limits search threads across games. `grep` finds no `GOMAXPROCS`, `LockOSThread`, QoS, affinity or `setpriority` anywhere in the repo, so Go runs with GOMAXPROCS = NumCPU = 10 (go1.27.1): at most 10 search goroutines run at once, time-sliced by the Go scheduler, on OS threads macOS spreads over 4 P-cores + 6 E-cores (`sysctl hw.perflevel0/1.physicalcpu` = 4/6).
- **Priority.** `ps` shows PID 77225 at **NI 10** (the challenger too). This Claude Code shell runs at NI 5, so any agent or tool job launched from here outranks the bot.
- **At -max-games 5**: up to 20 runnable search goroutines for 10 cores, about 2 cores per game, some of them E-cores. BUGS.md "Two threads cost a full ply": with one game, 2 threads reach mean depth 8.0 where 4 and 8 both reach 9.0. Commit c6ccaad: under a two-game load, 4 and 8 threads gave the same depth.
- **Table.** tt_bits 22 = 4,194,304 entries x 24 B (asserted in engine/ttsize_test.go:10) = **96 MiB per game**, allocated at game start, reused across that game's moves, freed at game end. At 5 games that is 480 MiB. Right now, with 2 games in flight, the process RSS is 460 MB. The machine has 16 GB, and `top` showed 397 MB free and a 2.2 GB compressor at 09:24.
- **Minor.** `lastSearchNodes` / `lastSearchDepth` are package globals reset by every search (search2.go:169, 1339, 1459), so they give wrong values when games overlap. Diagnostics only; the move log does not use them.

Games in flight, time-weighted over the export (when at least 1 game was running): 1 game 68.9%, 2: 21.6%, 3: 6.7%, 4: 1.9%, 5+: 0.8%.

Score against rating expectation by the most games in flight during the game (rated, stall-flag games excluded so sleep is not charged to concurrency; 95% interval on score in brackets):

| | 1 in flight | 2 in flight | 3+ in flight |
|---|---|---|---|
| bullet | n=49, +0.114 (+/-0.122), ~+80 Elo | n=78, -0.001 (+/-0.103), ~0 | n=25, -0.130 (+/-0.173), ~-91 |
| blitz | n=69, +0.028 (+/-0.103), ~+20 | n=90, +0.093 (+/-0.091), ~+65 | n=24, -0.117 (+/-0.175), ~-84 |
| rapid | n=24, +0.069 (+/-0.171), ~+48 | n=24, +0.131 (+/-0.176), ~+93 | n=65, +0.068 (+/-0.099), ~+50 |
| all | n=146, +0.067 (+/-0.071), ~+47 | n=200, +0.060 (+/-0.062), ~+42 | n=122, -0.024 (+/-0.074), ~-17 |

⚠ Removing the stalled losses pushes every cell upward, so only the differences between columns mean anything. The split is also confounded by 10 days of champion swaps and a changing opponent mix. Read it as "3+ is worse in bullet and blitz, 1 versus 2 is indistinguishable", not as exact Elo.

## 3. Move log: total versus budget

439 move lines, 9 games. 47 moves returned instantly (book, search under 5 ms); ratios below cover the 392 searched moves.

| metric | p10 | p50 | p90 | p99 | max |
|---|---|---|---|---|---|
| search / budget | 1.050 | 1.054 | 1.108 | 1.259 | 1.318 |
| total / budget | 1.055 | 1.069 | 1.144 | 1.301 | 1.464 |
| total - budget (ms) | +77 | +209 | +765 | +1338 | +1666 |
| post (ms, all 439) | 16 | 21 | 120 | 359 | 659 |
| total - search - post (ms) | 0 | 0 | 1 | 7 | 89 |

- 371/392 moves (95%) finish over budget. That is by design: the 1.05 hard abort is the normal end of a move. 208/392 end more than 5 ms past that abort, mostly by a few ms (p50 1.054); the tail reaches 1.32. By budget: [1, 3) s p90 1.090; [3, 10) s p90 1.159, p99 1.287.
- Worst: EhduAAzd today 09:27:20, search 1678 ms + post 659 ms against 1596 ms (1.46x); tXUrJtc9 09-22 00:26:41, 5428 ms search against 4119 ms (1.32x, only game in flight at the time).
- Slowest posts: 659, 436, 410, 399 and 361 ms; four of the five are from today's session. The overhead estimate rises after each one (bot.go:380).
- Replaying the move list and parsing cost nothing (p99 7 ms).

## 4. Time-loss and contention history

Git and BUGS.md:
- c6bbedd (09-13): hTmspQs0 lost on time. The old rule (1/20 of the clock + 0.9 increment) bled to a permanent scramble, and a correspondence game searching 15 s moves beside it added about 1 s a move of contention. Fixed rule; correspondence declined (state.go:43-53).
- 95395eb (09-13): 56 search threads on 10 cores (7 games x 8 threads), so the MaxGames cap was added.
- 32a5526, then c6ccaad (09-13): threads 8 -> 2 -> 4.
- 0570206: rUZ1clR4 lost on time after its game stream died and the bot stopped listening; the bot now reconnects game streams.
- 43e68fb, f9c6bfb (09-14): overhead measured per game; ceiling scaled with the clock. 1ccaed2 (09-22): 30 s cap after blbndbPY.

In the export (588 standard games with clocks): **55 losses on time**. Blitz 21/224, rapid 22/146, bullet 9/194, classical 3/24. By day: 09-15 15/91, 09-19 7/37, 09-20 5/21, 09-21 14/71, 09-22 3/42, 09-23 3/18.
- **54 are stalls.** In each, one move burnt 28 s to 1028 s beyond its budget. The only exception is hTmspQs0 (09-13, the old bleed: 17 moves each more than 5 s over, worst 7.3 s).
- Clustering stall starts within 60 s gives **44 events**. **8 events hit 2 to 4 games at the same moment** (19 games). Examples: 09-21 20:54:30 to 20:54:46 took M4OsJA0m, VTfXUuoe, 9yt0U3eo and 9OCZn3tT; 09-21 21:22:18 to 21:22:41 took focq66v8, qaOCjwPE and JoTrYF9Y. Four games stalling at once is a process-level or machine-level event, not a per-game search problem.
- Only 4 of the 44 events have an engine or bot commit within 20 minutes, so restarts for champion swaps do not explain most of them.
- **Proof for the two stalls a log covers: system sleep.** The log timestamps are wall clock, while "finished after" uses Go's monotonic clock, which on macOS does not advance during system sleep.
  - 1q6VdN8S started 00:43:05. Its last move was logged at 00:46:15, and it logged "finished after 3m33s" at 01:01:34: 18m29s of wall time against 3m33s monotonic, so about 15 minutes asleep.
  - jVzbwbGF started 03:20:01. Its last move was logged at 03:22:30. At 03:38:40 it logged `move h1g2 failed: ... connection reset by peer`, and at 03:38:54 "finished after 2m52s" against 18m53s of wall time.
  - Between them, the event stream died 7 times with `http2: client connection lost`, some connections lasting only 30 to 45 s, which fits dark wakes.
- **Power settings now** (`pmset -g custom`): `sleep 1` (minute) on **both** Battery and AC, `displaysleep` 2 (battery) / 10 (AC), powernap 1. The machine is on battery (97%, discharging). Current sleep blockers (`pmset -g assertions`): `caffeinate -i -t 300`, PID 85753, whose parent is the Claude Code session (PID 21653); Chrome audio; and powerd holding off sleep while the display is on. None of these is tied to the bot.
- `pmset -g log` since 09-25 09:05: 27 sleeps (17 Maintenance, 4 Idle, 2 Clamshell, **2 Dark Wake Thermal Emergency at 09-26 01:02:22 and 01:02:38**, 2 Sleep Service), 18 on battery and 9 on AC. The latest idle sleep was today at 09:14:23; the bot was started at about 09:15.

## 5. Recommendations

| # | change | reasoning | measurement that confirms it |
|---|---|---|---|
| R1 | **Keep the Mac awake and on AC, lid open, whenever the bot runs.** Immediate, no restart: `caffeinate -i -s -w 77225 &` (holds a sleep assertion until PID 77225 exits; sends it no signal; `-s` only takes effect on AC). Durable: the bot takes its own assertion at startup, for example by spawning `caffeinate -i -w <own pid>`; this is a code change plus restart, so it is the user's call. | 54/55 time losses were stalls, 2/2 logged ones proven sleep, sleep is set to 1 min on AC and battery, and 2 thermal-emergency plus 2 clamshell sleeps since 09-25. Worth about **+33 Elo** (bullet +24, blitz +31, rapid +45), valuing each stalled game at its expected score. | `pmset -g log` shows no `Sleep` line while games are in flight, and `runtime_audit.py` on the next 100 games shows 0 flag losses with a stall (one move far past budget). |
| R2 | **`-max-games 5` -> `2`**, threads stay **4**, challenger `WANT` stays **2**. | 2 x 4 = 8 search threads fit 10 cores; 4 and 8 threads reach the same depth with 1 or 2 games (BUGS.md, c6ccaad), so 8 threads buys nothing. 3+ in flight scored about -85/-91 Elo in blitz/bullet (noisy), while 1 and 2 are indistinguishable. With WANT 2, a cap of 5 only lets incoming challenges push the bot to 3-5 games (9.4% of busy time historically). The rating tracks strength per game; with 159+ games per mode, volume adds little. Needs a restart, so the user decides. | Add completed depth and nodes to the move log line, returned per search rather than from the racy globals, then compare mean depth at equal budget for 1, 2 and 3+ games in play. Expect 3+ to be at least 1 ply shallower. |
| R3 | **Run the bot at nice 0** (`sudo renice -n 0 -p 77225`: lowering nice needs root; renice sends no signal), and keep training, gauntlets and agent benchmarks off this machine during rated play. | The bot is at NI 10 and loses the CPU to anything at 0 to 5. There is precedent: hTmspQs0 lost about 1 s a move to a correspondence search on the same cores. | `search/budget` and depth per move with and without a background load. Today p50 is 1.054; a contended move shows up as higher overrun and lower depth. |
| R4 | ⚠ Suggestion, unmeasured: spend more of the clock. For example, drop the reserve from 2 s + 5 x inc to 2 s + 2 x inc, or use 25 moves to go instead of 30. | The rule settles at about 24 s (120+1), 60 s (300+3) and 96 s (600+5) that it never spends, and lost games end with 30 s (bullet), 64 s (blitz) and 144 s (rapid) unused. The Elo value of 15 to 30% more time per move was not measured here. | Must first keep the floors in `lichessbot/clocksim_test.go`, then win a real-clock race at 120+1 against the current rule before it ships. |
| R5 | ⚠ Suggestion, low priority: try tt_bits 23 (192 MiB per game) for rapid only. | At 8 to 15 s a move the table holds far fewer entries than the nodes searched. Not measured. | A paired race at a rapid-like budget. |

Leave `time_ms` alone: it has no effect in clocked games (bot.go:325-337). The 150 ms overhead floor is above p90 post latency (120 ms), but lowering it would buy 5 to 9% of a bullet budget at best, with p99 posts at 359 ms. Not worth the risk.

## Not covered

- Games after 09-23 11:54 (the export snapshot stops there) and any log before 09-21 23:58. The stall cause is proven for 2 of 54 stalls; the other 52 share the signature but cannot be tied to `pmset` records, which only go back to 09-25.
- No benchmark, match or engine test was run (the bot was playing). Depth under contention is inferred from BUGS.md and c6ccaad, not re-measured.

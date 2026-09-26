# Reachable lichess rating for tidymazebot (2026-09-26)

**Verdict.** 2730 bullet (+25%) is not realistic with the current engine. On lichess the
rating does not grow with games: at our RD floor it converges to the engine's true
performance in the bot pool within about 150 games, then only fluctuates (SD about 30).
Our bullet performance over the last 100 games is **2179**, so the rating (2183) is
already sitting on it. Holding 2730 means scoring 50% against the bots rated 2730 today
(saxtonengine, pawn_git, duchessai, grail-bot), about **+550 Elo of real strength** over
what the bot has shown. Realistic near term: **2250 to 2300 bullet** if the round-2 gain
(-265 to -175 against Stockfish 2700) carries over, reached within one or two days of
play. 2400 (+10%) needs roughly +220 of real strength on top of 2179.

The hard daily ceiling is **100 bot-vs-bot games per rolling day** (lila source), which
at 2+1 with 2 games in flight is about 4.7 hours of play.

## 1. Online bot pool

Snapshot: `https://lichess.org/api/bot/online?nb=200`, 200 bots returned
(`bots_online_2026-09-26.ndjson`). The endpoint returned exactly the requested maximum, so
more bots may be online (⚠ sample, not census). Established = not provisional and at
least 20 games in that speed.

| speed  | established | median | p75  | p90  | >= 2183 | >= 2400 | >= 2600 | >= 2700 | >= 2800 | >= 2930 | max  |
|--------|------------:|-------:|-----:|-----:|--------:|--------:|--------:|--------:|--------:|--------:|-----:|
| bullet | 160 | 2035 | 2503 | 3015 | 63 | 45 | 35 | 32 | 25 | 21 | 3093 |
| blitz  | 176 | 1858 | 2445 | 2964 | 60 | 46 | 37 | 31 | 25 | 21 | 3032 |
| rapid  | 167 | 1992 | 2416 | 2958 | 67 | 45 | 32 | 29 | 23 | 18 | 3050 |

The pool is compressed at the top: about 21 bots sit between 2930 and 3093, almost all
full-strength engines, so the scale saturates there.

Online bots in the challenger's +-250 window (bullet): 44 around 2183, 31 around 2400,
21 around 2730.

### What the top bots run (from the profile bio and links in the same payload)

- **2930 to 3093 (21 bots)**: 7 bios name Stockfish (cheszter, raspfish, resolutebot,
  bot_stockfish13, batyarapro, ...). Others: Alexandria (alexandrya 3066), pzchessbot 3015
  ("Estimated CCRL rating: ~3720"), Tcheran (jpg-bot 3015, Rust, 1 vCPU), admete_bot 2940
  ("3117 CCRL elo"), cloudnetbot 3010 (25% of moves from online databases).
- **2700 to 2899 (11 bots)**: weiawaga 2846 (Rust, Raspberry Pi), clrsrc_lc0 2840
  (Rust NNUE "built AI-assisted with Claude Code", 8 vCPU), leelajester 2809,
  aggressivestockfish 2803 (modified Stockfish 11), simple-bot 2787 (C++), grail-bot 2759
  (Rust NNUE), saxtonengine 2746 (C, NNUE, Ryzen 7900X), pawn_git 2736 (C++, RPi 4,
  3 threads), duchessai 2731, worst-ai 2718 (Fairy-Stockfish), deepbecky 2711 (C++
  written by AI).
- Bios that state a CCRL figure, used as a rough scale bridge:

| bot | CCRL claim in bio | lichess bullet |
|-----|-------------------|---------------:|
| pzchessbot | ~3720 (estimated) | 3015 |
| admete_bot | 3117 (list not named) | 2940 |
| ravenengine | 2590 (CCRL 40/40) | 2287 |
| eubos | ~2550 (CCRL Blitz) | 2427 |
| matmoi | "around 2000" | 2094 |
| likeawizard-bot | 1864 (CCRL Blitz) | 2344 |

Interpolating eubos to admete (slope 0.905 lichess per CCRL point), lichess bullet 2730
corresponds to about **CCRL 2885** (⚠ two self-reported points, different hardware).

## 2. Glicko-2 as lichess applies it

Read from source: lila `d04b5ee` (2026-09-26), scalachess master.

- `modules/round/src/main/PerfsUpdater.scala`: each game is its own rating period,
  `computeGame(..., skipDeviationIncrease = true)`, so step 6 uses 0 elapsed periods.
  **Volatility never enters a game's rating change.** It only matters for the RD growth
  between games (`liveDeviation`, 0.21436 periods per day), negligible when playing daily.
- `modules/rating/src/main/Glicko.scala`: `minDeviation = 45`, `maxVolatility = 0.1`,
  `maxRatingDelta = 700`; RD is capped to [45, 500] before and after each game.
  Standard chess uses `ColorAdvantage.standard = 11.782457` (white +5.9, black -5.9).
- `modules/rating/src/main/RatingRegulator.scala`: gains (never losses) are multiplied by
  1.010 bullet, 1.005 blitz, 1.015 rapid, 1.010 classical. The halving applies only to a
  human playing a bot, not bot vs bot.
- Tau 0.75 (`Tau.default`).

Our state: bullet/blitz/rapid RD 46 (`/api/user/tidymazebot`), so we are on the floor.

**Validation** on our own 396 rated games (bullet/blitz/rapid after the 30th game of each
speed, non-provisional opponents, both RDs assumed 45): predicted `ratingDiff` exact in
58%, within 1 point in 80% (bullet 109/130, blitz 144/167), MAE 1.44, signed mean -0.16.
The misses above 3 points are mostly early rapid games where our RD was still above 45.

## 3. Rating change per game (RD 45, bullet, colour-averaged)

Identical at any rating level, because at the RD floor the update only depends on the gap.

| opponent vs us | expected score | win | draw | loss |
|---:|---:|---:|---:|---:|
| -100 | 0.640 | +4.15 | -1.58 | -7.26 |
| 0    | 0.500 | +5.73 | 0.00  | -5.68 |
| +100 | 0.360 | +7.33 | +1.59 | -4.11 |
| +200 | 0.240 | +8.72 | +2.96 | -2.77 |

Equivalent Elo K is about 11.4. Linearised, each game closes about 1.6% of the gap
between rating and true strength: **time constant about 61 games**.

## 4. Games from 2183 to 2400 and 2730

Model: true strength T fixed, opponents at our rating -4.8 (our measured mean bullet
offset), draw rate 14.8% (our bullet games). Mean path (expected value) and a Monte Carlo
of 200 runs x 1500 games.

Mean path, rating after N games from 2183:

| true T | 50 g | 100 g | 150 g | 200 g | 300 g |
|---:|---:|---:|---:|---:|---:|
| 2270 | 2232 | 2254 | 2264 | 2268 | 2271 |
| 2300 | 2249 | 2278 | 2291 | 2297 | 2301 |
| 2400 | 2301 | 2357 | 2382 | 2393 | 2400 |
| 2500 | 2346 | 2432 | 2471 | 2488 | 2499 |
| 2730 | 2419 | 2580 | 2663 | 2701 | 2726 |

Games to first reach the target:

| true T | 2400, mean path | 2400, MC median (share of runs) | 2730, mean path | 2730, MC median (share) |
|---:|---:|---:|---:|---:|
| 2300 | never | 902 (6%) | never | never (0%) |
| 2400 | 304 | 154 (100%) | never | never (0%) |
| 2500 | 77 | 75 (100%) | never | never (0%) |
| 2600 | 56 | 55 (100%) | never | 863 (under 1% of runs) |
| 2730 | 46 | 46 (100%) | 380 | 228 (100%) |
| 2780 | 44 | n/a | 177 | n/a |
| 2800 | 43 | 43 (100%) | 161 | 159 (100%) |
| 2830 | 43 | n/a | 145 | n/a |

Stationary behaviour (MC, every T): mean equals T, **SD 29.8**. The median peak over 1500
games is T + 72. A true 2660 engine would touch 2730 at some point over about two weeks of
capped play but would not hold it.

## 5. Performance needed to hold 2730

- 50.0% against bots rated 2730.
- 54.3% against bots rated 2700.
- 95.9% against today's 2183 peers.

Today's bullet record: last 100 games 45 W / 10 D / 45 L, score 0.500, average opponent
2189, performance **2179** (SE about 32). All 162 rated bullet opponents were bots.

## 6. Our measured strength against that bar

- Round 2 moved the engine from -265 to -175 against Stockfish `UCI_Elo 2700` at 100 ms a
  move, i.e. about **2525 on Stockfish's scale**. Stockfish's `Skill` comment says the
  scale "covers CCRL Blitz Elo from 1320 to 3190". For `UCI_Elo 2700`, `search.h` gives
  level 8.86, so Stockfish picks its move at depth 9: its strength barely depends on the
  clock, while ours does. At the bot's real budgets (log: 1.33 s to 3.26 s per move at
  2+1) we should score better against that yardstick than at 100 ms (⚠ unmeasured).
- Bar for 2730 on the CCRL bridge above: about 2885. Gap at 100 ms: **about 360 points**.
- Bar on the lichess side: 2730 - 2179 = **551 points** over the performance of the older
  binaries. The running binary was built 09:11 today, after `15973a7`, and had played 1
  rated game at snapshot time, so its lichess strength is not measured yet.
- ⚠ Observation, not a diagnosis: by the CCRL bridge a 2435-2525 engine would sit around
  2320-2400 lichess bullet, above our 2179. Candidates to check elsewhere: two games in
  flight sharing `threads 4` on 4 performance cores (bot at 769% CPU), network overhead
  at 2+1, time management.
- Performance by window (all speeds on older binaries; SE about 321/sqrt(n)):

| speed | 09-13..16 | 09-17..20 | 09-21..26 |
|---|---|---|---|
| bullet | 2175 (n 97) | 2162 (n 53) | 2279 (n 12, SE 93) |
| blitz  | 2154 (n 100) | 2152 (n 60) | 2205 (n 43, SE 49) |
| rapid  | 2227 (n 62) | 2179 (n 14) | 2327 (n 57, SE 43) |

The latest window is up 50 to 150, within 1 to 2 SE: a hint that engine gains transfer,
not proof.

## 7. Daily limit, throughput, gain per day

- `modules/bot/src/main/BotLimit.scala`: `Max(100)`, `RateLimit[UserId](max, 1.day,
  "bot.vsBot.day")`, hit for both players at the start of every game where all players
  are bots, **rated or casual**. Error: "You played 100 games against other bots today".
  One counter per account for all speeds. clrsrc_lc0's bio cites the same "max. 100
  Botgames / Day". Our busiest days: 93 (09-13), 92 (09-16), 91 (09-15).
- Our casual games so far (33 bullet, 21 blitz, 13 rapid) consumed the same quota.
- Game lengths (our rated games, median wall clock) and throughput with 2 in flight,
  assuming about 60 s between games (challenger polls every 60 s):

| mode (challenger) | median | p90 | games/hour | hours to 100 games |
|---|---:|---:|---:|---:|
| bullet 120+1 | 282 s | 411 s | 21 | 4.7 |
| blitz 300+3 | 758 s | 1161 s | 8.8 | 11.4 |
| rapid 600+5 | 1154 s | 1861 s | 5.9 | 16.9 |

- Gain per day at the cap (100 bullet games): about 80% of the gap between rating and true
  strength on day 1, about 96% by day 2, then nothing. Examples from the table in section
  4: T 2300 gives +95 on day 1, T 2400 +174, T 2500 +249. The most the rating can drift
  from luck is about +-30 (1 SD), +-60 (2 SD).
- Reaching 2730 from 2183 takes 177 games (1.8 capped days, about 8.5 hours of bullet)
  for a true 2780 engine; 228 games median for a true 2730 engine. Games are not the
  constraint.

## 8. What is realistic

| target | real strength needed in the bot pool | status |
|---|---|---|
| 2183 (now) | 2179, measured | holding |
| 2250-2300 | +70 to +120 | plausible if round 2 transfers; visible after about 150 bullet games |
| 2400 (+10%) | +220 | about 1.5x what rounds 1 and 2 gave at 100 ms (+58 in `3bea0fc`, +90 in `15973a7`) |
| 2730 (+25%) | +550, CCRL-equivalent about 2885 | top 32 of the online pool; not reachable by playing more |

Next measurement that settles the question cheaply: 150 bullet games of the new binary
(1.5 capped days). Its performance rating over those games, SE about 26, is the new
equilibrium.

## Caveats

- ⚠ Opponent RD at game time is not in the export; validation assumed 45.
- ⚠ The CCRL bridge rests on self-reported bios and mixed hardware.
- ⚠ Stockfish's UCI_Elo anchoring at 100 ms is an inference from its depth-based handicap,
  not a measurement.
- The online list is capped at 200 entries.

## Reproduce

- `python3 analyses/bot-elo/glicko_projection.py analyses/bot-elo/tidymazebot_games_2026-09-26.ndjson`
  (3 s single core): validation, performance, per-game table, mean path, Monte Carlo.
- Data: `bots_online_2026-09-26.ndjson` (public), `tidymazebot_games_2026-09-26.ndjson`
  (`/api/games/user/tidymazebot`, needs the bot token since anonymous export now returns
  404).
- Sources read: lila `d04b5ee` `modules/bot/src/main/BotLimit.scala`,
  `modules/rating/src/main/{Glicko,RatingRegulator,Perf}.scala`,
  `modules/round/src/main/PerfsUpdater.scala`; scalachess
  `rating/src/main/scala/glicko/{GlickoCalculator,model}.scala`,
  `impl/RatingCalculator.scala`; Stockfish master `src/search.h` (`Skill`).

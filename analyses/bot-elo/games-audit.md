# TidyMazeBot lichess games audit (2026-09-26)

**Outcome:** the engine is not the main thing holding the rating down. 35 of our 117 losses in the last 300 games (30%) are losses on time where the bot simply stopped moving with 28 s to 605 s still on its clock. They cost -256 rating points over 262 rated games; every other game put together earned +346. Across all 592 games there are 55 such stalls. In 20 of them another game was in progress, and in 16 of those the bot made no move in the other game either. So the whole process froze or was absent; this is not one game's search hanging.

## Top 3 leaks

| # | leak | count | rating cost (sum of lichess ratingDiff) | evidence |
|---|---|---|---|---|
| 1 | **Stalls: loss on time with clock left.** The bot stops moving and flags. Not time trouble: our clock was never under 10 s in any of the 300 games. | 35 of 117 losses (last 300), 55 in all 592 games. Material at the flag: 24 level, 7 ahead by 2+, 4 behind. Plus 42 completed moves in 28 games that took longer than the budget rule allows (up to 1058 s), i.e. stalls the bot came back from. | -256 (last 300 rated: bullet -49, blitz -46, rapid -83, classical -78); -456 over all non-provisional rated games | sections 5, 10; of the 20 stalls with another game in progress, 16 had no move in that game either |
| 2 | **FataliiBot matchups.** Most frequent opponent, and the one we lose to far beyond expectation. | 36 games (14% of rated), 3/3/30, score 12.5% vs 33.2% expected, performance 1997. 9 of the 30 losses are stalls. | -45 in the 27 non-stall games (last 300); -68 over all games | section 6, 10 |
| 3 | **Clock left unused.** The budget rule never goes near the flag and leaves search depth on the table. | 0 of 300 games under 10 s. At our move 40 the median clock left is 40% of the initial time in bullet, 43% in blitz, 51% in rapid. At the end we still hold 41% (blitz) and 37% (rapid) of the initial time vs 27% and 30% for the opponent. | no direct rating number from game data (would need a measured A/B) | section 5 |

Minor: 9 draws where we were 2+ material ahead, 5 of them failed conversions rather than repetitions (fifty-move rule 3, stalemate 1, flag 1), e.g. [sXWkqol4](https://lichess.org/sXWkqol4) +8 fifty-move, [eDC8WcGI](https://lichess.org/eDC8WcGI) +9 stalemate. The number of concurrent games shows no measurable cost (section 8).

## Key numbers

| speed | rated games (last 300) | W/D/L | score | avg opp | performance | performance with our time losses removed | lichess rating 09-13 → 09-23 |
|---|---|---|---|---|---|---|---|
| bullet | 71 | 32/6/33 | 49.3% | 2198 | 2193 | 2243 | 2183 → 2183 |
| blitz | 110 | 49/19/42 | 53.2% | 2142 | 2164 | 2190 | 2182 → 2169 |
| rapid | 73 | 30/13/30 | 50.0% | 2317 | 2317 | 2415 | 2208 → 2271 |
| classical | 8 | 2/3/3 | 43.8% | 2223 | 2179 | 2357 (5 games) | 2271 → 2221 |

"Time losses removed" is an upper bound, since those games would not all have been wins. The fair figure sits between the two columns.

- **Trend:** flat in bullet and blitz for 10 days. Rapid gained +54 on 09-21 alone (50 games, 53%). The export ends with the game finished 09-23 12:00. Games played since the bot restart on 2026-09-26 09:19 are not in this data.
- **Before/after engine changes** (section 7, non-provisional rated): up to 09-16 21:15 (hand eval) performance 2188; 09-16 to 09-19 (NNUE only) 2165; **09-19 23:37 to 09-22 22:36 (rungs 12 to 23) 2287, 57.1% vs 48.8% expected over 105 games, 2380 without the time losses**; after 09-22 22:36, 18 games at 36.1% (too few to read). The engine gains show up on lichess. The stalls hide them.
- **Opponent bands:** we beat opponents 100 to 200 points below us (79% vs 70% expected) and underperform against those 100+ above (24% vs 30% at +100..+200, 7% vs 12% at +200 and up).
- **Losses by status (last 300):** 82 mate, 35 out of time, 0 resign (the bot never resigns), 0 abandon/timeout. Draws: 27 threefold repetition, 9 insufficient material, 6 fifty-move rule, 1 stalemate, 1 flag against insufficient material.
- **Unrated games:** 38 of the last 300, mostly against lichess AI levels 6 to 8 (rating 0 in the export, hence the meaningless 821 average).

## What would fix leak 1 (not measured, for the owner to decide)

- Cause not proven from game data. The bot log is overwritten on every restart (current log starts 2026-09-26 09:19) and `pmset -g log` starts 2026-09-25, so the stall windows 09-13 to 09-23 cannot be matched to a restart, a sleep or a dead stream. The data only rules out a per-game search hang.
- `pmset -g` shows `sleep 1` (system sleep after 1 min idle, currently held off by caffeinate, Chrome and coreaudiod). Any window without caffeinate would freeze the bot.
- Cheap next steps: append the bot log instead of overwriting it; log a heartbeat per game (last move sent, stream alive) so the next stall names its cause; keep `caffeinate -i` tied to the bot's lifetime.

## Method and caveats

- Games: `GET https://lichess.org/api/games/user/tidymazebot?max=300&pgnInJson=true&clocks=true&evals=false&opening=true`, raw at `/Users/yann.rolland/work/analyses/chess-go/logs/bot_games_300.ndjson` (300 lines). The same call without `max` gave all 592 games: `bot_games_all.ndjson`. Rating history: `rating_history.json`. Anonymous calls to both endpoints returned HTTP 404 for this account, so they were made with `LICHESS_BOT_TOKEN` (GET only, token not printed).
- Clocks: lichess `clocks` holds the mover's clock after each ply, in centiseconds. Games ending on time, by resignation or on a claim carry one extra entry (the side to move). Think time = previous own clock - current + increment, from move 3 on.
- Draw kinds and material were computed by replaying moves with python-chess 1.11.2.
- Regenerate the data sections below: `games_audit.py <300.ndjson> <all.ndjson> <rating_history.json> games-audit.md` (overwrites this header too).

Data: 300 most recent finished games (262 rated), 09-16 20:34 to 09-23 12:00 CEST. Trend/epoch tables use all 592 games (520 rated), 09-13 13:23 to 09-23 12:00.

## 1. Per speed (last 300, rated only)

`expected` = mean Elo expectancy from our pre-game rating vs opponent's; performance = opp avg + 400*log10(s/(1-s)).

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| bullet | 71 | 32/6/33 | 49.3% | 49.3% | 2179 | 2198 | 2193 |
| blitz | 110 | 49/19/42 | 53.2% | 52.3% | 2171 | 2142 | 2164 |
| rapid | 73 | 30/13/30 | 50.0% | 44.5% | 2256 | 2317 | 2317 |
| classical | 8 | 2/3/3 | 43.8% | 53.3% | 2248 | 2223 | 2179 |
| all rated | 262 | 113/41/108 | 51.0% | 49.3% | 2199 | 2208 | 2215 |
| unrated (all speeds) | 38 | 26/3/9 | 72.4% | 83.0% | 2182 | 821 | 989 |

Same, with our losses on time removed (what the engine scores when it actually moves):

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| bullet | 64 | 32/6/26 | 54.7% | 47.8% | 2179 | 2210 | 2243 |
| blitz | 102 | 49/19/34 | 57.4% | 52.5% | 2171 | 2139 | 2190 |
| rapid | 58 | 30/13/15 | 62.9% | 43.8% | 2250 | 2323 | 2415 |
| classical | 5 | 2/3/0 | 70.0% | 55.0% | 2248 | 2210 | 2357 |
| all rated | 229 | 113/41/75 | 58.3% | 49.0% | 2195 | 2207 | 2265 |

By colour (rated):

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| bullet white | 26 | 12/1/13 | 48.1% | 45.8% | 2181 | 2216 | 2203 |
| bullet black | 45 | 20/5/20 | 50.0% | 51.3% | 2178 | 2187 | 2187 |
| blitz white | 51 | 20/11/20 | 50.0% | 50.0% | 2171 | 2162 | 2162 |
| blitz black | 59 | 29/8/22 | 55.9% | 54.3% | 2170 | 2124 | 2165 |
| rapid white | 39 | 20/5/14 | 57.7% | 48.9% | 2251 | 2261 | 2315 |
| rapid black | 34 | 10/8/16 | 41.2% | 39.5% | 2261 | 2382 | 2320 |
| classical white | 6 | 1/2/3 | 33.3% | 49.4% | 2238 | 2242 | 2122 |
| classical black | 2 | 1/1/0 | 75.0% | 65.1% | 2278 | 2164 | 2355 |

By time control (rated, n>=5):

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| 120+1 (bullet) | 49 | 22/4/23 | 49.0% | 50.5% | 2178 | 2177 | 2170 |
| 600+5 (rapid) | 48 | 21/11/16 | 55.2% | 45.1% | 2271 | 2306 | 2342 |
| 300+3 (blitz) | 30 | 14/1/15 | 48.3% | 42.7% | 2171 | 2237 | 2225 |
| 180+2 (blitz) | 18 | 7/4/7 | 50.0% | 56.1% | 2172 | 2116 | 2116 |
| 180+0 (blitz) | 18 | 8/5/5 | 58.3% | 53.9% | 2170 | 2140 | 2198 |
| 60+1 (bullet) | 11 | 4/0/7 | 36.4% | 34.5% | 2182 | 2372 | 2275 |
| 180+1 (blitz) | 10 | 7/0/3 | 70.0% | 57.1% | 2166 | 2090 | 2237 |
| 300+2 (blitz) | 9 | 5/2/2 | 66.7% | 65.7% | 2174 | 1961 | 2082 |
| 60+2 (bullet) | 7 | 5/1/1 | 78.6% | 57.0% | 2185 | 2134 | 2359 |
| 180+3 (blitz) | 6 | 2/2/2 | 50.0% | 44.9% | 2173 | 2209 | 2209 |
| 180+5 (blitz) | 5 | 3/1/1 | 70.0% | 58.7% | 2168 | 2029 | 2176 |
| 300+0 (blitz) | 5 | 1/1/3 | 30.0% | 50.7% | 2168 | 2168 | 2021 |

## 2. Rating trend (all games, rated, per day)

| speed | day | games | score | rating start | rating end | net | flag losses |
|---|---|---|---|---|---|---|---|
| bullet | 09-13 | 16 | 47% | 3000 | 2175 | -825 | 0 |
| bullet | 09-14 | 6 | 25% | 2175 | 2159 | -16 | 0 |
| bullet | 09-15 | 40 | 50% | 2159 | 2174 | +15 | 2 |
| bullet | 09-16 | 35 | 50% | 2180 | 2179 | -1 | 0 |
| bullet | 09-17 | 23 | 41% | 2179 | 2179 | +0 | 2 |
| bullet | 09-18 | 17 | 59% | 2176 | 2196 | +20 | 0 |
| bullet | 09-19 | 13 | 42% | 2196 | 2171 | -25 | 4 |
| bullet | 09-21 | 3 | 100% | 2171 | 2185 | +14 | 0 |
| bullet | 09-22 | 7 | 57% | 2185 | 2191 | +6 | 0 |
| bullet | 09-23 | 1 | 0% | 2191 | 2183 | -8 | 1 |
| blitz | 09-13 | 15 | 23% | 3000 | 2180 | -820 | 0 |
| blitz | 09-14 | 8 | 44% | 2180 | 2160 | -20 | 0 |
| blitz | 09-15 | 38 | 53% | 2161 | 2157 | -4 | 10 |
| blitz | 09-16 | 39 | 44% | 2157 | 2164 | +7 | 2 |
| blitz | 09-17 | 20 | 55% | 2164 | 2181 | +17 | 1 |
| blitz | 09-18 | 21 | 45% | 2178 | 2163 | -15 | 0 |
| blitz | 09-19 | 14 | 50% | 2163 | 2167 | +4 | 2 |
| blitz | 09-20 | 5 | 30% | 2167 | 2162 | -5 | 1 |
| blitz | 09-21 | 14 | 71% | 2158 | 2182 | +24 | 0 |
| blitz | 09-22 | 15 | 57% | 2182 | 2187 | +5 | 3 |
| blitz | 09-23 | 14 | 46% | 2187 | 2160 | -27 | 1 |
| rapid | 09-13 | 35 | 63% | 3000 | 2229 | -771 | 0 |
| rapid | 09-14 | 4 | 50% | 2229 | 2228 | -1 | 1 |
| rapid | 09-15 | 10 | 40% | 2228 | 2225 | -3 | 3 |
| rapid | 09-16 | 13 | 46% | 2225 | 2217 | -8 | 1 |
| rapid | 09-17 | 4 | 25% | 2217 | 2215 | -2 | 0 |
| rapid | 09-18 | 4 | 25% | 2205 | 2203 | -2 | 0 |
| rapid | 09-19 | 5 | 60% | 2203 | 2196 | -7 | 1 |
| rapid | 09-20 | 1 | 100% | 2196 | 2209 | +13 | 0 |
| rapid | 09-21 | 50 | 53% | 2209 | 2263 | +54 | 13 |
| rapid | 09-22 | 4 | 75% | 2263 | 2280 | +17 | 0 |
| rapid | 09-23 | 3 | 0% | 2280 | 2271 | -9 | 1 |
| classical | 09-13 | 8 | 62% | 3000 | 2367 | -633 | 0 |
| classical | 09-14 | 4 | 50% | 2271 | 2253 | -18 | 0 |
| classical | 09-15 | 1 | 50% | 2253 | 2253 | +0 | 0 |
| classical | 09-16 | 2 | 50% | 2253 | 2261 | +8 | 0 |
| classical | 09-17 | 1 | 50% | 2261 | 2270 | +9 | 0 |
| classical | 09-19 | 2 | 75% | 2270 | 2284 | +14 | 0 |
| classical | 09-20 | 2 | 0% | 2284 | 2217 | -67 | 2 |
| classical | 09-21 | 2 | 25% | 2217 | 2207 | -10 | 1 |
| classical | 09-22 | 1 | 100% | 2207 | 2221 | +14 | 0 |

Lichess rating-history endpoint (month index is 0-based in the API, so `8` = September):

- Bullet: 13/09 2183, 14/09 2167, 15/09 2180, 16/09 2182, 17/09 2176, 18/09 2196, 19/09 2171, 20/09 2177, 21/09 2185, 22/09 2191, 23/09 2183
- Blitz: 13/09 2182, 14/09 2135, 15/09 2157, 16/09 2174, 17/09 2178, 18/09 2164, 19/09 2161, 20/09 2158, 21/09 2172, 22/09 2181, 23/09 2169
- Rapid: 13/09 2208, 14/09 2228, 15/09 2245, 16/09 2217, 17/09 2205, 18/09 2203, 19/09 2196, 20/09 2209, 21/09 2268, 22/09 2272, 23/09 2271
- Classical: 13/09 2271, 14/09 2253, 15/09 2253, 16/09 2261, 17/09 2270, 18/09 2286, 19/09 2284, 20/09 2217, 21/09 2207, 22/09 2221

## 3. Results by opponent rating band (last 300, rated)

Band = opponent rating minus our pre-game rating.

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| < -200 | 23 | 16/2/5 | 73.9% | 84.9% | 2180 | 1811 | 1992 |
| -200..-100 | 50 | 37/5/8 | 79.0% | 69.7% | 2178 | 2033 | 2263 |
| -100..0 | 59 | 27/17/15 | 60.2% | 56.8% | 2202 | 2154 | 2225 |
| 0..100 | 57 | 23/12/22 | 50.9% | 44.1% | 2237 | 2278 | 2284 |
| 100..200 | 43 | 9/3/31 | 24.4% | 29.5% | 2185 | 2337 | 2140 |
| 200 + | 30 | 1/2/27 | 6.7% | 11.7% | 2193 | 2597 | 2138 |

Absolute opponent rating (rated):

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| 1800-1899 | 5 | 4/0/1 | 80.0% | 85.1% | 2174 | 1871 | 2112 |
| 1900-1999 | 16 | 11/1/4 | 71.9% | 77.4% | 2173 | 1958 | 2121 |
| 2000-2099 | 53 | 39/5/9 | 78.3% | 68.2% | 2177 | 2044 | 2267 |
| 2100-2199 | 50 | 22/13/15 | 57.0% | 55.3% | 2186 | 2148 | 2197 |
| 2200-2299 | 38 | 16/9/13 | 53.9% | 43.9% | 2215 | 2259 | 2286 |
| 2300-2399 | 64 | 14/11/39 | 30.5% | 35.6% | 2229 | 2336 | 2192 |
| 2400-2499 | 14 | 2/1/11 | 17.9% | 18.7% | 2194 | 2453 | 2188 |
| 2500-2599 | 3 | 0/0/3 | 0.0% | 11.9% | 2177 | 2524 | 1848 |
| 2600-2699 | 6 | 0/0/6 | 0.0% | 7.2% | 2179 | 2626 | 1950 |

## 4. How games end (last 300, all)

| speed | result | status / kind | count |
|---|---|---|---|
| bullet | draw | flag vs insufficient material | 1 |
| bullet | draw | insufficient material | 1 |
| bullet | draw | threefold repetition | 6 |
| bullet | loss | mate | 30 |
| bullet | loss | outoftime | 7 |
| bullet | win | mate | 45 |
| bullet | win | resign | 2 |
| blitz | draw | fifty-move rule | 4 |
| blitz | draw | insufficient material | 3 |
| blitz | draw | threefold repetition | 13 |
| blitz | loss | mate | 36 |
| blitz | loss | outoftime | 8 |
| blitz | win | mate | 53 |
| blitz | win | resign | 5 |
| rapid | draw | fifty-move rule | 2 |
| rapid | draw | insufficient material | 2 |
| rapid | draw | stalemate | 1 |
| rapid | draw | threefold repetition | 8 |
| rapid | loss | mate | 16 |
| rapid | loss | outoftime | 17 |
| rapid | win | mate | 27 |
| rapid | win | outoftime | 1 |
| rapid | win | resign | 3 |
| classical | draw | insufficient material | 3 |
| classical | loss | outoftime | 3 |
| classical | win | mate | 2 |
| classical | win | resign | 1 |
| ALL | draw | fifty-move rule | 6 |
| ALL | draw | flag vs insufficient material | 1 |
| ALL | draw | insufficient material | 9 |
| ALL | draw | stalemate | 1 |
| ALL | draw | threefold repetition | 27 |
| ALL | loss | mate | 82 |
| ALL | loss | outoftime | 35 |
| ALL | win | mate | 127 |
| ALL | win | outoftime | 1 |
| ALL | win | resign | 11 |

Draws, material at the end from our side (pawn=1, minor=3, rook=5, queen=9):

| draw kind | n | we were ahead >=2 | level (-1..+1) | we were behind <=-2 | median plies |
|---|---|---|---|---|---|
| fifty-move rule | 6 | 3 | 3 | 0 | 270 |
| flag vs insufficient material | 1 | 1 | 0 | 0 | 129 |
| insufficient material | 9 | 0 | 5 | 4 | 192 |
| stalemate | 1 | 1 | 0 | 0 | 266 |
| threefold repetition | 27 | 4 | 18 | 5 | 114 |

Draws where we were ahead by 2+ points of material:

| game | speed | kind | material | plies | opp | opp rating |
|---|---|---|---|---|---|---|
| [6POlSzc0](https://lichess.org/6POlSzc0) | bullet | flag vs insufficient material | +14 | 129 | jangine | 2009 |
| [eDC8WcGI](https://lichess.org/eDC8WcGI) | rapid | stalemate | +9 | 266 | RavenEngine | 2247 |
| [sXWkqol4](https://lichess.org/sXWkqol4) | blitz | fifty-move rule | +8 | 270 | rchessengine | 2149 |
| [5ojKo53f](https://lichess.org/5ojKo53f) | rapid | threefold repetition | +3 | 30 | FataliiBot | 2348 |
| [6Il7C02I](https://lichess.org/6Il7C02I) | blitz | fifty-move rule | +2 | 159 | ChronicGambler | 1588 |
| [UAuzblWw](https://lichess.org/UAuzblWw) | bullet | threefold repetition | +2 | 162 | ? | 0 |
| [KhKyOYrJ](https://lichess.org/KhKyOYrJ) | rapid | threefold repetition | +2 | 93 | rchessengine | 2222 |
| [xT3hvB1J](https://lichess.org/xT3hvB1J) | blitz | fifty-move rule | +2 | 368 | georgii_ai | 1942 |
| [ocqCKkgH](https://lichess.org/ocqCKkgH) | bullet | threefold repetition | +2 | 310 | TalosBot | 2457 |

Losses, material at the end from our side:

| status | n | we were ahead >=2 | level | behind <=-2 | median plies |
|---|---|---|---|---|---|
| mate | 82 | 0 | 3 | 79 | 134 |
| outoftime | 35 | 7 | 24 | 4 | 48 |

## 5. Clock use (last 300)

Clock values are our remaining clock after our move N, seconds. `left at end` = our last clock / initial. `<10 s` = our clock fell under 10 s at least once (flag entry excluded).

| speed | games w/ clocks | median left @ move 20 (% of initial) | median left @ move 40 | median left at end (% init) | opp median left at end (% init) | games <10 s | lost of those | median our think/move s | median opp think/move s |
|---|---|---|---|---|---|---|---|---|---|
| bullet | 92 | 61.6% | 39.9% | 38.8% | 58.4% | 0 | 0 | 2.0 | 1.3 |
| blitz | 122 | 66.8% | 42.9% | 40.6% | 27.0% | 0 | 0 | 3.6 | 3.0 |
| rapid | 77 | 79.1% | 50.9% | 36.5% | 29.5% | 0 | 0 | 10.3 | 8.8 |
| classical | 9 | 84.3% | 80.5% | 44.4% | 37.9% | 0 | 0 | 19.5 | 29.0 |

### Our losses on time: 35 of 117 losses

`last clock` = our clock after our last completed move = the time the unfinished final think burned. `max think` = longest completed think before that.

| game | tc | colour | our moves | @20 s | @40 s | min before flag s | last clock s | max think s (move) | opp clock at end s | material | concurrent games |
|---|---|---|---|---|---|---|---|---|---|---|---|
| [ix0NpivB](https://lichess.org/ix0NpivB) 09-17 09:39 | 180+0 | black | 10 |  |  | 114.1 | 114.1 | 14.7 (#3) | 145.9 | +0 | 0.00 |
| [Go328fsQ](https://lichess.org/Go328fsQ) 09-17 16:58 | 120+1 | black | 21 | 71.5 |  | 69.5 | 69.5 | 4.3 (#3) | 120.1 | -1 | 0.00 |
| [3Y2LOLS7](https://lichess.org/3Y2LOLS7) 09-17 17:36 | 120+1 | black | 55 | 74.0 | 47.4 | 33.9 | 33.9 | 4.4 (#3) | 14.5 | -3 | 1.00 |
| [JCaxxkC1](https://lichess.org/JCaxxkC1) 09-19 02:35 | 60+2 | white | 68 | 50.6 | 44.9 | 42.3 | 46.7 | 2.6 (#4) | 19.9 | -7 | 0.00 |
| [neXYl8Vh](https://lichess.org/neXYl8Vh) 09-19 09:31 | 300+3 | white | 2 |  |  | 291.6 | 291.6 | 0.0 (#0) | 302.9 | +0 | 0.00 |
| [0eDUqd2W](https://lichess.org/0eDUqd2W) 09-19 10:44 | 120+1 | black | 33 | 71.8 |  | 53.7 | 53.7 | 4.4 (#3) | 101.3 | +9 | 0.00 |
| [toE5toS5](https://lichess.org/toE5toS5) 09-19 11:29 | 300+7 | black | 44 | 218.8 | 175.8 | 170.3 | 170.3 | 12.6 (#3) | 68.5 | +3 | 0.58 |
| [Zpp7h2Cr](https://lichess.org/Zpp7h2Cr) 09-19 11:43 | 120+1 | black | 39 | 72.4 |  | 47.9 | 47.9 | 4.4 (#3) | 98.1 | +2 | 0.91 |
| [0KP46fpN](https://lichess.org/0KP46fpN) 09-19 13:30 | 120+1 | black | 36 | 36.4 |  | 30.6 | 30.6 | 47.5 (#14) | 107.8 | +4 | 0.00 |
| [PO6Z3HkT](https://lichess.org/PO6Z3HkT) 09-19 18:04 | 300+3 | white | 152 | 181.7 | 119.1 | 66.2 | 79.9 | 11.1 (#3) | 13.0 | +0 | 0.00 |
| [ZkKSse4q](https://lichess.org/ZkKSse4q) 09-20 00:34 | 180+0 | white | 7 |  |  | 52.5 | 52.5 | 116.6 (#7) | 167.9 | +0 | 0.00 |
| [f2WnmBRa](https://lichess.org/f2WnmBRa) 09-20 04:39 | 600+2 | white | 11 |  |  | 461.6 | 461.6 | 15.9 (#5) | 492.8 | +0 | 0.00 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | white | 38 | 204.5 |  | 33.6 | 38.4 | 449.6 (#5) | 239.5 | +3 | 0.00 |
| [43PSibRm](https://lichess.org/43PSibRm) 09-20 16:37 | 1500+2 | white | 23 | 1043.0 |  | 282.7 | 282.7 | 722.8 (#23) | 1207.5 | -1 | 0.00 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | white | 17 |  |  | 182.0 | 182.0 | 814.2 (#8) | 1525.4 | +0 | 0.00 |
| [VzxJKfbv](https://lichess.org/VzxJKfbv) 09-21 03:27 | 900+10 | white | 24 | 813.3 |  | 785.4 | 785.4 | 19.6 (#7) | 239.5 | +1 | 0.00 |
| [ByHEP3lj](https://lichess.org/ByHEP3lj) 09-21 09:00 | 1800+2 | white | 17 |  |  | 488.3 | 488.3 | 1058.5 (#17) | 1590.6 | +0 | 0.00 |
| [7SVrRWpU](https://lichess.org/7SVrRWpU) 09-21 17:25 | 600+5 | black | 20 | 455.5 |  | 455.5 | 455.5 | 16.3 (#14) | 272.2 | +0 | 2.16 |
| [2zWK7gUF](https://lichess.org/2zWK7gUF) 09-21 17:47 | 600+5 | black | 90 | 505.0 | 306.8 | 139.9 | 139.9 | 16.0 (#25) | 22.6 | -1 | 2.34 |
| [ODpwzRPi](https://lichess.org/ODpwzRPi) 09-21 17:55 | 600+5 | white | 78 | 503.9 | 305.5 | 43.5 | 65.1 | 357.6 (#71) | 36.4 | +0 | 2.04 |
| [j9rPRZOT](https://lichess.org/j9rPRZOT) 09-21 18:02 | 600+5 | white | 43 | 503.3 | 327.2 | 304.0 | 304.0 | 16.2 (#26) | 269.4 | +0 | 2.79 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | black | 5 |  |  | 467.8 | 467.8 | 69.0 (#4) | 616.8 | +1 | 0.39 |
| [9yt0U3eo](https://lichess.org/9yt0U3eo) 09-21 20:34 | 600+5 | white | 53 | 505.4 | 296.5 | 282.4 | 306.2 | 23.1 (#40) | 176.7 | -2 | 1.15 |
| [VTfXUuoe](https://lichess.org/VTfXUuoe) 09-21 20:37 | 600+5 | white | 34 | 521.5 |  | 369.4 | 369.4 | 27.7 (#25) | 117.7 | +0 | 1.34 |
| [9OCZn3tT](https://lichess.org/9OCZn3tT) 09-21 20:49 | 600+5 | black | 9 |  |  | 544.5 | 544.5 | 16.0 (#4) | 430.2 | +0 | 2.12 |
| [M4OsJA0m](https://lichess.org/M4OsJA0m) 09-21 20:53 | 600+5 | white | 5 |  |  | 572.2 | 572.2 | 16.0 (#4) | 619.7 | +0 | 3.00 |
| [qaOCjwPE](https://lichess.org/qaOCjwPE) 09-21 21:07 | 600+5 | white | 35 | 504.8 |  | 347.7 | 347.7 | 16.0 (#29) | 293.2 | +1 | 0.87 |
| [focq66v8](https://lichess.org/focq66v8) 09-21 21:10 | 600+5 | black | 20 | 424.5 |  | 424.5 | 424.5 | 16.0 (#5) | 264.8 | +0 | 1.07 |
| [JoTrYF9Y](https://lichess.org/JoTrYF9Y) 09-21 21:21 | 600+5 | black | 2 |  |  | 600.0 | 605.0 | 0.0 (#0) | 544.6 | +0 | 1.62 |
| [1q6VdN8S](https://lichess.org/1q6VdN8S) 09-22 00:43 | 300+2 | white | 15 |  |  | 234.2 | 234.2 | 11.6 (#7) | 223.6 | +0 | 0.00 |
| [jVzbwbGF](https://lichess.org/jVzbwbGF) 09-22 03:20 | 300+0 | black | 22 | 254.6 |  | 248.2 | 248.2 | 4.1 (#9) | 197.9 | +0 | 0.00 |
| [ymteJEvj](https://lichess.org/ymteJEvj) 09-22 13:55 | 180+1 | black | 42 | 110.1 | 66.0 | 63.0 | 63.0 | 6.6 (#4) | 26.4 | +5 | 0.00 |
| [oumuVvMf](https://lichess.org/oumuVvMf) 09-23 00:59 | 60+30 | white | 8 |  |  | 60.0 | 222.4 | 15.9 (#8) | 168.5 | +0 | 0.00 |
| [NOwauSF0](https://lichess.org/NOwauSF0) 09-23 03:17 | 60+1 | black | 50 | 46.0 | 31.6 | 28.0 | 28.0 | 2.7 (#8) | 18.4 | +2 | 0.00 |
| [fd842nXj](https://lichess.org/fd842nXj) 09-23 11:54 | 300+1 | black | 26 | 193.2 |  | 159.8 | 159.8 | 10.9 (#7) | 155.0 | -3 | 0.14 |

Flag loss type: hang (>=10 s left, no move): 35

### Was the bot alive during each stall? (all games)

Stall window = from lastMoveAt (opponent's last move) to lastMoveAt + our remaining clock. `our moves elsewhere` = our moves in OTHER games whose estimated time (clock deltas scaled to createdAt..lastMoveAt, +-5 s) falls inside the window. `games started` = other games created inside it. `next game after flag` = minutes from the flag to the next game we started.

| game | tc | stall s | our moves elsewhere | other games in window | games started in window | next game after flag (min) |
|---|---|---|---|---|---|---|
| [hTmspQs0](https://lichess.org/hTmspQs0) 09-13 21:13 | 300+3 | 3 | 0 | 2 | 0 | 6 |
| [rUZ1clR4](https://lichess.org/rUZ1clR4) 09-14 09:11 | 600+2 | 586 | 0 | 0 | 0 | 748 |
| [B3VWgRTW](https://lichess.org/B3VWgRTW) 09-15 01:03 | 300+1 | 297 | 0 | 0 | 0 | 26 |
| [zZ268pfw](https://lichess.org/zZ268pfw) 09-15 02:11 | 600+5 | 436 | 1 | 1 | 0 | 13 |
| [PFO0TBCu](https://lichess.org/PFO0TBCu) 09-15 03:28 | 60+5 | 89 | 0 | 0 | 0 | 93 |
| [XcCUnY4l](https://lichess.org/XcCUnY4l) 09-15 07:40 | 180+0 | 141 | 0 | 0 | 0 | 63 |
| [MnRf6Ms5](https://lichess.org/MnRf6Ms5) 09-15 10:41 | 300+2 | 229 | 0 | 1 | 0 | 52 |
| [ufu0Plbo](https://lichess.org/ufu0Plbo) 09-15 12:10 | 300+1 | 132 | 0 | 0 | 0 | 46 |
| [i4FtWSpF](https://lichess.org/i4FtWSpF) 09-15 17:26 | 300+3 | 136 | 8 | 1 | 0 | 0 |
| [DfCma7PY](https://lichess.org/DfCma7PY) 09-15 18:05 | 300+3 | 136 | 0 | 1 | 0 | 46 |
| [8xjLdv8H](https://lichess.org/8xjLdv8H) 09-15 18:07 | 300+3 | 20 | 0 | 0 | 0 | 45 |
| [pLUqIY3m](https://lichess.org/pLUqIY3m) 09-15 19:03 | 600+3 | 270 | 0 | 0 | 0 | 9 |
| [qEOYFuWQ](https://lichess.org/qEOYFuWQ) 09-15 19:17 | 300+3 | 241 | 0 | 1 | 0 | 10 |
| [vsMd5KPN](https://lichess.org/vsMd5KPN) 09-15 19:43 | 600+1 | 343 | 0 | 1 | 0 | 11 |
| [gsULa4Jd](https://lichess.org/gsULa4Jd) 09-15 19:47 | 300+3 | 145 | 0 | 0 | 0 | 14 |
| [zfQR8dYn](https://lichess.org/zfQR8dYn) 09-15 21:35 | 120+1 | 67 | 0 | 0 | 0 | 17 |
| [FjBA92RT](https://lichess.org/FjBA92RT) 09-15 21:36 | 120+1 | 90 | 0 | 1 | 0 | 16 |
| [eBcLYbOh](https://lichess.org/eBcLYbOh) 09-16 12:09 | 600+3 | 194 | 0 | 0 | 0 | 64 |
| [8rvyvsmD](https://lichess.org/8rvyvsmD) 09-16 18:49 | 300+3 | 100 | 0 | 1 | 0 | 25 |
| [XPTyTcss](https://lichess.org/XPTyTcss) 09-16 18:51 | 300+3 | 126 | 0 | 0 | 0 | 25 |
| [ix0NpivB](https://lichess.org/ix0NpivB) 09-17 09:39 | 180+0 | 114 | 0 | 0 | 0 | 48 |
| [Go328fsQ](https://lichess.org/Go328fsQ) 09-17 16:58 | 120+1 | 69 | 0 | 0 | 0 | 3 |
| [3Y2LOLS7](https://lichess.org/3Y2LOLS7) 09-17 17:36 | 120+1 | 34 | 0 | 1 | 0 | 10 |
| [JCaxxkC1](https://lichess.org/JCaxxkC1) 09-19 02:35 | 60+2 | 47 | 0 | 0 | 0 | 173 |
| [neXYl8Vh](https://lichess.org/neXYl8Vh) 09-19 09:31 | 300+3 | 292 | 0 | 0 | 0 | 20 |
| [0eDUqd2W](https://lichess.org/0eDUqd2W) 09-19 10:44 | 120+1 | 54 | 0 | 0 | 0 | 41 |
| [toE5toS5](https://lichess.org/toE5toS5) 09-19 11:29 | 300+7 | 170 | 0 | 1 | 0 | 29 |
| [Zpp7h2Cr](https://lichess.org/Zpp7h2Cr) 09-19 11:43 | 120+1 | 48 | 0 | 0 | 0 | 31 |
| [0KP46fpN](https://lichess.org/0KP46fpN) 09-19 13:30 | 120+1 | 31 | 0 | 0 | 0 | 6 |
| [PO6Z3HkT](https://lichess.org/PO6Z3HkT) 09-19 18:04 | 300+3 | 80 | 0 | 0 | 0 | 309 |
| [ZkKSse4q](https://lichess.org/ZkKSse4q) 09-20 00:34 | 180+0 | 53 | 0 | 0 | 0 | 242 |
| [f2WnmBRa](https://lichess.org/f2WnmBRa) 09-20 04:39 | 600+2 | 462 | 0 | 0 | 0 | 422 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 38 | 0 | 0 | 0 | 254 |
| [43PSibRm](https://lichess.org/43PSibRm) 09-20 16:37 | 1500+2 | 283 | 0 | 0 | 0 | 56 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 182 | 0 | 0 | 0 | 106 |
| [VzxJKfbv](https://lichess.org/VzxJKfbv) 09-21 03:27 | 900+10 | 785 | 0 | 0 | 0 | 10 |
| [ByHEP3lj](https://lichess.org/ByHEP3lj) 09-21 09:00 | 1800+2 | 488 | 0 | 0 | 0 | 32 |
| [7SVrRWpU](https://lichess.org/7SVrRWpU) 09-21 17:25 | 600+5 | 456 | 51 | 4 | 1 | 3 |
| [2zWK7gUF](https://lichess.org/2zWK7gUF) 09-21 17:47 | 600+5 | 140 | 0 | 1 | 0 | 3 |
| [ODpwzRPi](https://lichess.org/ODpwzRPi) 09-21 17:55 | 600+5 | 65 | 1 | 1 | 0 | 126 |
| [j9rPRZOT](https://lichess.org/j9rPRZOT) 09-21 18:02 | 600+5 | 304 | 0 | 2 | 0 | 1 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | 468 | 0 | 0 | 0 | 118 |
| [9yt0U3eo](https://lichess.org/9yt0U3eo) 09-21 20:34 | 600+5 | 306 | 0 | 1 | 0 | 8 |
| [VTfXUuoe](https://lichess.org/VTfXUuoe) 09-21 20:37 | 600+5 | 369 | 0 | 2 | 0 | 7 |
| [9OCZn3tT](https://lichess.org/9OCZn3tT) 09-21 20:49 | 600+5 | 544 | 0 | 0 | 0 | 4 |
| [M4OsJA0m](https://lichess.org/M4OsJA0m) 09-21 20:53 | 600+5 | 572 | 0 | 3 | 0 | 4 |
| [qaOCjwPE](https://lichess.org/qaOCjwPE) 09-21 21:07 | 600+5 | 348 | 0 | 1 | 0 | 12 |
| [focq66v8](https://lichess.org/focq66v8) 09-21 21:10 | 600+5 | 425 | 0 | 2 | 0 | 11 |
| [JoTrYF9Y](https://lichess.org/JoTrYF9Y) 09-21 21:21 | 600+5 | 605 | 0 | 0 | 0 | 8 |
| [1q6VdN8S](https://lichess.org/1q6VdN8S) 09-22 00:43 | 300+2 | 234 | 0 | 0 | 0 | 99 |
| [jVzbwbGF](https://lichess.org/jVzbwbGF) 09-22 03:20 | 300+0 | 248 | 0 | 0 | 0 | 22 |
| [ymteJEvj](https://lichess.org/ymteJEvj) 09-22 13:55 | 180+1 | 63 | 0 | 0 | 0 | 7 |
| [oumuVvMf](https://lichess.org/oumuVvMf) 09-23 00:59 | 60+30 | 222 | 0 | 0 | 0 | 132 |
| [NOwauSF0](https://lichess.org/NOwauSF0) 09-23 03:17 | 60+1 | 28 | 0 | 0 | 0 | 64 |
| [fd842nXj](https://lichess.org/fd842nXj) 09-23 11:54 | 300+1 | 160 | 0 | 0 | 0 | - |

Flag losses in all games: 55. Bot provably moving elsewhere during the stall: 4. Other games overlapped but we made no move in any of them: 16.

Opponents who flagged against us: 1.

### Single thinks over 30 s (last 300, completed moves)

128 thinks over 30 s.

| game | tc | move | think s | clock before s | result |
|---|---|---|---|---|---|
| [ByHEP3lj](https://lichess.org/ByHEP3lj) 09-21 09:00 | 1800+2 | 17 | 1058.5 | 1544.8 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 8 | 814.2 | 1663.6 | 0.0 |
| [43PSibRm](https://lichess.org/43PSibRm) 09-20 16:37 | 1500+2 | 23 | 722.8 | 1003.5 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 5 | 449.6 | 850.0 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 16 | 428.6 | 636.8 | 0.0 |
| [ODpwzRPi](https://lichess.org/ODpwzRPi) 09-21 17:55 | 600+5 | 71 | 357.6 | 396.2 | 0.0 |
| [0zIgtr7g](https://lichess.org/0zIgtr7g) 09-23 08:57 | 300+2 | 10 | 193.4 | 281.7 | 0.5 |
| [ZkKSse4q](https://lichess.org/ZkKSse4q) 09-20 00:34 | 180+0 | 7 | 116.6 | 169.2 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 13 | 99.6 | 783.2 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 24 | 76.3 | 168.3 | 0.0 |
| [uZEZNd9T](https://lichess.org/uZEZNd9T) 09-17 17:31 | 300+2 | 37 | 75.3 | 113.5 | 1.0 |
| [ZP5sb0pG](https://lichess.org/ZP5sb0pG) 09-21 14:45 | 600+5 | 16 | 70.0 | 558.7 | 1.0 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | 4 | 69.0 | 578.4 | 0.0 |
| [oVvHY7Br](https://lichess.org/oVvHY7Br) 09-21 14:26 | 600+5 | 62 | 67.6 | 195.8 | 1.0 |
| [vqflBfpz](https://lichess.org/vqflBfpz) 09-21 14:33 | 600+5 | 36 | 66.2 | 299.9 | 0.5 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | 5 | 51.6 | 514.4 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 30 | 51.5 | 85.0 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 9 | 49.2 | 365.7 | 0.0 |
| [0KP46fpN](https://lichess.org/0KP46fpN) 09-19 13:30 | 120+1 | 14 | 47.5 | 86.3 | 0.0 |
| [6QJpC01Q](https://lichess.org/6QJpC01Q) 09-19 05:45 | 1800+20 | 92 | 46.2 | 2187.1 | 0.5 |
| [6QJpC01Q](https://lichess.org/6QJpC01Q) 09-19 05:45 | 1800+20 | 93 | 46.0 | 2160.8 | 0.5 |
| [9d74Eo7j](https://lichess.org/9d74Eo7j) 09-21 07:12 | 1800+20 | 81 | 45.5 | 2164.3 | 0.5 |
| [6QJpC01Q](https://lichess.org/6QJpC01Q) 09-19 05:45 | 1800+20 | 94 | 45.3 | 2134.8 | 0.5 |
| [9d74Eo7j](https://lichess.org/9d74Eo7j) 09-21 07:12 | 1800+20 | 82 | 45.0 | 2138.8 | 0.5 |
| [XDvXOTkM](https://lichess.org/XDvXOTkM) 09-19 08:54 | 120+2 | 7 | 44.5 | 107.2 | 0.0 |

## 6. Most frequent opponents (last 300, rated)

| opponent | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| FataliiBot | 36 | 3/3/30 | 12.5% | 33.2% | 2211 | 2335 | 1997 |
| jangine | 30 | 23/3/4 | 81.7% | 69.3% | 2175 | 2032 | 2292 |
| likeawizard-bot | 20 | 7/5/8 | 47.5% | 46.4% | 2284 | 2309 | 2292 |
| MrTonnerre | 16 | 5/4/7 | 43.8% | 47.5% | 2190 | 2207 | 2163 |
| RavenEngine | 16 | 12/4/0 | 87.5% | 48.4% | 2246 | 2258 | 2596 |
| rchessengine | 9 | 2/5/2 | 50.0% | 53.0% | 2172 | 2151 | 2151 |
| TucuEngine | 8 | 6/0/2 | 75.0% | 62.9% | 2200 | 2108 | 2299 |
| JewkieBot-Dev | 7 | 3/1/3 | 50.0% | 56.7% | 2171 | 2124 | 2124 |
| DeMen100ns-bot | 6 | 2/3/1 | 58.3% | 58.9% | 2198 | 2136 | 2194 |
| walpurti | 6 | 4/0/2 | 66.7% | 75.0% | 2214 | 2022 | 2143 |
| GreenseerEngine | 6 | 5/1/0 | 91.7% | 60.2% | 2194 | 2121 | 2538 |
| StockDog-bot | 6 | 1/1/4 | 25.0% | 20.2% | 2174 | 2419 | 2228 |
| georgii_ai | 5 | 2/2/1 | 60.0% | 74.3% | 2176 | 1988 | 2059 |
| artidigm_research | 4 | 0/2/2 | 25.0% | 33.3% | 2180 | 2311 | 2120 |
| TalosBot | 4 | 0/1/3 | 12.5% | 17.1% | 2184 | 2459 | 2121 |

66 distinct rated opponents. Top 5 opponents = 118 of 262 rated games.

## 7. Before/after engine changes (all games)

The running binary is not recorded per game, so the cut points are commit times of the champion changes; a new champion only goes live when the bot is restarted after the commit. Games where our rating was still provisional (first day, from 3000) are excluded.

Commits touching champion_bot.json / lichessbot / cmd/lichessbot in the game period:

- 09-13 13:22 `a7a3dd8` A lichess bot that plays the champion, TDD, 100 percent coverage on the new package
- 09-13 13:26 `8435e54` The bot no longer tries to accept its own outgoing challenges
- 09-13 13:32 `59af7a9` Detect our own outgoing challenges by challenger id, not a nonexistent field
- 09-13 14:33 `5711be1` Time management comes from the game's own clock, not champion.json
- 09-13 15:07 `784b860` A game rebuilt from a FEN must track repetition, like one from the start
- 09-13 15:22 `95395eb` Cap how many lichess games are played at once
- 09-13 15:23 `6a802af` Check for our own outgoing challenge before the game limit, not after
- 09-13 16:00 `61434d5` Play the lichess bot with an opening book built from real games
- 09-13 16:51 `67168db` Build the opening book from the whole game database, not a quarter of it
- 09-13 19:20 `32a5526` Spend the clock in fast games, and keep bullet and blitz actually running
- 09-13 20:13 `c1a9364` Study all 75 lichess games, and correct three wrong readings
- 09-13 20:15 `c6ccaad` Give the bot four search threads, not two
- 09-13 20:41 `2598242` Think properly in games that have no clock
- 09-13 20:59 `61c32c2` Reconnect the lichess bot instead of going deaf on a dead socket
- 09-13 21:06 `6c1855c` Abort a dead lichess stream by cancelling it, not by closing it
- 09-13 21:14 `948f7c3` Let the transport ping a quiet lichess stream instead of timing it out
- 09-13 21:18 `6e46fdf` Log the games the bot plays
- 09-13 21:35 `c6bbedd` Stop the clock rule bleeding itself into a permanent scramble
- 09-13 21:42 `9021029` Add clockaudit, which replays real games through the real budget rule
- 09-13 21:47 `b8429d1` Log what each move really costs, split into search and round trip
- 09-13 21:57 `7bcfc2c` Charge the budget for sending the move, and play rated games only
- 09-13 22:06 `57f84dd` Accept unrated games again, and stop claiming a cause I had not proved
- 09-13 22:41 `f7beb04` Ship the better book, and stop reserving an overhead that is not there
- 09-13 23:09 `3f068b6` Track the hoarding case: a short base with a fat increment
- 09-14 02:22 `43e68fb` Measure the per move overhead instead of assuming one
- 09-14 04:09 `f9c6bfb` Scale the per move ceiling with the clock, so slow games use theirs
- 09-14 08:45 `c1d8bc7` Revert the book: my rebuild opened 1.Na3 in every game
- 09-14 08:54 `171d0dd` Weight book moves the way PolyGlot does: a count, not a rate
- 09-14 10:38 `0570206` Reconnect to a game whose stream drops, instead of abandoning it
- 09-16 21:15 `66eed6b` Champion drops the hand evaluation: +33 Elo at 10 ms a move
- 09-16 21:33 `4c5ba52` Adopt improving; report nodes, nps and time on the UCI info line
- 09-18 19:48 `2aabc1d` The champion learns from each position once
- 09-18 22:38 `38841d9` Razoring adopted, and the UI champion catches up
- 09-19 03:29 `b23b2d5` Retrain on 11.5M positions: +9 +/- 10 over 5,100 games
- 09-19 05:57 `d38f5bd` Cheap labels win: +12 +/- 11 from a 26 minute batch
- 09-19 07:50 `9aebdec` 16.8M positions: +22 +/- 16, the largest network gain measured here
- 09-19 08:53 `b1684bb` The ruler works at 1 s and not at 10 ms, which corrects the retraction
- 09-19 23:37 `aedca94` feat(engine): adopt conthist and histlmr as champion rung 12 (+23 Elo)
- 09-20 21:27 `5dfad5a` feat(champion): adopt 20.06M net as rung 13 (+40 Elo)
- 09-21 20:49 `881c534` feat(champion): adopt 20.80M net as rung 14 (+6 Elo)
- 09-21 22:33 `9631ce2` feat(champion): adopt 21.20M net as rung 15 (+44 Elo)
- 09-21 23:34 `1cd16e0` feat(champion): adopt 21.60M net as rung 16 (+54 Elo)
- 09-22 21:28 `52a6e9d` perf(engine): zero-copy transposition table lookups and persistent table reuse
- 09-22 22:36 `1ccaed2` fix(lichessbot): cap per-move budget at 30s to avoid very long thinks
- 09-23 00:30 `36c3b56` feat(champion): adopt nullpieces
- 09-23 07:44 `03bd53a` feat(engine): drawscale shrinks scores in endgames the side ahead cannot win

Split by date around the major champion changes (rated, non-provisional):

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| to 09-16 21:15 (hand eval champion) | 183 | 74/28/81 | 48.1% | 47.9% | 2180 | 2201 | 2188 |
| to 09-16 21:15 (hand eval champion), flag losses removed | 164 | 74/28/62 | 53.7% | 48.8% | 2180 | 2192 | 2218 |
| 09-16 21:15 to 09-19 23:37 (NNUE-only, retrains) | 132 | 53/19/60 | 47.3% | 49.9% | 2181 | 2183 | 2165 |
| 09-16 21:15 to 09-19 23:37 (NNUE-only, retrains), flag losses removed | 122 | 53/19/50 | 51.2% | 49.3% | 2181 | 2187 | 2196 |
| 09-19 23:37 to 09-22 22:36 (rungs 12-23) | 105 | 52/16/37 | 57.1% | 48.8% | 2224 | 2237 | 2287 |
| 09-19 23:37 to 09-22 22:36 (rungs 12-23), flag losses removed | 85 | 52/16/17 | 70.6% | 49.3% | 2217 | 2228 | 2380 |
| after 09-22 22:36 (30 s cap, nullpieces) | 18 | 4/5/9 | 36.1% | 49.7% | 2191 | 2220 | 2121 |
| after 09-22 22:36 (30 s cap, nullpieces), flag losses removed | 15 | 4/5/6 | 43.3% | 46.5% | 2187 | 2246 | 2199 |

## 8. Concurrent games (all games, rated, non-provisional)

Mean number of other bot games in flight during the game. All share the same 4 search threads' cores.

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| alone (<0.25) | 194 | 84/28/82 | 50.5% | 50.2% | 2185 | 2192 | 2195 |
| 0.25-1 | 128 | 51/16/61 | 46.1% | 47.5% | 2182 | 2202 | 2175 |
| 1-2 | 91 | 41/16/34 | 53.8% | 48.6% | 2201 | 2215 | 2242 |
| 2+ | 25 | 7/8/10 | 44.0% | 45.5% | 2261 | 2293 | 2251 |

bullet:

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| alone (<0.25) | 64 | 29/8/27 | 51.6% | 48.2% | 2180 | 2210 | 2221 |
| 0.25-1 | 46 | 18/5/23 | 44.6% | 49.9% | 2184 | 2186 | 2148 |
| 1+ | 35 | 14/7/14 | 50.0% | 51.9% | 2186 | 2180 | 2180 |

blitz:

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| alone (<0.25) | 89 | 40/14/35 | 52.8% | 53.7% | 2167 | 2127 | 2146 |
| 0.25-1 | 62 | 24/9/29 | 46.0% | 45.6% | 2165 | 2199 | 2171 |
| 1+ | 32 | 14/6/12 | 53.1% | 48.1% | 2162 | 2182 | 2203 |

rapid:

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| alone (<0.25) | 34 | 13/4/17 | 44.1% | 43.3% | 2226 | 2327 | 2286 |
| 0.25-1 | 20 | 9/2/9 | 50.0% | 48.1% | 2230 | 2245 | 2245 |
| 1+ | 49 | 20/11/18 | 52.0% | 45.0% | 2267 | 2301 | 2315 |

classical:

| slice | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| alone (<0.25) | 7 | 2/2/3 | 42.9% | 56.1% | 2246 | 2200 | 2150 |
| 0.25-1 | 0 | | | | | | |
| 1+ | 0 | | | | | | |

## 9. Openings (last 300, rated, ECO family, n>=6)

| opening (first word group) | games | W/D/L | score | expected | our avg rating | opp avg rating | performance |
|---|---|---|---|---|---|---|---|
| Sicilian Defense | 60 | 34/10/16 | 65.0% | 48.1% | 2217 | 2249 | 2357 |
| French Defense | 43 | 17/6/20 | 46.5% | 49.5% | 2184 | 2209 | 2185 |
| Ruy Lopez | 22 | 9/3/10 | 47.7% | 44.9% | 2213 | 2263 | 2247 |
| Four Knights Game | 20 | 4/4/12 | 30.0% | 44.9% | 2181 | 2208 | 2060 |
| Queen's Pawn Game | 17 | 6/0/11 | 35.3% | 40.3% | 2183 | 2257 | 2152 |
| Queen's Gambit Declined | 13 | 5/4/4 | 53.8% | 59.1% | 2186 | 2118 | 2144 |
| English Opening | 10 | 4/2/4 | 50.0% | 46.6% | 2178 | 2216 | 2216 |
| Indian Defense | 9 | 3/2/4 | 44.4% | 62.3% | 2190 | 2070 | 2032 |
| King's Indian Defense | 8 | 4/2/2 | 62.5% | 43.8% | 2230 | 2314 | 2402 |
| Scotch Game | 7 | 3/2/2 | 57.1% | 51.2% | 2198 | 2194 | 2244 |
| Rapport-Jobava System | 6 | 3/0/3 | 50.0% | 55.9% | 2195 | 2147 | 2147 |

## 10. Rating points ledger (sum of our lichess ratingDiff)

| bucket | last 300 rated: games | points | all rated non-provisional: games | points |
|---|---|---|---|---|
| our losses on time (stalls) | 33 | -256 | 52 | -456 |
| vs FataliiBot (excluding stalls) | 27 | -45 | 39 | -68 |
| opponent 200+ above us (excl. above) | 28 | -12 | 45 | -46 |
| everything else | 174 | +346 | 302 | +628 |
| total | 262 | +33 | 438 | +58 |

Per speed, points lost to our losses on time (last 300 rated):

- bullet: 7 flag losses, -49 points
- blitz: 8 flag losses, -46 points
- rapid: 15 flag losses, -83 points
- classical: 3 flag losses, -78 points

### Completed thinks far above the budget rule (all games)

Upper bound used: max(15 s, clock/50) + 10 s. The rule never allows more, so these are stalls the bot recovered from, not long searches.

42 such moves in 28 games; 28 of them in games we lost.

| game | tc | move | think s | clock before s | result |
|---|---|---|---|---|---|
| [9PZvBKIy](https://lichess.org/9PZvBKIy) 09-13 13:23 | 600+5 | 14 | 86.6 | 647.0 | 1.0 |
| [ALzWe84Z](https://lichess.org/ALzWe84Z) 09-13 15:05 | 600+5 | 34 | 25.5 | 260.2 | 0.0 |
| [fQjoMFEI](https://lichess.org/fQjoMFEI) 09-13 15:08 | 600+5 | 27 | 30.2 | 324.1 | 0.0 |
| [w2CvsaKf](https://lichess.org/w2CvsaKf) 09-14 03:15 | 2100+2 | 89 | 32.2 | 933.7 | 0.0 |
| [weXhJBBb](https://lichess.org/weXhJBBb) 09-15 01:40 | 1800+20 | 37 | 473.6 | 1488.0 | 0.5 |
| [weXhJBBb](https://lichess.org/weXhJBBb) 09-15 01:40 | 1800+20 | 39 | 43.8 | 1032.6 | 0.5 |
| [weXhJBBb](https://lichess.org/weXhJBBb) 09-15 01:40 | 1800+20 | 42 | 30.4 | 1006.4 | 0.5 |
| [weXhJBBb](https://lichess.org/weXhJBBb) 09-15 01:40 | 1800+20 | 43 | 57.4 | 996.0 | 0.5 |
| [lioJnpQO](https://lichess.org/lioJnpQO) 09-15 10:41 | 600+1 | 5 | 250.2 | 571.1 | 0.0 |
| [8xjLdv8H](https://lichess.org/8xjLdv8H) 09-15 18:07 | 300+3 | 22 | 159.6 | 176.8 | 0.0 |
| [jSRBu5hz](https://lichess.org/jSRBu5hz) 09-15 20:11 | 300+3 | 9 | 123.5 | 246.5 | 0.5 |
| [wL1iT2S2](https://lichess.org/wL1iT2S2) 09-16 01:36 | 600+10 | 40 | 25.8 | 374.5 | 1.0 |
| [fexh3I7c](https://lichess.org/fexh3I7c) 09-16 17:36 | 1800+1 | 46 | 72.9 | 746.8 | 0.5 |
| [8rvyvsmD](https://lichess.org/8rvyvsmD) 09-16 18:49 | 300+3 | 22 | 57.5 | 176.8 | 0.0 |
| [XPTyTcss](https://lichess.org/XPTyTcss) 09-16 18:51 | 300+3 | 19 | 45.4 | 190.1 | 0.0 |
| [uZEZNd9T](https://lichess.org/uZEZNd9T) 09-17 17:31 | 300+2 | 37 | 75.3 | 113.5 | 1.0 |
| [XDvXOTkM](https://lichess.org/XDvXOTkM) 09-19 08:54 | 120+2 | 7 | 44.5 | 107.2 | 0.0 |
| [0KP46fpN](https://lichess.org/0KP46fpN) 09-19 13:30 | 120+1 | 14 | 47.5 | 86.3 | 0.0 |
| [ZkKSse4q](https://lichess.org/ZkKSse4q) 09-20 00:34 | 180+0 | 7 | 116.6 | 169.2 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 5 | 449.6 | 850.0 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 9 | 49.2 | 365.7 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 15 | 28.8 | 274.4 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 21 | 29.0 | 204.5 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 24 | 76.3 | 168.3 | 0.0 |
| [iNgVEvGm](https://lichess.org/iNgVEvGm) 09-20 11:54 | 900+2 | 30 | 51.5 | 85.0 | 0.0 |
| [43PSibRm](https://lichess.org/43PSibRm) 09-20 16:37 | 1500+2 | 23 | 722.8 | 1003.5 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 8 | 814.2 | 1663.6 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 9 | 37.0 | 849.5 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 13 | 99.6 | 783.2 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 14 | 38.6 | 683.6 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 16 | 428.6 | 636.8 | 0.0 |
| [YDE3wFvS](https://lichess.org/YDE3wFvS) 09-20 18:21 | 1800+0 | 17 | 26.1 | 208.1 | 0.0 |
| [ByHEP3lj](https://lichess.org/ByHEP3lj) 09-21 09:00 | 1800+2 | 17 | 1058.5 | 1544.8 | 0.0 |
| [oVvHY7Br](https://lichess.org/oVvHY7Br) 09-21 14:26 | 600+5 | 62 | 67.6 | 195.8 | 1.0 |
| [vqflBfpz](https://lichess.org/vqflBfpz) 09-21 14:33 | 600+5 | 36 | 66.2 | 299.9 | 0.5 |
| [ZP5sb0pG](https://lichess.org/ZP5sb0pG) 09-21 14:45 | 600+5 | 16 | 70.0 | 558.7 | 1.0 |
| [ODpwzRPi](https://lichess.org/ODpwzRPi) 09-21 17:55 | 600+5 | 71 | 357.6 | 396.2 | 0.0 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | 4 | 69.0 | 578.4 | 0.0 |
| [j4yRerIq](https://lichess.org/j4yRerIq) 09-21 18:25 | 600+5 | 5 | 51.6 | 514.4 | 0.0 |
| [VTfXUuoe](https://lichess.org/VTfXUuoe) 09-21 20:37 | 600+5 | 25 | 27.7 | 477.9 | 0.0 |
| [tXUrJtc9](https://lichess.org/tXUrJtc9) 09-22 00:06 | 600+2 | 45 | 35.8 | 183.2 | 1.0 |
| [0zIgtr7g](https://lichess.org/0zIgtr7g) 09-23 08:57 | 300+2 | 10 | 193.4 | 281.7 | 0.5 |


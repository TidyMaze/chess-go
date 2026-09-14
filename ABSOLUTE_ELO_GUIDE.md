# Standard Methods for Global, Shareable Chess Engine Elo

## 10 Standard Ways to Establish an Absolute Elo

| # | Method | Authority | Format / Protocol | Shareability | Verdict |
|---|---|---|---|---|---|
| 1 | **Lichess Bot API** | Global (Lichess.org) | Connect via `lichess-bot` in rated blitz/rapid pool | Public profile URL with live Glicko-2 rating and game history | **Best Overall for Public Sharing** |
| 2 | **CCRL (Computer Chess Rating Lists)** | Computer Chess Standard | `cutechess-cli` against CCRL 40/15 anchor engines | Official rating on computerchess.org.uk | **Best Offline Standard** |
| 3 | **CEGT (Chess Engine Grand Tournament)** | Recognized Rating List | 40 moves in 20 min or 40 in 4 min | Published in CEGT tables | Strong alternative to CCRL |
| 4 | **Calibrated Stockfish Ladder via cutechess-cli** | Universal baseline | Match against SF UCI_Elo (1800, 2000, 2200, 2400, 2600, 2800) | JSON match records, repeatable by anyone with Stockfish binary | Best self-hosted method |
| 5 | **OpenBench / Fastchess SPRT** | Open Source Standard | Automated distributed testing against engine anchors | Web dashboard with Elo confidence intervals | Best for continuous engine development |
| 6 | **TCEC (Top Chess Engine Championship)** | Premier Elite League | Invited or qualification tournament | Streamed, archived games | High barrier to entry (top 20 engines) |
| 7 | **Lichess Bot Arena / Swiss Tournaments** | Competitive Bot Pool | Scheduled weekend engine tournaments | Tournament standings and tournament performance rating (TPR) | Excellent community proof |
| 8 | **STS (Strategic Test Suite) / BT2630** | Benchmark Suite | 1,000+ tactical & positional test positions (FEN) | Single number score (% solved) mapped to Elo | Deterministic, but measures suite solving rather than playing strength |
| 9 | **SSDF (Swedish Chess Computer Association)** | Historic Organization | Standard tournament controls on reference hardware | Published in quarterly reports | Slow, restricted to commercial/top engines |
| 10 | **FGRL (Fast GM Rating List)** | Rapid/Blitz Engine List | Fast time controls (1m+1s) | Public engine rating list | Focused on fast hardware and active engines |

---

## The Best Option: Lichess Bot Account

### Why It Is the Best
1. **Zero Argument**: Anyone in the world can click the link and inspect real games against real players.
2. **Standard Glicko-2**: Matches global chess rating distributions (Blitz, Rapid, Classical).
3. **Automated**: Runs 24/7 via open-source `lichess-bot` client using standard UCI protocol.
4. **Official Badge**: Lichess displays a purple `BOT` badge on the account.

### How to Deploy
1. Create a Lichess account (e.g. `chess-go-bot`).
2. Upgrade to Bot via API token: `POST https://lichess.org/api/bot/account/upgrade`.
3. Run `lichess-bot` bridge configured with `./play-bin -uci` or `chess-go` UCI executable.
4. Share URL: `https://lichess.org/@/chess-go-bot`.

---

## Best Offline/Engineering Standard: CCRL Protocol via `cutechess-cli`
If you want an official rating listed on [computerchess.org.uk](https://www.computerchess.org.uk/ccrl/4040/):
1. Use `cutechess-cli`.
2. Play 40 moves in 15 minutes (or 2 minutes) with UHO (unbalanced human openings) book.
3. Opponents: 10 calibrated reference engines around 2500-2800 Elo (e.g., Arasan, Laser, Scorpio, Senpai).
4. Compute BayesElo or Ordo rating against the anchor pool.

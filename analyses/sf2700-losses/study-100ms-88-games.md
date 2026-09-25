# Why the champion loses to Stockfish 2700 at 100 ms a move: 88 games

**Outcome.** 39 of the 88 critical moves (44.3%) are tactics that our own depth-14 search finds. For 23 of those 39, a fresh depth-10 search already avoids the move, yet replays at 100 ms only reach depth 5 to 8. 21 game moves cannot be reproduced by any fresh fixed-depth search, so the first fix is to log what the game search actually did. The clearest evaluation defect is in the network's input. I checked it in code today: the input cannot tell which wing the king is on, and it does not see a king step from e1 to e2.

Scope: 88 games (73 losses, 15 draws), one critical move per game. "d10" and "d14" mean fresh fixed-depth searches. "d13 after" means a depth-13 search of the position after the move. Stockfish (SF) scores are from our side's point of view, as in the findings.

## 1. Causes

| cause | games | share | fresh d10 avoids our move | fresh d14 avoids our move | d14 plays SF's move |
|---|---|---|---|---|---|
| tactic_found_deeper | 39 | 44.3% | 32 | 39 | 24 |
| king_safety | 12 | 13.6% | 10 | 10 | 5 |
| eval_positional | 7 | 8.0% | 5 | 4 | 3 |
| tactic_missed_even_deep | 7 | 8.0% | 4 | 5 | 1 |
| passed_pawn_or_pawn_race | 6 | 6.8% | 5 | 6 | 4 |
| draw_misjudged_as_win | 6 | 6.8% | 4 | 3 | 2 |
| won_position_not_converted | 4 | 4.5% | 4 | 4 | 2 |
| other (not reproducible) | 4 | 4.5% | 4 | 4 | 3 |
| bad_start_position | 2 | 2.3% | 2 | 2 | 1 |
| endgame_technique | 1 | 1.1% | 1 | 1 | 1 |
| **total** | **88** | **100%** | **71** | **78** | **46** |

"Avoids our move" does not mean the move it plays instead is good. For example, in 21, 26, 82 and 92 the depth-10 alternative falls to the same idea.

Patterns that cut across causes:
- **21 game moves (23.9%) are never played by a fresh fixed-depth search** at any depth tried: 0, 4, 8, 10, 14, 31, 33, 38, 44, 45, 46, 52, 60, 66, 69, 81, 88, 93, 95, 96, 99. Replays that carry the game history reproduce only 0 (depth 6 at movetime 50) and 10 (depth 8 at 50/100 ms). Game 12's move appears only in a history replay (depth 6).
- **4 moves are chosen only by a depth 1 to 3 search:** 2 (d3), 16 (d1), 73 (d1-2; its in-game score of -0.18 matches no fixed depth) and 80 (d1-2).
- **At 100 ms the game search reaches depth 5 to 8** in replays (games 0, 7, 10, 17).
- **Only 7 game moves are confirmed by both d10 and d14:** 3, 18, 48, 54, 58, 71, 98. d14 alone plays our move in 10 games: those 7 plus 13, 22 and 65.
- **The endgame family** (passed pawn, draw misjudged, won not converted, endgame technique) covers 17 games (19.3%).

## 2. The top causes

### 2.1 tactic_found_deeper (39 games)

**Phase.** Queens are still on the board in 34 of the 39. The 5 queenless ones are 16, 62, 68, 83 and 97.

**Themes** (my grouping of the explanations):
- **Material left loose to a 3 to 5 ply forcing line (19):** 2, 10, 12, 15, 26, 34, 39, 55, 62, 66, 68, 70, 74, 83, 89, 95, 96, 97, 99. Typical cases: an unguarded piece (68 Bc3, 96 Bd7), a pawn dropped (12, 15, 74), a fork (26, 95) or a discovered attack (99).
- **A pawn grab or excursion that loses tempo against our queen or traps a piece (11):** 7, 21, 40, 47, 49, 57, 63, 67, 69, 73, 92.
- **The move opens our own king (8):** 16, 28, 41, 42, 51, 77, 82, 87.
- **A missed win (1):** 86.

**Depth profile.**
- **A, 23 games:** our move is picked only at fresh depth 9 or less, or only in a history replay, and d10 avoids it. Games 2, 7, 10, 12, 15, 16, 34, 39, 40, 41, 42, 47, 55, 62, 63, 67, 68, 73, 74, 77, 83, 87, 97.
- **B, 11 games:** d10 still plays our move or one refuted the same way; depth 11 to 14 fixes it. Games 21, 26, 28, 49, 51, 57, 70, 82, 86, 89, 92.
- **C, 5 games:** no fixed depth reproduces the move. Games 66, 69, 95, 96, 99.

**Examples.**
- **Game 40 (group A).** FEN `r1b1k2r/1p3p1p/pqnp1bp1/8/4PB2/2NQ4/PPP2P1P/2KR1BR1 b kq - 5 7`. We played b6f2 (Qxf2); SF plays c6b4 (Nb4). SF goes from +0.97 to -4.38: 8.Nd5 threatens the Nc7+ fork and hits Bf6. We play Qxf2 at d1-6 and Nb4 from d7 on (d10 +0.04, d14 +0.50). d13 after Qxf2: -0.90.
- **Game 28 (group B).** FEN `r2qkb1r/ppp2pp1/2n1p1p1/1B6/4n3/1P4P1/PBPP3P/RN1QK2R w KQkq - 0 7`. We played e1g1 (O-O); SF plays d1f3 (Qf3). SF goes from 0.00 to -3.30: ...Qd6 hits g3 together with Ne4 against a king with no e- or f-pawn cover. d10 plays O-O itself (-1.01). d14 plays Qf3 (-0.68) and scores O-O at -1.41.
- **Game 66 (group C).** FEN `5Bk1/1b3q1p/6p1/4p3/2r5/4Q3/PP3P1P/R3K1R1 w Q - 3 21`. We played e3b6 (Qb6); SF plays f8h6 (Bh6). SF goes from +6.64 to -6.11 after ...Re4+ Qe3 Rxe3+ fxe3 Qxf8. We scored Qb6 at +2.40 in the game. A fresh search never plays Qb6 from d5 to d14 (Qa7 at d5, Rc1 at d6-8, Bh6 from d9 at +5.9). d13 after Qb6: -3.63.

### 2.2 king_safety (12 games)

**Where our king was.**
- In the centre, or displaced from it (4): 3 Ke1, 6 Kf8, 27 Ke8, 38 Ke2.
- Castled short (7): 0, 11, 43, 52, 81, 84, 85 (kings on g8, h7, h6 or g1).
- Castled long (1): 22 Kb2.

**How SF wins.** Mostly with a quiet plan, not material. The findings say so explicitly for 0 ("a plan, not a short tactic"), 22 ("wins no material"), 27 ("no material in 8 plies") and 38 (no material "anywhere in its 12-ply line").

**Depth does not remove the misjudgement.**
- d14 plays SF's move in 5 games (0, 6, 38, 52, 84), but the score gap remains. In 6 it scores +0.61 after Qa5 where SF says -3.36; in 38, -1.09 against -4.77; in 52, +0.14 against -3.37; in 84, -2.11 against -7.31.
- d14 plays our own move in 2 games (3, 22).
- d14 plays another move SF rates badly in 5: 11 Bg5 (SF -2.35), 27 Rxh4 (-2.85), 43 gxf4 (-3.91), 81 Rb8 (-9.31), 85 Bb2 (-3.85).

**The link to the network input, checked in code.** The champion net has 8 king buckets (`"buckets": 8` in `champion_net.json`), 64 hidden units and `hand_blend 0`. So the hand-written `kingsafety` term never runs, and the net is the only king-safety knowledge the champion has. In `/Users/yann.rolland/chess-go/engine/halfkp.go`:
- `kingBucket` folds the king's file into 4 pairs (a/h, b/g, c/f, d/e) and splits ranks into two halves (ranks 1-4 and 5-8, seen from each side). With 4 file pairs and 2 rank halves, that gives 8 buckets.
- `halfKPIndex` flips piece squares vertically for Black but never mirrors them horizontally.
- Kings are not piece inputs, and castling rights are not inputs at all.

I ran a temporary test today (deleted after running) comparing the two sides' input sets with `AppendHalfKPFeaturesN(..., 8)`:
- Game 38 before and after e1e2: `identical=true`. The move Ke2 changes no network input at all.
- `6k1/8/8/8/8/8/5PPP/6K1` against `6k1/8/8/8/8/8/5PPP/1K6`: `identical=true`. A king behind its f2/g2/h2 shield looks the same as a king on b1, five files away from it.
- `.../6K1` against `.../7K` (control): `identical=false`.

The 32-square scheme in the same file (`kingCanonicalSquare`) folds the king's file in the same way and does not mirror pieces either. So the earlier 32-square trial (idea 7 in `PLATEAU_CAMPAIGN.md`) kept this blind spot.

**Examples.**
- **Game 38.** FEN `rnbq1rk1/4ppbp/p2p2p1/1p1P4/3N4/2N5/PPP2PPP/R1BQK2R w Q - 1 5`. We played e1e2; SF plays c1e3. SF goes from -1.23 to -4.77. d10 and d14 both play Be3, and d13 after Ke2 gives -1.09. Ke2 was not reproduced at any fixed depth (only d3 deviates, playing a3).
- **Game 3.** FEN `1r2k2r/p2n1pp1/2Q1p3/3p1qPp/3P4/2N2bRP/PP3P2/R3KB2 w Qk - 1 13`. We played f1b5; SF plays f1e2. SF goes from -0.77 to -2.02: after ...O-O!, h4 traps Rg3 and our king stays stuck on e1 under Qf3. d10 (+0.30) and d14 (-0.73) both play Bb5. d13 after it: -0.79.
- **Game 84.** FEN `rb2k3/1p2q1p1/2p1p1n1/p1P3p1/3PPnQr/1P2B2P/P2N1PB1/R4RK1 w q - 5 17`. We played g4f3; SF plays g4d1. SF goes from -2.45 to -7.31 after ...g4 hxg4 Qg5 ... Qh3 against Kg1. We play Qf3 at d5-9 and d12, and Qd1 at d10, 11, 13 and 14. At d14 we score the root -0.49 and Qf3 -2.11. In the game we scored +1.21.

### 2.3 Tied third: eval_positional (7) and tactic_missed_even_deep (7)

**eval_positional** (games 13, 18, 23, 37, 64, 71, 93). None of these has a forcing refutation.
- **Piece for pawns.** In 13 we give a knight for two pawns: d13 after says -1.06, SF -5.77. In 71 we give a knight for two pawns plus Rxb2: we say about -1.7, SF -5.1. Game 48 from the next group is the same pattern with a bishop: -0.97 against -2.59.
- **A trapped piece counted as healthy.** In 64 we score the trapped Ba7 at 0.00, SF -3.10.
- **Structural concessions:**
  - 18 Bxf4 gives up the dark bishop and the e5 square: -0.01 against -2.16.
  - 23 lets Black keep the d4 wedge: our scores for all candidate moves are about equal.
  - 37 gives up the c2-c4 break: +0.20 against -1.91.
  - 93 accepts doubled c-pawns and gives up the d-file: -2.60 against -6.6.

d14 plays our move in 13, 18 and 71. In 64, d14 plays Rb2, which SF rates -2.63.

Examples:
- **Game 13.** FEN `r1bqrbk1/p2p1ppp/1pp5/1P1Np1Pn/2N1P3/3PBP2/P1PQ3P/R4RK1 w - - 0 15`. We played d5b6; SF plays b5c6. SF goes from -2.45 to -5.77. d10 plays the other sacrifice, Nc4xb6 (SF -4.30); d14 plays our move.
- **Game 71.** FEN `r1r5/p2k1ppp/4p3/1p1nPn2/8/1P2PN2/1P1BK1PP/R2R4 b - - 0 9`. We played c8c2; SF plays d5b6. SF goes from -2.2 to -5.49. Both d10 and d14 play Rc2 and do see the 10.e4 fork (+2.10 and +1.74 for White).

**tactic_missed_even_deep** (games 25, 29, 33, 44, 48, 58, 78). Each refutation turns on a quiet move or a long run of checks:
- 25: Rg5 pins our queen.
- 29: Nb7 hits Rd8, then b3 shuts in our bishop.
- 44: ...h3 threatens mate after ...Qe4+.
- 48: b3 traps the pawn-grabbing bishop. We see it but misvalue it.
- 58: quiet Rad1 and Rfe1.
- 78: Na4 and Bb2 trap the queen. The finding says this "points to the quiet rim retreat being reduced or pruned".
- 33: a 7-ply run of checks.

Examples:
- **Game 25.** FEN `3rk3/5p1p/pq3Pnb/1p1p4/4pr2/2PBN1Q1/PP3P1P/4RRK1 w - - 0 10`. We played e3d5; SF plays d3e2. SF goes from -1.88 to -3.94. d10 plays Nxd5 at +1.11. d14 plays c4 (-0.25), which SF rates -4.88 because the same combination follows.
- **Game 29.** FEN `rk1r4/p1p2ppp/B4n2/2N5/1p1nPP2/8/bP1N2PP/2R1K2R b K - 1 15`. We played d4e6; SF plays c7c6. SF goes from -0.08 to -2.26. We score Ne6 at +0.42 (d10) and +0.31 (d14), and prefer Nd7 at +0.81/+0.78, which SF rates -1.68.

## 3. Fix proposals, ranked

Rule applied throughout: Stockfish never provides training labels, it only judges. Labels come from our own search, our own self-play results, or tablebases.

Where the problem sits, over the 80 regression positions in section 4:

| side | test | positions |
|---|---|---|
| search | fresh d14 plays SF's move or an also-OK move | 52 |
| eval | fresh d14 still plays our move | 10 |
| both | d14 avoids our move but picks another move SF rates badly, or SF did not score it | 18 |

Across all 88 games, d14 plays SF's move in 46.

### Fix 1 (harness and search): find out what the 100 ms search really did

**Evidence.**
- The 21 unreproduced moves (listed in section 1) and the 4 moves chosen at depth 1 to 3 (2, 16, 73, 80).
- Replays at 100 ms reach depth 5 to 8.
- `uci-bin` was rebuilt at 22:01, after the games were logged in `sf100_study.jsonl` at 21:54 (game 14 note; the file date confirms 22:01). So every reproduction in this study may have used a different binary from the one that played.

**Change.** For each move, log the completed depth, node count, score, whether the move came from an aborted iteration, and the binary's hash. `lastSearchDepth` and `lastSearchNodes` already exist in `/Users/yann.rolland/chess-go/engine/search2.go`. Then act on what the log shows:
- **(a) Starved CPU.** The Stockfish instrument in `NEXT_STEPS.md` runs 5 games in parallel. If that is the cause, run fewer games or pin threads.
- **(b) Aborted iterations.** `searchIterative` starts a new iteration however little time is left, and plays a root move from the cut-off iteration if that move "completes and beats the standing choice". If that is the cause, screen a version that plays only completed iterations.
- **(c) State carried across moves.** Games 0, 10 and 12 reproduce only with the game history. There is nothing to fix in the engine, but future analyses should replay with history by default.

**Mechanism.** Until we know the real depth in the game, we cannot separate an engine weakness from a starved or cut-off search.

**Games it could save.** At most 25: the 21 unreproduced moves plus the 4 shallow picks. That covers only the share that turns out to be the harness rather than the engine.

**10-minute test.** Run all 88 FENs at movetime 100, three times with 5 positions in parallel as in the study, then three times one at a time, logging depth, nodes, move and the aborted flag.
- Starvation is confirmed if the game moves come back only under parallel load, or at completed depth 1 to 3.
- (b) is confirmed if they come back only on moves taken from aborted iterations.
- Both are refuted if depth stays at 5 to 8 and none of the 21 moves come back.

### Fix 2 (evaluation input, then retraining): make the network's input relative to the king's wing

**Evidence.** The 12 king_safety games, and the verified input facts in section 2.2 (Ke2 is invisible to the network; g1 and b1 share every weight). 7 of the 12 are not fixed by d14: 3, 11, 22, 27, 43, 81, 85. King exposure is also named as the eval gap in 28, 42, 77, 79, 87 and 96.

**Change,** in `halfKPIndex` in `/Users/yann.rolland/chess-go/engine/halfkp.go`:
- When the side's king is on files e to h, mirror the piece squares horizontally as well, so shield pawns and attackers are coded relative to the king's wing.
- Split the rank grouping into back rank, 2nd rank and the rest, so Ke1 and Ke2 differ.
- Optionally add 4 castling-right inputs.

Generate the pool with `-king-buckets 64` (commit `d9512d1`), so the new scheme can be derived from the same positions and our own champion labels. Then retrain, warm-starting from the champion's weights for kings on files a to d.

**Mechanism.** Today the network cannot express "pawns in front of my king" or "enemy queen next to my king". The search only notices king danger once it turns into material inside its horizon, and in 0, 22, 27 and 38 it does not.

**Games it could save.** Up to 12. The likeliest are the 7 that d14 does not fix.

**10-minute test.**
- **(a) Unit tests** (red today): game 38 before and after e1e2 must give different inputs; `.../5PPP/6K1` and `.../5PPP/1K6` must give different inputs; Kg1 behind f2/g2/h2 and Kb1 behind a2/b2/c2 must give identical inputs.
- **(b) Probe:** after the smallest retrain that fits the budget, run a d10 probe on the 12 king_safety FENs and count moves that change and score gaps that shrink.
- **(c) Adoption:** the adoption decision is the 100 ms screen against the champion, not the probe.

### Fix 3 (search): more depth inside 100 ms, only after a time-doubling probe

**Evidence.** Group A of tactic_found_deeper (23 games). Also 6, 9, 17, 37, 50, 84 and 94: our move is a shallow-depth pick there, and d10 plays SF's move or one SF rates as holding. Overall, d10 plays SF's move in 32 of 88 and d14 in 46.

**Change.** If Fix 1 shows the budget is not starved, buy depth with the queued search tuning (`LMPBase`, `LMRDiv`, `NullBonusPer` in `NEXT_STEPS.md`) and NPS work measured on `BenchmarkChampionSearch`.

**Mechanism.** Extra plies at the root turn group A picks into depth-10 choices.

**Games it could save.** Up to 30.

**10-minute test.** Run those 30 FENs at movetime 100, 200 and 400, three times each, and count positions where the game move disappears. A doubled budget is the upper bound of what a 2x faster search buys. If 200 ms fixes few of them, drop this fix below Fix 4. `LEARNINGS.md` ("The gap to Stockfish narrows with the clock") already records that earlier speed-ups barely moved the Stockfish result.

### Fix 4 (training data): label endgames with the result of our own self-play playouts

**Evidence.** The 17 endgame-family games. Our score is wrong even at d13 or d14:
- 9: -1.30 where SF says -4.81.
- 19: +0.27 against +3.91.
- 46: -1.58 against -4.73.
- 53: +0.06 against -4.69.
- 24: we see the two moves 0.5 apart; SF sees 3.5.
- 54: +2.00 against +0.47.
- 61: +2.76 against 0.00.
- 65: +5.5 to +6.4 in a fortress SF scores 0.00.
- 98: we prefer the drawn rook ending (+1.34 against +1.10).

Depth fixes most of the passed-pawn cases (d14 plays SF's move in 9, 19, 45 and 46; in 53 it plays Rf8+, which also holds). It does not fix the draws: in 54, 65 and 98, d14 still plays our move.

**Change.** Take low-material positions from our own games and play each one out, champion against champion. Label only those positions with the playout result. `LEARNINGS.md` lists this as the untried form of outcome learning: no per-game constant blended into every position.

**Mechanism.** Depth-4 labels share the network's material-counting view. A playout that ends in a draw carries the drawing signal that a static label misses.

**Games it could save.** Up to 17. The likeliest are 24, 54, 61, 65, 80 and 98.

**10-minute test, before any training.** For games 17, 19, 24, 46, 53, 54, 61, 65 and 98, play out the position after our move and the position after SF's move. Use champion against champion at 100 ms, both colours, a few games each. The fix can only work if self-play separates the two, that is, if SF's position is won more often. If our engine converts both or draws both against itself, drop this fix.

### Fix 5 (search): stop pruning quiet moves that attack a higher-value piece

**Evidence.** The refutations in 25 (Rg5 pinning our queen), 29 (Nb7 hitting a rook), 58 (Rad1 with tempo on the queen) and 78 (Na4 and Bb2 trapping the queen), plus the finding's own suspicion of pruning in 78.

**Code.** In the move loop in `/Users/yann.rolland/chess-go/engine/search2.go`, late-move pruning (LMP), forward futility and late-move reductions (LMR) exempt captures, checks, promotions and advanced pawns (and killer moves, for LMP). Nothing exempts a quiet move whose piece attacks an enemy rook or queen.

**Change.** After making the move, test whether the moved piece attacks a higher-value enemy piece. If it does, skip LMP and futility for it, and reduce it one ply less.

**Games it could save.** Up to 4: 25, 29, 58 and 78.

**10-minute test.** Apply our move to the 7 tactic_missed_even_deep FENs and search at d14, with and without the change. Check whether the side to move now finds SF's refutation (25 ...Rxd5 with Rg5 in the line, 29 Nb7, 44 ...Qe4+, 33 Bxd7+, 48 b3, 58 Bxd4, 78 Na4), and log node counts. Then run the 100 ms screen, because the extra nodes cost depth elsewhere.

### Fix 6 (training data): piece-for-pawns imbalances and trapped pieces

**Evidence.** Games 13, 71 and 48 (piece for two pawns), 64 (trapped bishop), and 18, 23, 37 and 93 (structure). d14 plays our move in 13, 18, 48 and 71, so deeper labels from our own search would not help.

**Change.** The same playout labelling as Fix 4, applied to positions from our own games with a minor piece against pawns. Confidence is lowest here, because the game result is many moves away.

**Games it could save.** Up to 8.

**10-minute test.** The Fix 4 playout check on the positions after our move and after SF's move in 13, 48, 64 and 71.

**Not proposed:** singular extensions, quiet checks in quiescence, more self-play data at the same label depth, and depth-8 labels at 200k. `NEXT_STEPS.md` lists all four as flat, and this study gives no new reason to retry them.

## 4. Regression positions

Selection: 80 positions with an SF score before the move above -3 (the 7 positions that were already lost are left out), minus game 96, where SF's own scan and its multipv disagree on the best move.

How to test: at movetime 100 in study conditions, and at depth 10, the engine must not play the "played" move. The target is "expected" or an "also OK" move. "Also OK" means SF rates it within 0.3 pawn of its best move, or, in game 60, still clearly winning.

Tiers:
- **S:** fresh d14 already plays expected or also-OK. These are search regressions.
- **E:** d14 still plays our move. These are evaluation regressions.
- **D:** d14 avoids our move but plays something SF rates badly or did not score.

Cause abbreviations: tfd = tactic_found_deeper, king = king_safety, eval = eval_positional, missed = tactic_missed_even_deep, passed = passed_pawn_or_pawn_race, draw = draw_misjudged_as_win, won = won_position_not_converted, end = endgame_technique, start = bad_start_position.

| game | tier | cause | FEN | played | expected | also OK |
|---|---|---|---|---|---|---|
| 0 | S | king | `2rq1rk1/4bpp1/2bp1n1p/pp1NpP2/4P3/3B1QP1/PPPN2RP/1K1R4 b - - 5 3` | g7g6 | c6d5 | |
| 2 | S | tfd | `3n4/1p5k/p5p1/N3Q3/7p/4b3/PP4PK/5q2 w - - 2 35` | e5e7 | e5e3 | |
| 3 | E | king | `1r2k2r/p2n1pp1/2Q1p3/3p1qPp/3P4/2N2bRP/PP3P2/R3KB2 w Qk - 1 13` | f1b5 | f1e2 | |
| 4 | S | other | `r2q1rk1/ppp2ppp/3bbn2/1Qp5/4P3/2N2N1P/PPP2PP1/R1B2RK1 b - - 3 7` | c7c6 | a7a6 | |
| 6 | S | king | `r2q1k1r/pp1n1p2/2p5/3pPNp1/1b1Pn3/2N1P2P/PP4P1/R2QKB1R b KQ - 2 9` | d8a5 | e4c3 | |
| 7 | S | tfd | `2r1kbnr/5ppp/p1q1p3/1p1p4/3P1B2/P1N1Q3/1PP2PPP/2KR3R w k - 0 9` | e3g3 | d1d3 | f4e5 |
| 8 | S | other | `5rk1/1pprq1p1/p3np1p/2p1p2P/4P1Q1/1N1P2P1/RPP2P2/R5K1 w - - 0 23` | f2f4 | b3d2 | g1g2, b3a5 |
| 9 | S | passed | `6k1/6p1/8/5p1p/2P5/7P/5rPK/2R5 b - - 1 25` | f5f4 | g8f7 | f2a2 |
| 10 | D | tfd | `r1bq1rk1/pp2bpp1/5n2/3p2P1/1n6/2N1PN2/PP2BPP1/2QRK2R b K - 0 7` | f6e4 | f6h7 | |
| 11 | D | king | `r1b2rk1/pp3pp1/2pq3p/P3pP2/1n1pP3/1B1PbQN1/1PP3PP/RN3R1K b - - 3 12` | d6c5 | d6d8 | |
| 13 | E | eval | `r1bqrbk1/p2p1ppp/1pp5/1P1Np1Pn/2N1P3/3PBP2/P1PQ3P/R4RK1 w - - 0 15` | d5b6 | b5c6 | |
| 14 | S | other | `2r5/pp1b1pk1/5p2/1P1P2p1/P2Q2P1/q6p/3P1P1P/4R1KB w - - 4 27` | d4a1 | g1f1 | |
| 15 | D | tfd | `2kr3r/1pp2pp1/1bn1q3/4p2p/Q5P1/4PB1P/1BPP1R2/R5K1 b - - 0 18` | e5e4 | c6a7 | |
| 16 | S | tfd | `6k1/6p1/6Np/ppppR2P/b5P1/2r2PK1/8/8 b - - 1 20` | a4b3 | g8f7 | |
| 17 | S | draw | `Q1b5/1p4pR/6k1/2p5/P1P3q1/1P2K3/7P/5R2 w - - 2 28` | h7h8 | h7h4 | |
| 18 | E | eval | `1rbq1rk1/4ppbp/1pn3p1/p1p5/2P1Pp2/2NPB1P1/PP4BP/R2Q1RK1 w - - 0 9` | e3f4 | g3f4 | |
| 19 | S | passed | `4r3/pp1B1p1p/5Q2/2kp4/2p2PK1/7R/6PP/4q3 b - - 6 37` | e8g8 | e1e7 | |
| 21 | S | tfd | `rnbqrbk1/2p2ppp/p2p1n2/1p6/2BNPB2/2N2P2/PPP3PP/R2Q1RK1 w - - 0 3` | c4b3 | c4d5 | |
| 22 | E | king | `r3q1k1/3b2p1/p2p4/n1pPp1Bp/p3P2P/1PNQ4/1KP3P1/5R2 w - - 0 22` | f1a1 | b3a4 | |
| 23 | S | eval | `4k2r/1p2bppp/p3p3/3rNn2/3p3q/1P3Q2/PBP2PPP/R3R1K1 w k - 4 17` | e1e4 | g2g3 | a1d1 |
| 24 | S | draw | `8/8/4r1kp/4Rp1p/5P2/3pK1PB/2b5/8 b - - 7 43` | g6f6 | e6e5 | |
| 25 | D | missed | `3rk3/5p1p/pq3Pnb/1p1p4/4pr2/2PBN1Q1/PP3P1P/4RRK1 w - - 0 10` | e3d5 | d3e2 | |
| 26 | S | tfd | `2b4k/p5pp/2p5/2prp1q1/PpN2r2/1P1PQP2/2P1R1P1/3R2K1 b - - 13 20` | c8b7 | g5h4 | |
| 27 | D | king | `2r1kb2/5p1p/p1np1q2/1p1Q1P2/4pr1P/2P1N3/PP2BP2/3RKR2 b - - 2 8` | f8e7 | f8g7 | |
| 28 | S | tfd | `r2qkb1r/ppp2pp1/2n1p1p1/1B6/4n3/1P4P1/PBPP3P/RN1QK2R w KQkq - 0 7` | e1g1 | d1f3 | |
| 29 | D | missed | `rk1r4/p1p2ppp/B4n2/2N5/1p1nPP2/8/bP1N2PP/2R1K2R b K - 1 15` | d4e6 | c7c6 | |
| 31 | S | end | `7k/6p1/7p/8/6PP/3p1K2/5P2/8 b - - 0 77` | h8h7 | h6h5 | |
| 33 | S | missed | `1r2kb1r/N2b1ppp/4p3/2pn4/B4P2/2PP2P1/PP2KNqn/R1BQ3R b k - 0 14` | b8a8 | h2g4 | g2g3, g2f3 |
| 34 | S | tfd | `r4rk1/pp2qpbp/4bnp1/2p1p1B1/2Pn4/2N1P1P1/PP1N1PBP/R2Q1RK1 b - - 0 7` | d4c6 | d4f5 | |
| 36 | D | won | `r4rk1/p3bp1p/2pqbp2/3p4/3N4/1PN5/P1PQ1PPP/R3R1K1 w - - 1 12` | d2e3 | c3e2 | |
| 37 | S | eval | `1r2k2r/3nqppp/2Qb4/1p6/3P4/6PP/PBP1PP2/RR4K1 w k - 1 15` | e2e4 | c2c4 | |
| 38 | S | king | `rnbq1rk1/4ppbp/p2p2p1/1p1P4/3N4/2N5/PPP2PPP/R1BQK2R w Q - 1 5` | e1e2 | c1e3 | |
| 39 | S | tfd | `r2qk2r/ppp2ppp/2nbpn2/3p1b2/2P5/1P1P1NP1/PB2PPBP/RN1QK2R b KQkq - 0 3` | e6e5 | h7h6 | e8g8 |
| 40 | S | tfd | `r1b1k2r/1p3p1p/pqnp1bp1/8/4PB2/2NQ4/PPP2P1P/2KR1BR1 b kq - 5 7` | b6f2 | c6b4 | |
| 41 | D | tfd | `r3kb1r/1p2qppp/p7/3pn3/1P1pnQ2/P7/1BP1NPPP/2KR1B1R b kq - 4 11` | g7g5 | e7c7 | |
| 42 | S | tfd | `r1bqkb1r/p1p2ppp/2p2n2/3p4/4P3/2NB4/PPP2PPP/R1BQK2R b KQkq - 0 1` | d8e7 | f8b4 | |
| 43 | D | king | `r4r2/pb2b1k1/1p2p3/3pPppB/2p2P1p/1qP1P2P/1P2Q1PB/3R1R1K b - - 2 4` | g7h7 | b7c6 | |
| 44 | S | missed | `2r2r2/6k1/4p1q1/Q1b1P3/6Pp/2P1P3/P4P1B/3RR2K w - - 1 12` | h2f4 | a5a4 | |
| 45 | S | passed | `6k1/p1r3p1/3N3p/4PP2/8/1r6/6PP/R5K1 w - - 0 25` | a1d1 | e5e6 | |
| 46 | S | passed | `8/1k5P/1p6/8/P1P2K2/8/1b6/8 w - - 3 46` | c4c5 | f4f5 | |
| 47 | S | tfd | `r1b1kb1r/pp3ppp/2n1p3/3nP3/2N1Q3/5N2/qP1B1PPP/2R1KB1R b Kkq - 3 4` | c6b4 | c8d7 | |
| 48 | E | missed | `r4rk1/ppp2ppp/2n1b3/8/5N2/5PP1/PPP4P/2K1RB1R b - - 0 12` | e6a2 | e6f5 | |
| 49 | S | tfd | `r1b2rk1/p4pb1/q3p1pp/2p1N3/2NP2Pn/4R3/PP3P1P/R1BQ2K1 w - - 0 8` | e3h3 | d4c5 | |
| 50 | S | won | `r2r2k1/BQp2p1p/3q2p1/4b3/8/2P5/PP3PPP/1R2R1K1 w - - 3 16` | h2h3 | g2g3 | |
| 51 | D | tfd | `6rr/pb4k1/1p2p3/3pPpbB/2pRP3/1qP3BP/1P2Q2K/4R3 b - - 0 8` | f5e4 | g5e3 | |
| 52 | S | king | `r2q1rk1/pp3pbp/2np4/2p1p3/2P5/1PNPPb2/PB1QNP1R/2KR4 b - - 1 10` | a8c8 | f7f5 | |
| 53 | S | passed | `7r/1k6/5K2/7P/6P1/8/8/8 b - - 7 65` | b7a7 | b7c6 | h8f8 |
| 54 | E | draw | `8/5R2/4P1k1/3Pr2p/6p1/7P/5K2/8 w - - 2 46` | h3g4 | f7d7 | |
| 55 | S | tfd | `r1bqk2r/1pp2pb1/p1n2n1p/3pp3/1P2P1p1/NBPP4/P2N1PPP/R1BQR1K1 b kq - 1 4` | g4g3 | e8g8 | c8e6 |
| 57 | D | tfd | `rn1qk2r/pp4pb/2p1B3/3pN1pp/1b1Pn2P/1P6/PBP1PP2/RN1Q1RK1 w kq - 2 10` | e2e3 | f2f3 | |
| 58 | E | missed | `2rq1k1r/ppp1bpp1/2n2n2/7p/2NP3P/PQ3BP1/1B3P2/R4RK1 b - - 0 13` | c6d4 | f6d5 | |
| 60 | S | won | `8/1p6/7P/6P1/pN1k4/P7/7K/1b6 w - - 0 42` | b4a2 | h2g3 | h2h3 |
| 61 | D | draw | `4k3/1R6/5p2/8/7p/5pr1/8/5K2 b - - 3 53` | f6f5 | g3g4 | |
| 62 | S | tfd | `2r2b1r/1p1nkpp1/1p2p1p1/1BppB2n/3P4/2P1P2P/PP1N1PP1/R4RK1 b - - 7 11` | f7f5 | h5f6 | |
| 63 | S | tfd | `rnbqkb1r/1p3p1p/p2p1np1/4p1P1/3NP3/2N5/PPP2P1P/R1BQKBR1 b Qkq - 0 2` | f6g4 | e5d4 | |
| 64 | D | eval | `r7/Bk1nb1pp/1p3p2/1N2p3/2rPP3/2P3PP/R5PK/1R6 w - - 3 23` | b1a1 | g3g4 | |
| 65 | E | draw | `5k2/1R6/5pp1/8/6KP/1p6/5P2/1r6 b - - 0 40` | b3b2 | f8e8 | |
| 66 | S | tfd | `5Bk1/1b3q1p/6p1/4p3/2r5/4Q3/PP3P1P/R3K1R1 w Q - 3 21` | e3b6 | f8h6 | |
| 67 | S | tfd | `2b1r1k1/p1p1qpp1/2P2n2/2b4p/1r6/2N3B1/PPP1BPPP/1R1Q1K1R w - - 0 9` | e2h5 | h2h4 | |
| 68 | D | tfd | `r4bk1/5np1/1R3p2/1P1P4/5PN1/prB3P1/7P/3R3K w - - 3 40` | b6a6 | c3a1 | |
| 69 | S | tfd | `r4rk1/2p2ppp/p1nbp2n/1p3q2/3P1P2/PP1pPN2/Q2B2PP/R2NR1K1 b - - 2 16` | h6g4 | b5b4 | |
| 70 | D | tfd | `r5k1/1pqb1pp1/p1pp1n1p/4r3/2PNP3/P2QPB1P/2P3P1/1R3RK1 w - - 1 17` | b1d1 | d3b3 | |
| 71 | E | eval | `r1r5/p2k1ppp/4p3/1p1nPn2/8/1P2PN2/1P1BK1PP/R2R4 b - - 0 9` | c8c2 | d5b6 | |
| 73 | S | tfd | `1rb2rk1/4ppbp/2p3p1/q1p3P1/N3P3/1B4R1/PPP1QP1P/R3K3 w Q - 1 11` | a4c3 | c2c3 | |
| 74 | S | tfd | `r1bqkb1r/1p3p2/p2p2p1/4p1Pp/4P3/2N5/PPP1BP1n/R1BQK1R1 b Qkq - 1 6` | d6d5 | h2g4 | |
| 75 | S | won | `r1b2rk1/p1p1qppp/2p2n2/4b3/4p3/2N4P/PPPBBPP1/1R1QK2R b K - 3 5` | e5c3 | c8f5 | |
| 80 | D | passed | `3bb1rk/pq4r1/4p3/3pPp2/pPpR3p/2P1P2P/2BQR1PB/7K w - - 2 23` | h1g1 | d2e1 | |
| 82 | D | tfd | `4r1k1/3b1q1p/3R1ppP/2p5/1p2PB1R/5PQ1/rPPK2P1/8 w - - 3 16` | d2d1 | h4h1 | |
| 83 | S | tfd | `3rn1k1/8/4p1K1/p3P3/1pR5/1PpN4/2P1P2P/8 b - - 0 33` | d8a8 | d8d7 | |
| 84 | S | king | `rb2k3/1p2q1p1/2p1p1n1/p1P3p1/3PPnQr/1P2B2P/P2N1PB1/R4RK1 w q - 5 17` | g4f3 | g4d1 | |
| 86 | S | tfd | `r6r/pb2b1k1/1p2p3/3pPppB/q1p2P1p/P1P1P2P/1P2Q1PB/3RR1K1 b - - 0 1` | g5f4 | h8h5 | |
| 88 | S | other | `3r2k1/4rppp/p1R2b2/1p6/4P3/1P2BPQ1/1qP1K1PP/3R4 b - - 6 11` | e7d7 | e7e8 | |
| 89 | S | tfd | `8/RQn1rkpp/3q1p2/2pP4/8/5N1P/5PP1/6K1 w - - 1 27` | b7c6 | a7a5 | b7b1 |
| 92 | S | tfd | `2bb1rk1/Br1q1pp1/3p3p/1p1N4/3Q4/8/PP3PPP/R4RK1 w - - 2 12` | a7b6 | a1c1 | |
| 93 | S | eval | `r1b2rk1/1pq1bp2/1np4p/p3p1p1/P1B1Pn2/5NBP/1PP1NPP1/2RQR1K1 w - - 3 4` | b2b3 | c4b3 | |
| 94 | S | start | `3b3k/p2b2r1/1p2r3/8/q1pRQB1p/P1P4P/1P4P1/4R1K1 w - - 0 12` | d4d7 | f4e5 | |
| 95 | D | tfd | `1r1b1rk1/5pp1/1q1p1n1p/pp1PpP2/8/3BNQP1/PPP4P/1K1R3R w - - 5 5` | h1f1 | d3e4 | |
| 97 | S | tfd | `1n1r1k2/3bppb1/pp5p/n1p3p1/P1P1P1P1/NP2BN1P/5PB1/3R1K2 w - - 6 21` | b3b4 | d1b1 | |
| 98 | E | draw | `6k1/1p3ppp/p7/8/3Rp2P/1P4P1/r4P2/5K2 b - - 0 27` | f7f5 | g8f8 | |
| 99 | S | tfd | `2r2rk1/3nqppb/2pbp2p/8/1pP1n1P1/1P1Q1N1P/1B2NPB1/R2R2K1 w - - 2 14` | d1c1 | e2g3 | d3e3 |

**HEADS UP:** Every fixed-depth result in the findings may come from a different binary than the one that played the games (the 22:01 rebuild). Fix 1's hash logging closes that gap. The theme groups in 2.1 and the "also OK" list are my reading of the findings; all counts come from the findings' per-game fields. Code facts come from `/Users/yann.rolland/chess-go/engine/halfkp.go` and `/Users/yann.rolland/chess-go/engine/search2.go`. The temporary test file is deleted and `git status` for `engine/` is clean.
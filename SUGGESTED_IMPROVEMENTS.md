# Suggested Improvements for chess-go

Based on the documentation, state files, and known bugs, here are 10 actionable improvements for the engine, categorized by Search, Evaluation, Training, and Technical Debt:

## Search and Pruning
1. **Address the Stalemate at Stand-Pat Cutoff Bug:**
   *Issue:* As noted in `STATE.md` and `NEXT_STEPS.md` (`9483013`), when the stand-pat cuts off, a stalemated side ahead on material incorrectly receives its material score rather than 0.
   *Action:* Write a failing test first, then implement a cheap legal-move check (e.g., `HasAnyLegalMove`) at the cutoff point and measure its Elo impact.

2. **Explore Singular Extensions:**
   *Issue:* Listed in `NEXT_STEPS.md` as an available experiment.
   *Action:* Implement singular extensions to allow the search to identify forced (or strongly preferred) moves and search them deeper without spending much time on alternatives, improving tactical vision.

3. **Determine the Optimal Quiescence Cap:**
   *Issue:* `NEXT_STEPS.md` notes that capping qply to 16 yielded positive results.
   *Action:* Experiment with qply caps of 24 and 32 against the 16 baseline to find exactly where the cap stops paying off in Elo.

4. **Implement Quiet Checks in Quiescence:**
   *Issue:* `NEXT_STEPS.md` lists "quiet checks in quiescence" as a next step. 61% of losses are due to evaluation lag (Stockfish seeing it 10+ plies first).
   *Action:* Allow certain highly-promising quiet checks to be generated and searched in quiescence search. This can resolve tactical blunders that occur just over the horizon.

## Evaluation & NNUE
5. **Fix NNUE Jumpy Evaluation (Smoothness):**
   *Issue:* `REVIEW_BRIEF.md` notes the NNUE is accurate but jumpy (0.58 pawns of change per move vs 0.38 for the hand evaluation). This jumpiness strongly limits the network's effectiveness and blending weight.
   *Action:* Experiment with adding regularization (like L2 weight penalty or an output smoothing penalty term) or architectural changes to reduce the jumpiness.

6. **Experiment with Loss Functions (Sigmoid vs MSE):**
   *Issue:* `NEXT_STEPS.md` points out the need to compare Sigmoid (win probability) against squared error on the same 3.2M positions.
   *Action:* Evaluate the results of the loss function experiment. If Sigmoid yields a better Elo return, fine-tune the champion on a larger slice.

7. **Implement and Screen SPSA Tuned Parameters:**
   *Issue:* A 482-round SPSA read +12 +/- 21, but a single parameter moves more Elo per game than twelve at once (`NEXT_STEPS.md`).
   *Action:* Isolate and screen the three largest SPSA recommendations: `LMPBase` (3 to 6.5), `LMRDiv` (2.5 to 2.2), and `NullBonusPer` (1.5 to 1.15). Apply those that show a measured gain.

## Data and Training Pipeline
8. **Rebuild Self-Play Training Volume:**
   *Issue:* `STATE.md` notes the corpus shrank from 24.58M to 13.67M and the duplicate rate is rising, which hinders data-driven gains.
   *Action:* Rebuild data volume by generating more self-play data. Consider varying openings or increasing the play depth slightly to inject more diversity and reduce duplicate positions.

9. **Investigate the Target Blending Weight:**
   *Issue:* `REVIEW_BRIEF.md` highlights the training target `0.8 * search_score + 0.2 * result_pawns`.
   *Action:* Experiment with tweaking this ratio. While distilling from a depth-10 Stockfish failed due to jumpiness, adjusting the weight of the game result vs. the self-search score could improve practical play.

## Technical Debt & Infrastructure
10. **Clean up Harness Debt (Deprecated Flags):**
    *Issue:* `NEXT_STEPS.md` and `STATE.md` point out that `scripts/chunked_match.sh`, `ladder.sh`, `ladder_scratch.sh`, and `screen.sh` pass deprecated `-halfkp` flags that `gauntlet-bin` now correctly refuses.
    *Action:* Update these scripts to use the clean recipe (`jq '.net_file=...' champion.json > cand.json`) and retire any scripts that are no longer necessary.
# Suggested Improvements for chess-go

Based on the documentation, state files, and known bugs, here are 10 actionable improvements for the engine, categorized by Search, Evaluation, Training, and Technical Debt:

## Search and Pruning
1. **Address the Stalemate at Stand-Pat Cutoff Bug:**
   *Issue:* As noted in `STATE.md` and `NEXT_STEPS.md` (`9483013`), when the stand-pat cuts off, a stalemated side ahead on material incorrectly receives its material score rather than 0.
   *Action:* (Implemented in PR) Added a check `!inCheck && !g.HasAnyLegalMoveInCheck(color, false)` right before returning the `standPat` score.

2. **Explore Singular Extensions:**
   *Issue:* Listed in `NEXT_STEPS.md` as an available experiment.
   *Action:* Implement singular extensions to allow the search to identify forced (or strongly preferred) moves and search them deeper without spending much time on alternatives, improving tactical vision.

3. **Determine the Optimal Quiescence Cap:**
   *Issue:* `NEXT_STEPS.md` notes that capping qply to 16 yielded positive results.
   *Action:* Experiment with qply caps of 24 and 32 against the 16 baseline to find exactly where the cap stops paying off in Elo. Qply 24 was bench tested yielding +17 Elo over 400 games (within margin of error).

4. **Implement Quiet Checks in Quiescence:**
   *Issue:* `NEXT_STEPS.md` lists "quiet checks in quiescence" as a next step. 61% of losses are due to evaluation lag.
   *Action:* Allow certain highly-promising quiet checks to be generated and searched in quiescence search.

## Evaluation & NNUE
5. **Fix NNUE Jumpy Evaluation (Smoothness):**
   *Issue:* `REVIEW_BRIEF.md` notes the NNUE is accurate but jumpy.
   *Action:* Experiment with adding regularization (like L2 weight penalty or an output smoothing penalty term) or architectural changes to reduce the jumpiness.

6. **Experiment with Loss Functions (Sigmoid vs MSE):**
   *Issue:* `NEXT_STEPS.md` points out the need to compare Sigmoid (win probability) against squared error.
   *Action:* Evaluate the results of the loss function experiment.

7. **Implement and Screen SPSA Tuned Parameters:**
   *Issue:* A 482-round SPSA read +12 +/- 21, but a single parameter moves more Elo per game than twelve at once.
   *Action:* Isolate and screen the three largest SPSA recommendations: `LMPBase`, `LMRDiv`, and `NullBonusPer`.

## Data and Training Pipeline
8. **Rebuild Self-Play Training Volume:**
   *Issue:* The corpus shrank and duplicate rate is rising.
   *Action:* Rebuild data volume by generating more self-play data. Consider varying openings or increasing the play depth slightly to inject more diversity.

9. **Investigate the Target Blending Weight:**
   *Issue:* The training target is `0.8 * search_score + 0.2 * result_pawns`.
   *Action:* Experiment with tweaking this ratio to improve practical play.

## Technical Debt & Infrastructure
10. **Clean up Harness Debt (Deprecated Flags):**
    *Issue:* Bash scripts pass deprecated `-halfkp` flags that `gauntlet-bin` now correctly refuses.
    *Action:* (Implemented in PR) Updated these scripts to use the clean recipe (`jq '.net_file=...' champion.json > cand.json`).

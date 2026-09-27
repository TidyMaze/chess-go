# Iterative Self-Improvement Pipeline

This document defines the exact, ordered loop that produced verified Elo gains across Rungs 9 through 13.

---

## The Core Loop (Step-by-Step)

```
[1. Baseline Champion]
        |
        v
[2. High-Volume Self-Play Generation] (openings.txt, depth-4 label, champion teacher)
        |
        v
[3. Deduplication & Merge] (poolmerge into cumulative corpus)
        |
        v
[4. PyTorch GPU Training] (MPS, HalfKP h64, early stopping on held-out loss)
        |
        v
[5. Bit-Exact Verification] (Go eval == PyTorch eval)
        |
        v
[6. Chunked SPRT Match] (chunked_match.sh vs reference champion)
        |
        +---> [Failed / Worse] ---> Reject candidate, inspect loss/moveaudit
        |
        v [Passed / Better: SPRT[0,10] settled]
[7. Promotion & Commit] (update champion.json, sync tests, commit to git)
        |
        v
[8. Advance to Next Generation] (new champion becomes the teacher)
```

---

## Detailed Step Specifications

### Step 1: Baseline Champion State
The system always maintains a single source of truth for the strongest engine configuration:
- `champion.json`: Deployed champion descriptor (evaluated with 8 threads at 1s).
- `champion_bot.json`: Lichess bot configuration (evaluated with 4 threads at 1s).
- `champion_ui.json`: Web UI configuration.
- `champion_net.json`: Deployed HalfKP neural network weights.

All three champion files must maintain identical search features (guarded by `TestChampionFilesAgreeOnSearchFeatures`).

---

### Step 2: High-Throughput Self-Play Generation
Generate new positions from self-play using the current champion as the teacher.

**Key Findings:**
- **Start positions:** Use `-opening-book openings.txt` (150,000 diverse master openings). Ten random plies depleted novelty quickly (37% duplicate rate). Real openings maintain >90% novelty.
- **Label Depth:** Use `label-depth 4`. Label depth 4 generates ~1,261 pos/sec (compared to 132 pos/sec at depth 6). Volume dominates label depth because volume averages out noise.
- **Play Depth:** Self-play games generated at depth 2 or 3.

**Command:**
```bash
./nnue-bin \
  -opening-book openings.txt \
  -label-champion champion.json \
  -label-depth 4 \
  -play-depth 3 \
  -pool-file /tmp/chesslogs/gen_d4_v<N>.bin \
  -games 600 \
  -generations 100 \
  -epochs 0
```

---

### Step 3: Deduplication and Corpus Merging
Merge the newly generated pool into the cumulative deduplicated master corpus.

**Key Findings:**
- Exact duplicate positions harm network learning (-19 Elo observed when duplicates retained).
- Duplicate rate is the primary metric of corpus health.

**Command:**
```bash
go run ./cmd/poolmerge \
  -out /tmp/chesslogs/merged_v<N>.bin \
  /tmp/chesslogs/merged_v<N-1>.bin \
  /tmp/chesslogs/gen_d4_v<N>.bin
```
Verify output metadata in `/tmp/chesslogs/merged_v<N>.bin.meta.json` (check positions written vs duplicates dropped).

---

### Step 4: PyTorch GPU Training
Train a HalfKP network on Apple Silicon GPU (MPS).

**Architecture:**
- Inputs: 5,120 (HalfKP: 8 king buckets x 10 piece types x 64 squares).
- Hidden: 64 units per perspective (128 units total entering output layer).
- Activation: Clipped ReLU [0, 1].
- Loss: Mean Squared Error (`--loss mse`).

**Command:**
```bash
.venv/bin/python -u pytorch/train.py \
  --pool /tmp/chesslogs/merged_v<N>.bin \
  --hidden 64 \
  --buckets 8 \
  --batch 16384 \
  --device mps \
  --lr 0.001 \
  --patience 8 \
  --lr-decay 8 \
  --epochs 30 \
  --out /tmp/chesslogs/net_v<N>_h64.json \
  --status nnue_status.json \
  --label "<N>M positions h64"
```
Training automatically restores weights from the epoch with lowest held-out test loss.

---

### Step 5: Bit-Exact Export Verification
Ensure the network exported by PyTorch matches the Go engine's SIMD/accumulator inference bit-for-bit.

**Commands:**
```bash
./nnue-bin -emit-eval-check /tmp/eval_check.json -net-file /tmp/chesslogs/net_v<N>_h64.json
.venv/bin/python pytorch/verify.py --net /tmp/chesslogs/net_v<N>_h64.json --cases /tmp/eval_check.json | grep identically
```
If verification fails, stop immediately: never deploy a network that evaluates differently across implementations.

---

### Step 6: Chunked SPRT Match Against Champion
Race candidate network against current champion in sequential chunks.

**Protocol:**
- Match runner: `scripts/chunked_match.sh`.
- Hypothesis test: Sequential Probability Ratio Test `SPRT[0, 10]` (verifies >= +10 Elo gain).
- Chunk size: 200 games per chunk.
- Stopping criteria: Automatically terminates as soon as log-likelihood ratio (LLR) crosses +2.94 (`better`) or -2.94 (`worse`).

**Command:**
```bash
# Create temporary challenger descriptor
cat <<EOF > candidate_v<N>.json
{
  "label": "candidate v<N>: <N>M positions h64",
  "depth": 1,
  "net_file": "/tmp/chesslogs/net_v<N>_h64.json",
  "hand_blend": 0.0,
  "features": "lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate,iir,improving,razoring,conthist,histlmr",
  "book": "games_book_v5.txt"
}
EOF

# Run chunked SPRT match (10ms time control)
CHESS_SPRT=on CHESS_SPRT_ELO1=10 scripts/chunked_match.sh 1600 200 \
  -champion candidate_v<N>.json \
  -ref-champion champion_bot.json \
  -threads 1 \
  -time-ms 10
```

---

### Step 7: Promotion, Verification & Git Commit
When the SPRT test settles `better`:

1. **Deploy Network Weights:**
   ```bash
   cp /tmp/chesslogs/net_v<N>_h64.json champion_net.json
   cp champion_net.json halfkp_latest.json
   cp champion_net.json halfkp_best.json
   ```

2. **Update Golden Invariant Regression Tests:**
   Run `go test -v ./engine -run TestChampionNetworkStillEvaluatesToTheBit` to obtain exact float outputs, then update `championOutputs` in `engine/halfkp_regression_test.go`.

3. **Run Full Test Suite:**
   ```bash
   go test ./...
   ```
   Must exit with code 0. Never use pipelines ending with `tail` or `grep` when checking test exits.

4. **Update Champion Descriptors:**
   Update `champion.json`, `champion_bot.json`, and `champion_ui.json`:
   - Increment rung label and Elo by measured delta.
   - Update adoption timestamp and `elo_note`.

5. **Rebuild Engine Binaries:**
   ```bash
   go build -o gauntlet-bin ./gauntlet
   go build -o lichessbot-bin ./lichessbot
   go build -o uci-bin ./uci
   ```
   Note: The running Lichess bot monitors `champion.json` via `ChampionWatcher` and loads updated weights automatically without needing a restart.

6. **Document and Commit:**
   Record milestone in `LEARNINGS.md` and make a git commit:
   ```bash
   git add LEARNINGS.md champion.json champion_bot.json champion_net.json champion_ui.json engine/halfkp_regression_test.go
   git commit -m "feat(champion): adopt <N>M net as rung <R> (+<X> Elo)"
   ```

---

### Step 8: Loop Continuation
The adopted champion is now the teacher for generation `v<N+1>`. Return to Step 2 to generate the next pool.

# chess-go

A chess engine written from scratch in Go, built test first, with a browser UI
and a learning loop that trains its evaluation on its own games.

About 24k lines of Go across 22 packages, 90 test files, no engine
dependencies. The neural network is trained in PyTorch and runs in Go.

## The one rule

The engine may only ever learn from scores it computed itself.

Game databases are fair game: the positions in the training pools come from
real Lichess games. Pre-computed evaluations are not, so the Lichess
evaluation dump and Stockfish's opinion of a position never reach a training
target. Every label in every pool is the output of this engine's own search.

Stockfish is used for one thing only, as a ruler: it plays matches so the
engine's strength can be placed on a public scale.

## Strength

2281 Elo, measured against Stockfish with both sides on the same 1 second per
move, 30 games per level across a ladder from 1600 to 3000.

Treat that number as the only comparable one. The same engine reads 2476 on a
ladder where each Stockfish rung plays at a fixed shallow depth instead of a
clock, because a fixed depth puts Stockfish well below its rating label. The
two instruments measure different things and their numbers must not be
compared. Per-rung estimates at 1 second spanned 2147 to 2480, so the
instrument itself carries about 170 points of spread.

## What is in it

Search: alpha-beta with principal variation search, iterative deepening,
aspiration windows, a transposition table, killer moves, history and
countermoves, late move reductions, null move pruning, futility pruning,
static exchange evaluation, check extensions and quiescence.

Evaluation: a HalfKP network (8 king buckets, 64 hidden units, incremental
accumulator) blended 0.45 with a hand written evaluation of material, piece
square tables, pawn structure, mobility and king safety. The blend is not a
compromise anyone settled for: the network on its own plays clearly worse
than the blend, and so does every other weighting that was tried.

Move generation is validated by Perft against the published node counts,
including Kiwipete, which is the only way to know the rules are right.

## Run it

```bash
go build -o play-bin ./play && ./play-bin -port 8765
```

Then open `http://localhost:8765/gui.html` and play the current champion.
`champion.json` names the network, search depth and blend it uses.

```bash
go build -o uci-bin ./uci && ./uci-bin      # speak UCI on stdin/stdout
go run ./cli                                # play in the terminal
```

## How a change is judged

Nothing is adopted on a point estimate. A change is played head to head
against the build before it over paired openings with the colours reversed,
with SPRT early stopping, and it has to beat its own error bar.

Two habits in here were learned the hard way and are worth stealing:

- **Prove the ruler before trusting it.** The champion against itself must
  read zero. A measurement bug that handicapped one side by 19 Elo survived
  for seven training rounds because nobody ran that check at enough games to
  see it.
- **Never adopt on the race that selected the candidate.** Screen first, then
  confirm on openings the screen never used, and let only the confirmation
  decide. On its first real use the screen said +30 and the confirmation said
  +11, which is what selection on your own test set is worth.

## Findings

Every number below is from a match, not a guess. `NEXT_STEPS.md` has the
full record, including the ideas that failed.

| change | measured |
|---|---|
| raise alpha at the root after each move | +168 Elo at 1s/move |
| fix a transposition table keyed on the wrong side to move | +274 |
| one extra ply of search | about +140 |
| keep the cut-off iteration's better move, use the whole clock | +37 |
| train on 8M positions labelled by the champion | +11 over 6500 games |
| late move pruning stacked on SEE ordering | rejected |
| mixing the game result into the training target | rejected |
| training against win probability instead of pawns | rejected |
| doubling the network's hidden layer | 90.3% of variance explained against 90.2%, rejected |
| averaging the best epochs' weights | better held-out loss, weaker play, rejected |

The rejections are given without numbers on purpose. They were measured
before the harness handicap described above was found, so their magnitudes
are overstated by roughly 19 points. Correcting for it leaves every one of
them a rejection, which is why they are still listed, but the sizes in
`NEXT_STEPS.md` should be read with that offset in mind. The adoptions above
them were all measured either after the fix or with both sides running the
same binary behind UCI, where the handicap cannot apply.

The last two are the interesting ones. The network already predicts
everything about its teacher's search that piece placement can predict, so
capacity is not the constraint, and a lower held-out loss is not the same
thing as a stronger player.

## Layout

```
board/ moves/ game/   the rules
engine/               search, evaluation, match harness, UCI
nnue/                 training pools, labelling, PGN import
pytorch/              the trainer, exporter and Go-agreement check
play/ cli/ arena/     browser UI, terminal UI, tournaments
gauntlet/ calibrate/  head to head matches and Stockfish calibration
scripts/              measurement and coverage helpers
```

## Tests

```bash
go test ./...
./scripts/coverage.sh     # Go, union across packages
./scripts/pycoverage.sh   # Python, at 100%
```

The rules and the trainer are covered hardest, because a bug there is silent:
`game` reads 96.8% and `nnue` 94.5%, while `board` and `moves` look low in
isolation only because the engine's tests are what drive them.

Training pools, checkpoints and experiment networks are not in the
repository. They are large and reproducible: `nnue-bin` labels a PGN and
`pytorch/train.py` trains on the result.

## Licence

MIT, see `LICENSE`. Game data comes from the Lichess database, which is CC0.

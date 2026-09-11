<h1 align="center">chess-go</h1>

<p align="center">
  A chess engine written from scratch in Go, test first, that learns its
  evaluation only from scores it computed itself.
</p>

<p align="center">
  <img alt="Go 1.27" src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white">
  <img alt="Elo 2347" src="https://img.shields.io/badge/Elo-2347%20vs%20Stockfish%20%401s%2Fmove-2b6cb0">
  <img alt="MIT" src="https://img.shields.io/badge/licence-MIT-3fa34d">
  <img alt="no deps" src="https://img.shields.io/badge/engine%20deps-none-6b46c1">
</p>

---

## The rule

**No borrowed evaluations.** Game databases are fair game, so the training
positions come from real Lichess games. Pre-computed scores are not, so
Stockfish's opinion of a position never reaches a training target. Every
label is this engine's own search output.

Stockfish is a ruler here, never a teacher: it plays matches so the strength
number means something to other people.

## Quickstart

```bash
go build -o play-bin ./play && ./play-bin -port 8765   # play it at /gui.html
go build -o uci-bin ./uci && ./uci-bin                 # or speak UCI
go test ./...                                          # 90 test files
```

## Inside

**Search.** Alpha-beta with PVS, iterative deepening, aspiration windows,
transposition table, killers, history, countermoves, LMR, null move,
futility, SEE, check extensions, quiescence. Move generation is validated by
Perft against the published counts, Kiwipete included.

**Evaluation.** A HalfKP network (8 king buckets, 64 hidden, incremental
accumulator) blended 0.45 with a hand written evaluation. Trained in PyTorch,
run in Go, with a test that both agree to the last bit. How it is
trained, and what goes in and comes out: [TRAINING.md](TRAINING.md).

**Strength.** 2347 Elo with both sides on 1 second per move, and an even
score against Stockfish 2400 over 30 games. Ladders that pin Stockfish to a
shallow fixed depth read higher and flatter us, so this is the number to
quote.

## Highlights

Every figure below came from a match, not a hunch.

| | |
|---|---|
| 🥇 raise alpha at the root after each move | **+168** Elo at 1s/move |
| 🔑 transposition table keyed on the wrong side to move | **+274** |
| ⏱ keep the cut-off iteration's better move, use the whole clock | **+37** |
| 🧠 train on 8M positions the champion labelled itself | **+11** over 6500 games |
| 🧊 late move pruning on top of SEE ordering | rejected |
| 🎲 mixing the game result into the target | rejected |
| 📉 training against win probability instead of pawns | rejected |
| 🧱 doubling the hidden layer | 90.3% explained vs 90.2%, rejected |

The last two are the interesting ones. The network already predicts
everything about its teacher's search that piece placement can predict, and a
better held-out loss turned out not to mean a stronger player.

The rejections are listed without numbers on purpose: they were measured
before a harness bug was found and their sizes are overstated by about 19
points. Every direction survives the correction. `NEXT_STEPS.md` has the full
record with that caveat attached.

## Two habits worth stealing

- **Prove the ruler before you trust it.** The champion against itself must
  read zero. A bug that handicapped one side by 19 Elo survived seven
  training rounds because nobody ran that check at enough games to see it.
- **Never adopt on the race that picked the candidate.** Screen, then confirm
  on openings the screen never used, and let only the confirmation decide.
  First real use: the screen said +30, the confirmation said +11.

## Layout

```
board/ moves/ game/    rules
engine/                search, evaluation, match harness, UCI
nnue/ pytorch/         labelling and training
play/ cli/ arena/      browser UI, terminal, tournaments
gauntlet/ calibrate/   head to head, Stockfish calibration
```

Pools, checkpoints and experiment networks are not in the repo. They are
large and reproducible: `nnue-bin` labels a PGN, `pytorch/train.py` trains.

## Licence

MIT. Game data from the Lichess database, CC0.

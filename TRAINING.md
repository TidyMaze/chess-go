# How the network is trained

The engine never learns from anyone else's evaluation. A position's label is
what **this engine's own search** said about it, so the network is a
compression of the current champion's search into something that costs one
pass instead of thousands of nodes.

## The loop

```mermaid
flowchart LR
    PGN[Lichess PGN<br/>real games, CC0] --> POS[pick positions<br/>skip first 8 plies<br/>keep quiet ones]
    POS --> LABEL[champion searches<br/>each position to depth 3]
    LABEL --> POOL[(pool file<br/>features + target)]
    POOL --> TRAIN[PyTorch<br/>Adam, MSE, smoothing prior]
    TRAIN --> NET[net.json]
    NET --> CHECK{evaluates<br/>identically in Go?}
    CHECK -- no --> STOP[stop the rung]
    CHECK -- yes --> SCREEN[screen vs champion]
    SCREEN -- loses --> DROP[discard]
    SCREEN -- wins --> CONFIRM[confirm on openings<br/>the screen never used]
    CONFIRM -- clears zero --> ADOPT[new champion<br/>labels the next rung]
    CONFIRM -- does not --> DROP
```

The champion that labels a rung is the champion the rung must then beat. That
is the whole mechanism: no outside evaluation enters at any point.

## Inputs

The feature set is HalfKP. A position is described **twice**, once from each
side's point of view, and each description is a list of active features of the
form *(where my king is, what piece, on which square)*.

```mermaid
flowchart TD
    B[position] --> O[own perspective]
    B --> P[opponent perspective]
    O --> OF["active features<br/>king bucket x piece kind x square"]
    P --> PF["active features<br/>same encoding, mirrored"]
    OF --> OA["accumulator 64 values"]
    PF --> PA["accumulator 64 values"]
    OA --> C["concatenate to 128"]
    PA --> C
    C --> R["clipped ReLU, clamp to 0..1"]
    R --> OUT["one number, in pawns"]
```

| | |
|---|---|
| king buckets | 8 (the 64 king squares folded into 8 classes) |
| piece kinds | 10 (5 types x 2 colours, kings excluded) |
| squares | 64 |
| **inputs per perspective** | 8 x 10 x 64 = **5120** |
| first layer | 5120 x 64 = 327,680 weights |
| hidden | 64 per perspective, 128 concatenated |
| output layer | 128 weights, 1 bias |

Only a handful of inputs are non-zero (one per piece on the board), so the
first layer is never multiplied out. The accumulator is updated incrementally
as moves are made and unmade, which is what makes the network affordable
inside a search.

## Output

**One number: the position's value in pawns**, from the side to move.

It is deliberately *not* a win probability. A network emitting probabilities
has to be inverted by the search, and inverting amplifies: an error of 0.11 at
p=0.95 is four pawns. Training against win probability instead of pawns was
tried and measured worse, at -37 Elo.

At play time the number is blended with the hand written evaluation:

```
score = 0.45 x hand_eval + 0.55 x network
```

0.45 is measured, not chosen. The network alone, and every other weighting
tried (0.0, 0.2, 0.3, 0.55, 0.6, 0.65), played worse.

## The target

```
target = the champion's search score for that position, in pawns, clamped to +/-12
```

Three things about it were settled by matches rather than argument:

| question | answer | evidence |
|---|---|---|
| how deep should the labelling search go? | depth 3 | depths 3, 5 and 8 measured the same, and 3 labels 14x faster |
| mix in the game's result? | no | lambda 0.8 measured -29 and lambda 0.5 -52 against pure search scores |
| which positions? | quiet ones | a static function cannot learn tactics; positions where quiescence disagrees with the static score by more than 0.35 pawns are dropped |

## Training details

- **Loss**: mean squared error on pawns.
- **Split**: 15% held out **by game**, never by position, because consecutive
  positions in one game are near duplicates and splitting by position leaks.
- **Smoothing prior**: each square's weights are pulled toward its neighbours'
  after every epoch. Worth about +56 Elo, the single largest training-side
  effect ever measured here.
- **Stopping**: when the held-out loss stops improving by a fraction of the
  constant-predictor loss. A fraction, not an absolute number, because the
  size of the loss depends on which loss you use.
- **Contract**: the exported network must evaluate identically in Go, checked
  on a set of positions before the rung is allowed to race.

## What the numbers look like

A trained rung explains about 90% of the variance in its teacher's search
scores. The remaining 10% is mostly tactics, which piece placement cannot
predict: doubling the hidden layer to 128 moved it to 90.3% from 90.2% and
lost Elo, so capacity is not the constraint.

"""Where and how a recorded match against Stockfish is lost.

Reads gauntlet -games-out JSONL (our side is the one whose name starts with
"challenger"; scores are each mover's own view, one per ply). For every loss it
finds when our score first fell below -1.5 and when Stockfish's first rose above
+1.5, and classifies the loss:
  lag      Stockfish saw it 10+ plies before we did: an evaluation blind spot
  sudden   we saw it within 10 plies of Stockfish: a tactic or search horizon
  unseen   our score never fell below -1.5

Usage: loss_phases.py GAMES.jsonl
"""
import collections
import json
import statistics
import sys
from typing import Any, NamedTuple

import chess


class Loss(NamedTuple):
    kind: str
    ply: int  # ply where our score collapsed, -1 when unseen
    men: int  # men on the board at that ply


def our_parity(record: dict[str, Any]) -> int:
    """0 when our moves are at even plies of the record, 1 when odd."""
    board = chess.Board(record["start_fen"])
    we_white = record["white"].startswith("challenger")
    return 0 if (board.turn == chess.WHITE) == we_white else 1


def is_our_loss(record: dict[str, Any]) -> bool:
    # winner 1 is Black, 0 is White; decisive false is a draw.
    we_white = record["white"].startswith("challenger")
    return bool(record["decisive"]) and (record["winner"] == 1) == we_white


def classify(record: dict[str, Any]) -> Loss:
    ours = our_parity(record)
    scores: list[float | None] = record["scores"]
    us = next((i for i, s in enumerate(scores) if i % 2 == ours and s is not None and s < -1.5), None)
    sf = next((i for i, s in enumerate(scores) if i % 2 != ours and s is not None and s > 1.5), None)
    if us is None:
        return Loss("unseen", -1, 0)
    board = chess.Board(record["start_fen"])
    for m in record["moves"][:us]:
        move = chess.Move.from_uci(m)
        board.push(move if move in board.legal_moves else chess.Move.from_uci(m + "q"))
    kind = "lag" if sf is not None and us - sf >= 10 else "sudden"
    return Loss(kind, us, len(board.piece_map()))


def main() -> None:
    records = [json.loads(line) for line in open(sys.argv[1])]
    losses = [classify(r) for r in records if is_our_loss(r)]
    kinds = collections.Counter(loss.kind for loss in losses)
    print(f"{len(records)} games, {len(losses)} losses: {dict(kinds)}")
    for kind in ("lag", "sudden"):
        group = [loss for loss in losses if loss.kind == kind]
        if group:
            print(f"  {kind:6} median collapse ply {statistics.median(l.ply for l in group):.0f}, "
                  f"median men {statistics.median(l.men for l in group):.0f}")
    early = sum(1 for loss in losses if 0 <= loss.ply < 20)
    print(f"  collapsed before ply 20: {early}")


if __name__ == "__main__":
    main()

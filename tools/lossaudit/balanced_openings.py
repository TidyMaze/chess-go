"""Keep only start positions Stockfish scores as roughly level.

About 14% of openings.txt starts are already +1 or more for one side, so those
games are decided by the start rather than by the engines. Stockfish only judges
here: nothing it scores reaches a training pool.

Usage: balanced_openings.py IN OUT [count] [max-abs-pawns] [depth]
"""
import sys

import chess
import chess.engine


def is_balanced(score_cp: int | None, limit_pawns: float) -> bool:
    """Level enough to start a game from; a mate score never is."""
    return score_cp is not None and abs(score_cp) < 100 * limit_pawns


def usable(fen: str) -> bool:
    """A legal position Stockfish will accept; openings.txt holds some with impossible castling rights."""
    try:
        return chess.Board(fen).is_valid()
    except ValueError:
        return False


def main() -> None:
    src, out = sys.argv[1], sys.argv[2]
    count = int(sys.argv[3]) if len(sys.argv) > 3 else 20000
    limit = float(sys.argv[4]) if len(sys.argv) > 4 else 0.5
    depth = int(sys.argv[5]) if len(sys.argv) > 5 else 8
    engine = chess.engine.SimpleEngine.popen_uci("/opt/homebrew/bin/stockfish")
    engine.configure({"Threads": 1})
    kept = seen = 0
    with open(src) as f, open(out, "w") as o:
        for line in f:
            fen = line.split("|")[0].strip()
            if not fen:
                continue
            seen += 1
            if seen > count:
                break
            if not usable(fen):
                continue
            score = engine.analyse(chess.Board(fen), chess.engine.Limit(depth=depth))["score"].white()
            if is_balanced(score.score(), limit):
                o.write(fen + "\n")
                kept += 1
    engine.quit()
    print(f"{kept} of {min(seen, count)} positions within {limit} pawns at depth {depth}", file=sys.stderr)


if __name__ == "__main__":
    main()

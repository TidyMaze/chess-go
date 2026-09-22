"""Stockfish eval curve and biggest drops for one side of a PGN.

Usage: pgn_audit.py GAME.pgn BOTNAME [depth]
"""
import sys

import chess
import chess.engine
import chess.pgn


def main() -> None:
    path, bot = sys.argv[1], sys.argv[2]
    depth = int(sys.argv[3]) if len(sys.argv) > 3 else 18
    with open(path) as f:
        game = chess.pgn.read_game(f)
    assert game is not None
    side = chess.WHITE if game.headers["White"] == bot else chess.BLACK
    engine = chess.engine.SimpleEngine.popen_uci("/opt/homebrew/bin/stockfish")
    board = game.board()
    rows = []
    prev = engine.analyse(board, chess.engine.Limit(depth=depth))["score"].pov(side)
    for move in game.mainline_moves():
        mover = board.turn
        best = engine.analyse(board, chess.engine.Limit(depth=depth))
        san = board.san(move)
        best_san = board.san(best["pv"][0]) if best.get("pv") else "?"
        num = board.fullmove_number
        board.push(move)
        after = engine.analyse(board, chess.engine.Limit(depth=depth))["score"].pov(side)
        cp_before = prev.score(mate_score=10000)
        cp_after = after.score(mate_score=10000)
        rows.append((num, mover, san, best_san, cp_before, cp_after))
        prev = after
    engine.quit()
    print("move  side  played   sf-best   eval(bot pov) before -> after")
    for num, mover, san, best_san, b, a in rows:
        tag = "BOT" if mover == side else "opp"
        mark = "  <== %+d" % (a - b) if mover == side and a - b <= -80 else ""
        print("%3d   %s  %-8s %-8s %6d -> %6d%s" % (num, tag, san, best_san, b, a, mark))


if __name__ == "__main__":
    main()

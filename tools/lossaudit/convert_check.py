"""Does the engine convert a won position before the fifty-move rule draws it?

Each engine plays the side to move against Stockfish defending, until mate, a
draw (the automatic ones Lichess applies: fifty moves, threefold, stalemate,
insufficient material) or a ply cap.
  game   positions of a PGN where SIDE is to move, from a ply on, with the
         game's own halfmove clock; every EVERY-th one plus the last
  suite  textbook won endgames and positions from the game, each started at
         every halfmove clock of --clocks

Every finished game is appended to OUT.jsonl before the next one starts, and a
rerun skips the games already there, so a run cut by a timeout resumes.

Usage:
  convert_check.py game OUT.jsonl --pgn GAME.pgn --engine NAME=WRAPPER ... [--from-ply 109] [--every 10]
  convert_check.py suite OUT.jsonl --engine NAME=WRAPPER ... [--clocks 0,80]
  convert_check.py report OUT.jsonl
"""
import argparse
import io
import json
import subprocess
import time
from pathlib import Path
from typing import Any, NamedTuple

import chess
import chess.engine
import chess.pgn

STOCKFISH = "/opt/homebrew/bin/stockfish"

SUITE = [
    ("KQK", "8/8/8/3k4/8/8/8/KQ6 w - - 0 1"),
    ("KRK", "8/8/8/3k4/8/8/8/KR6 w - - 0 1"),
    ("KBBK", "8/8/8/3k4/8/8/8/KBB5 w - - 0 1"),
    ("KBNK", "8/8/8/3k4/8/8/8/KBN5 w - - 0 1"),
    ("KRBK", "8/8/8/3k4/8/8/8/KRB5 w - - 0 1"),
    ("KQ+P vs K", "8/8/8/3k4/8/8/4P3/KQ6 w - - 0 1"),
    ("KR+2P vs KP", "8/8/4k3/7p/8/6PP/8/R5K1 w - - 0 1"),
    ("game ply 110", "2R5/1K4k1/3B2p1/3P2P1/1p6/1P6/1P6/8 w - - 1 56"),
    ("game ply 150", "3k4/5R2/1K4p1/3PB1P1/1p6/1P6/1P6/8 w - - 41 76"),
    ("game ply 208", "8/6B1/1k2KRp1/3P2P1/1p6/1P6/1P6/8 w - - 99 105"),
]


class Start(NamedTuple):
    name: str
    fen: str


def with_clock(fen: str, clock: int) -> str:
    """The same position with its halfmove clock set to CLOCK."""
    fields = fen.split()
    fields[4] = str(clock)
    return " ".join(fields)


def termination(board: chess.Board) -> str | None:
    """How the game ended, or None. Mate is checked first: a mate delivered by
    the move that brings the clock to 100 still wins (FIDE 9.3). Draws are the
    ones Lichess applies by itself, so the fifty-move draw comes at clock 100,
    not a move early as python-chess's claim test would have it."""
    if board.is_checkmate():
        return "mate"
    if board.is_stalemate():
        return "stalemate"
    if board.is_insufficient_material():
        return "insufficient"
    if board.halfmove_clock >= 100:
        return "fifty"
    if board.is_repetition(3):
        return "threefold"
    return None


def game_starts(pgn_text: str, from_ply: int, side: chess.Color) -> list[Start]:
    """Every position of the game from FROM_PLY on where SIDE is to move and the game is not over."""
    game = chess.pgn.read_game(io.StringIO(pgn_text))
    if game is None:
        raise ValueError("no game in the PGN")
    board = game.board()
    starts = []
    for ply, move in enumerate(game.mainline_moves(), 1):
        board.push(move)
        if ply >= from_ply and board.turn == side and termination(board) is None:
            starts.append(Start(f"ply {ply}", board.fen()))
    return starts


def sample(starts: list[Start], every: int) -> list[Start]:
    """Every EVERY-th start from the first, plus the last one."""
    picked = starts[::every]
    if starts and starts[-1] not in picked:
        picked.append(starts[-1])
    return picked


def game_key(mode: str, name: str, clock: int, engine: str) -> str:
    return f"{mode}|{name}|{clock}|{engine}"


def done_keys(path: Path) -> set[str]:
    """Keys of the games already recorded. A torn last line (a run killed mid-write) is ignored."""
    if not path.exists():
        return set()
    keys = set()
    for line in path.read_text().splitlines():
        try:
            keys.add(json.loads(line)["key"])
        except (json.JSONDecodeError, KeyError, TypeError):
            continue
    return keys


def score_text(score: chess.engine.PovScore | None, side: chess.Color) -> str:
    if score is None:
        return "?"
    pov = score.pov(side)
    return f"mate {pov.mate()}" if pov.is_mate() else f"cp {pov.score()}"


def parse_bestmove(board: chess.Board, lines: list[str]) -> tuple[chess.Move, str]:
    """The move and the last "score ... depth" of one search. Our server prints a
    promotion without its piece ("d7d8"), which python-chess rejects as illegal;
    it only ever promotes to a queen, so a bare pawn move to the last rank gets "q"."""
    score = "?"
    for line in lines:
        f = line.split()
        if f[:1] == ["info"] and "score" in f:
            i = f.index("score")
            depth = f[f.index("depth") + 1] if "depth" in f else "?"
            score = f"{f[i + 1]} {f[i + 2]} d{depth}"
    uci = lines[-1].split()[1]
    if len(uci) == 4:
        piece = board.piece_at(chess.parse_square(uci[:2]))
        if piece and piece.piece_type == chess.PAWN and uci[3] in "18":
            uci += "q"
    return board.parse_uci(uci), score


class Ours:
    """Our UCI server, spoken to directly (see parse_bestmove for why not through python-chess)."""

    def __init__(self, cmd: str) -> None:
        self.proc = subprocess.Popen([cmd], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)
        self.send("uci")
        self.until("uciok")

    def send(self, line: str) -> None:
        assert self.proc.stdin is not None
        self.proc.stdin.write(line + "\n")
        self.proc.stdin.flush()

    def until(self, prefix: str) -> list[str]:
        assert self.proc.stdout is not None
        lines = []
        while True:
            line = self.proc.stdout.readline()
            if not line:
                raise RuntimeError(f"engine exited before {prefix!r}")
            lines.append(line.strip())
            if line.startswith(prefix):
                return lines

    def new_game(self) -> None:
        self.send("ucinewgame")
        self.send("isready")
        self.until("readyok")

    def play(self, board: chess.Board, movetime_ms: int) -> tuple[chess.Move, str]:
        moves = " ".join(m.uci() for m in board.move_stack)
        self.send(f"position fen {board.root().fen()}" + (f" moves {moves}" if moves else ""))
        self.send(f"go movetime {movetime_ms}")
        return parse_bestmove(board, self.until("bestmove"))

    def quit(self) -> None:
        self.send("quit")
        self.proc.wait(timeout=10)


def play_game(ours: Ours, sf: chess.engine.SimpleEngine, fen: str, key: str,
              our_time: float, sf_time: float, max_plies: int) -> dict[str, Any]:
    """Our engine plays the side to move, Stockfish the other, until the game ends or MAX_PLIES."""
    board = chess.Board(fen)
    side = board.turn
    scores: list[str] = []
    max_clock, resets = board.halfmove_clock, 0
    ours.new_game()
    while (end := termination(board)) is None and len(board.move_stack) < max_plies:
        if board.turn == side:
            move, score = ours.play(board, round(our_time * 1000))
            scores.append(score)
        else:
            r = sf.play(board, chess.engine.Limit(time=sf_time), game=key)
            if r.move is None:
                raise RuntimeError(f"{key}: Stockfish gave no move at {board.fen()}")
            move = r.move
        resets += board.is_zeroing(move)
        board.push(move)
        max_clock = max(max_clock, board.halfmove_clock)
    won = end == "mate" and board.turn != side
    return {
        "result": end or "cap",
        "converted": won,
        "lost": end == "mate" and not won,
        "plies": len(board.move_stack),
        "our_moves": len(scores),
        "resets": resets,
        "max_clock": max_clock,
        "final_fen": board.fen(),
        "moves": [m.uci() for m in board.move_stack],
        "scores": scores,
    }


def cell(row: dict[str, Any]) -> str:
    """One table cell: mate in N of our moves, or the draw and the plies it took."""
    if row["converted"]:
        return f"mate in {row['our_moves']}"
    if row["lost"]:
        return f"LOST in {row['plies']} plies"
    if row["result"] == "cap":
        return f"no mate in {row['plies']} plies (clock {row['max_clock']})"
    return f"draw: {row['result']} after {row['plies']} plies"


def by_key(rows: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    return {r["key"]: r for r in rows}


def report(rows: list[dict[str, Any]]) -> str:
    """Markdown tables, one per mode, engines side by side, and conversions per engine and clock."""
    out = []
    engines = list(dict.fromkeys(r["engine"] for r in rows))
    table = by_key(rows)
    for mode in ("game", "suite"):
        mine = [r for r in rows if r["mode"] == mode]
        if not mine:
            continue
        starts = list(dict.fromkeys((r["name"], r["clock"]) for r in mine))
        head = ["start", "clock", "Stockfish at start"] + engines + [f"{e} fifty-move draw" for e in engines]
        out += [f"### {mode}", "", "| " + " | ".join(head) + " |", "|" + "---|" * len(head)]
        for name, clock in starts:
            got = [table.get(game_key(mode, name, clock, e)) for e in engines]
            sf = next((g["sf_start"] for g in got if g), "?")
            cells = [cell(g) if g else "not run" for g in got]
            fifty = ["yes" if g and g["result"] == "fifty" else "no" if g else "-" for g in got]
            out.append("| " + " | ".join([name, str(clock), sf] + cells + fifty) + " |")
        out += ["", "| engine | clock | converted | games |", "|---|---|---|---|"]
        for e in engines:
            for clock in sorted({c for _, c in starts}) if mode == "suite" else ["all"]:
                games = [r for r in mine if r["engine"] == e and (clock == "all" or r["clock"] == clock)]
                out.append(f"| {e} | {clock} | {sum(r['converted'] for r in games)} | {len(games)} |")
        out.append("")
    return "\n".join(out)


def run(args: argparse.Namespace, starts: list[tuple[Start, int]]) -> None:
    out = Path(args.out)
    done = done_keys(out)
    engines = dict(spec.split("=", 1) for spec in args.engine)
    todo = [(s, c, e) for s, c in starts for e in engines if game_key(args.mode, s.name, c, e) not in done]
    print(f"{len(todo)} games to play, {len(starts) * len(engines) - len(todo)} already in {out}", flush=True)
    if not todo:
        return
    opened = {e: Ours(engines[e]) for e in engines}
    sf = chess.engine.SimpleEngine.popen_uci(STOCKFISH)
    sf_start: dict[str, str] = {}
    try:
        for start, clock, name in todo:
            fen = with_clock(start.fen, clock)
            if fen not in sf_start:
                info = sf.analyse(chess.Board(fen), chess.engine.Limit(time=1.0))
                sf_start[fen] = score_text(info.get("score"), chess.Board(fen).turn)
            key = game_key(args.mode, start.name, clock, name)
            t0 = time.monotonic()
            row = play_game(opened[name], sf, fen, key, args.our_time, args.sf_time, args.max_plies)
            row.update(key=key, mode=args.mode, name=start.name, clock=clock, engine=name, fen=fen,
                       sf_start=sf_start[fen], our_time=args.our_time, sf_time=args.sf_time,
                       wall_s=round(time.monotonic() - t0, 1))
            with out.open("a") as f:
                f.write(json.dumps(row) + "\n")
            print(f"{key}: {cell(row)} (max clock {row['max_clock']}, {row['wall_s']}s)", flush=True)
    finally:
        for e in opened.values():
            e.quit()
        sf.quit()


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("mode", choices=["game", "suite", "report"])
    p.add_argument("out")
    p.add_argument("--engine", action="append", default=[], help="NAME=WRAPPER, repeatable")
    p.add_argument("--pgn")
    p.add_argument("--from-ply", type=int, default=109)
    p.add_argument("--every", type=int, default=10)
    p.add_argument("--side", choices=["white", "black"], default="white")
    p.add_argument("--clocks", default="0,80")
    p.add_argument("--only", help="comma-separated start names to keep")
    p.add_argument("--our-time", type=float, default=1.0)
    p.add_argument("--sf-time", type=float, default=0.2)
    p.add_argument("--max-plies", type=int, default=100)
    args = p.parse_args()
    if args.mode == "report":
        print(report([json.loads(line) for line in Path(args.out).read_text().splitlines() if line.strip()]))
        return
    if not args.engine:
        p.error("at least one --engine NAME=WRAPPER")
    if args.mode == "game":
        if not args.pgn:
            p.error("game mode needs --pgn")
        side = chess.WHITE if args.side == "white" else chess.BLACK
        picked = sample(game_starts(Path(args.pgn).read_text(), args.from_ply, side), args.every)
        starts = [(s, chess.Board(s.fen).halfmove_clock) for s in picked]
    else:
        clocks = [int(c) for c in args.clocks.split(",")]
        starts = [(Start(n, f), c) for n, f in SUITE for c in clocks]
    if args.only:
        keep = set(args.only.split(","))
        starts = [(s, c) for s, c in starts if s.name in keep]
    run(args, starts)


if __name__ == "__main__":
    main()

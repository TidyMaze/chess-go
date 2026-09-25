"""What our engine really does at a short clock: completed depth, nodes, move.

Replays positions through uci-bin at a fixed movetime, with N engines running
at once to reproduce the load of a match, and reports whether the move played
in the game comes back. A study of games lost at 100 ms found replays reaching
only depth 5 to 8, and game moves no fresh fixed-depth search reproduces.

Usage: depth_probe.py POSITIONS.json OUT.jsonl MOVETIME_MS PARALLEL [champion.json]
POSITIONS.json: [{"game": int, "fen": str, "game_move": str}, ...]
"""
import json
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import NamedTuple

REPO = Path(__file__).resolve().parents[2]
INFO = re.compile(r"^info depth (\d+) score (cp|mate) (-?\d+) nodes (\d+)")


class Search(NamedTuple):
    depth: int
    nodes: int
    move: str
    score_cp: int | None


def parse_search(output: str) -> Search:
    """The deepest completed iteration's depth, nodes and score, and the move."""
    depth, nodes, score = 0, 0, None
    move = ""
    for line in output.splitlines():
        m = INFO.match(line)
        if m:
            depth, nodes = int(m.group(1)), int(m.group(4))
            value = int(m.group(3))
            score = value if m.group(2) == "cp" else (100000 - abs(value)) * (1 if value > 0 else -1)
        elif line.startswith("bestmove "):
            move = line.split()[1]
    return Search(depth, nodes, move, score)


def search(fen: str, movetime_ms: int, champion: str) -> Search:
    commands = f"uci\nisready\nposition fen {fen}\ngo movetime {movetime_ms}\n"
    proc = subprocess.Popen([str(REPO / "uci-bin"), "-champion", champion], stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, cwd=REPO)
    assert proc.stdin is not None and proc.stdout is not None
    proc.stdin.write(commands)
    proc.stdin.flush()
    lines = []
    for line in proc.stdout:
        lines.append(line)
        if line.startswith("bestmove"):
            break
    proc.stdin.close()
    proc.kill()
    return parse_search("".join(lines))


def main() -> None:
    positions = json.loads(Path(sys.argv[1]).read_text())
    out, movetime, parallel = Path(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
    champion = sys.argv[5] if len(sys.argv) > 5 else "champion.json"
    with ThreadPoolExecutor(parallel) as pool:
        found = list(pool.map(lambda p: search(p["fen"], movetime, champion), positions))
    with out.open("w") as f:
        for p, r in zip(positions, found):
            f.write(json.dumps({**p, "depth": r.depth, "nodes": r.nodes, "move": r.move, "score_cp": r.score_cp,
                                "game_move_again": r.move == p["game_move"]}) + "\n")
    depths = sorted(r.depth for r in found)
    again = sum(r.move == p["game_move"] for p, r in zip(positions, found))
    print(f"{len(found)} positions at {movetime} ms, {parallel} at once: depth min {depths[0]} "
          f"median {depths[len(depths) // 2]} max {depths[-1]}; game move played again in {again}")


if __name__ == "__main__":
    main()

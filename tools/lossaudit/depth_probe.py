"""What our engine really does at a short clock: depth, nodes, move, cut-off.

Replays positions through uci-bin at a fixed movetime, RUNS times each with a
seeded tie-break, on PARALLEL long-lived engines at once to reproduce a match's
load. Each engine is warmed up by one search first, so a fresh table's
first-touch cost does not shorten the searches measured. For every search it
records whether the move is the one played in the game (a bad move) and
whether it came from an iteration the clock cut off.

Usage: depth_probe.py POSITIONS.json OUT.jsonl MOVETIME_MS PARALLEL RUNS [champion.json]
POSITIONS.json: [{"game": int, "fen": str, "game_move": str}, ...]
"""
import json
import queue
import re
import subprocess
import sys
import threading
from pathlib import Path
from typing import Any, NamedTuple

REPO = Path(__file__).resolve().parents[2]
INFO = re.compile(r"^info depth (\d+) score (cp|mate) (-?\d+) nodes (\d+)")
CUTOFF = re.compile(r"^info string cutoff ([01])")
WARMUP_FEN = "r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3"


class Search(NamedTuple):
    depth: int
    nodes: int
    move: str
    score_cp: int | None
    cutoff: bool | None


def parse_search(output: str) -> Search:
    """The deepest completed iteration's depth, nodes and score, the move, and the cut-off flag."""
    depth, nodes, score, cutoff = 0, 0, None, None
    move = ""
    for line in output.splitlines():
        m = INFO.match(line)
        c = CUTOFF.match(line)
        if m:
            depth, nodes = int(m.group(1)), int(m.group(4))
            value = int(m.group(3))
            score = value if m.group(2) == "cp" else (100000 - abs(value)) * (1 if value > 0 else -1)
        elif c:
            cutoff = c.group(1) == "1"
        elif line.startswith("bestmove "):
            move = line.split()[1]
    return Search(depth, nodes, move, score, cutoff)


def rates(rows: list[dict[str, Any]]) -> dict[str, int]:
    """How many searches played the game's bad move, and how many of those came from a cut-off iteration."""
    return {
        "searches": len(rows),
        "bad": sum(1 for r in rows if r["bad"]),
        "bad_from_cutoff": sum(1 for r in rows if r["bad"] and r["cutoff"]),
        "cutoff": sum(1 for r in rows if r["cutoff"]),
    }


class Engine:
    def __init__(self, champion: str) -> None:
        self.proc = subprocess.Popen([str(REPO / "uci-bin"), "-champion", champion], stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, cwd=REPO)

    def search(self, fen: str, movetime_ms: int, seed: int) -> Search:
        assert self.proc.stdin is not None and self.proc.stdout is not None
        self.proc.stdin.write(f"setoption name Seed value {seed}\nposition fen {fen}\ngo movetime {movetime_ms}\n")
        self.proc.stdin.flush()
        lines = []
        for line in self.proc.stdout:
            lines.append(line)
            if line.startswith("bestmove"):
                break
        return parse_search("".join(lines))

    def close(self) -> None:
        self.proc.kill()


def main() -> None:
    positions = json.loads(Path(sys.argv[1]).read_text())
    out, movetime, parallel, runs = Path(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5])
    champion = sys.argv[6] if len(sys.argv) > 6 else "champion.json"
    tasks: "queue.Queue[tuple[dict[str, Any], int]]" = queue.Queue()
    for run in range(1, runs + 1):
        for p in positions:
            tasks.put((p, run))
    rows: list[dict[str, Any]] = []
    lock = threading.Lock()

    def worker() -> None:
        engine = Engine(champion)
        engine.search(WARMUP_FEN, movetime, 0)
        while True:
            try:
                p, run = tasks.get_nowait()
            except queue.Empty:
                break
            r = engine.search(p["fen"], movetime, run)
            with lock:
                rows.append({**p, "run": run, "depth": r.depth, "nodes": r.nodes, "move": r.move,
                             "score_cp": r.score_cp, "cutoff": r.cutoff, "bad": r.move == p["game_move"]})
        engine.close()

    threads = [threading.Thread(target=worker) for _ in range(parallel)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    with out.open("w") as f:
        for row in rows:
            f.write(json.dumps(row) + "\n")
    depths = sorted(r["depth"] for r in rows)
    s = rates(rows)
    print(f"{s['searches']} searches at {movetime} ms, {parallel} engines at once: depth median "
          f"{depths[len(depths) // 2]}; bad move {s['bad']} ({100 * s['bad'] / s['searches']:.1f}%), "
          f"{s['bad_from_cutoff']} of them from a cut-off iteration; cut-off moves overall {s['cutoff']}")


if __name__ == "__main__":
    main()

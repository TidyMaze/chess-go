"""Aggregate the three probe chunks into class, phase, speed and pattern tables.

Usage: report_data.py > report_data_output.txt
Class per mistake: chunk 0 uses the SF 3 s recheck verdict when there is one,
chunk 1 its final_verdict, chunk 2 its 30 s verdict_robust ('depth' is printed
as depth-fixed); the task-spec verdict is counted too. The pattern of each
mistake is PATTERN below, grouped from the chunk agents' labels; 8NmPGEfJ:44
moved to passed pawns after SF showed 24.g4 hxg4 creating an outside h-passer.
"""

import json
from collections import Counter, defaultdict

PASSER, KING, MINOR = "passed pawns / pawn races", "king safety", "minor-piece & endgame judgement"
TACTIC, CONV, OPEN, OTHER = "short tactic", "conversion / prophylaxis", "opening", "other positional"
PATTERN = {
    "0pvMtfkF:52": MINOR, "0pvMtfkF:54": PASSER, "5oG7XqrX:73": MINOR, "8eQ3xHrn:80": MINOR,
    "8eQ3xHrn:86": MINOR, "CFxbTTbm:175": PASSER, "CFxbTTbm:201": PASSER, "DchppvSq:48": KING,
    "H00KZ8Zi:47": CONV, "MXEKlu5J:11": TACTIC, "MXEKlu5J:49": CONV, "VIv1K6hG:30": KING,
    "ZKqmJmWf:164": PASSER, "edywGYGk:20": TACTIC, "owIpx8vO:39": KING, "owIpx8vO:59": KING,
    "19WhnReJ:85": PASSER, "19WhnReJ:87": PASSER, "6w2cGg7b:74": PASSER, "6w2cGg7b:88": PASSER,
    "9Tr32GxM:62": PASSER, "9Tr32GxM:64": PASSER, "EOS2sALb:37": CONV, "LCmlLCpG:19": OPEN,
    "LCmlLCpG:73": OTHER, "YwJ4nQC2:34": KING, "YwJ4nQC2:38": KING, "aJeii27Z:134": KING,
    "fRMNWevI:67": TACTIC, "lYIjzJrt:36": TACTIC, "p3uIqx8X:162": MINOR, "tFbHzkWe:120": KING,
    "uScrvAwN:36": KING, "uScrvAwN:42": KING, "8NmPGEfJ:44": PASSER, "FNBpPuJt:50": OTHER,
    "LT6K3dEg:33": TACTIC, "LT6K3dEg:51": TACTIC, "TalsX4r0:32": PASSER, "TalsX4r0:42": OTHER,
    "cVtd9Y8I:112": PASSER, "cVtd9Y8I:120": PASSER, "hThp7eKv:41": MINOR, "hThp7eKv:89": MINOR,
    "qXlUX6kr:43": KING, "qXlUX6kr:49": KING, "th0nCR2K:16": OPEN, "th0nCR2K:104": PASSER,
    "xJ46lS17:57": MINOR, "xJ46lS17:69": MINOR,
}


def load() -> tuple[list[dict], dict]:
    rows = []
    for c in range(3):
        for m in json.load(open(f"probe_chunk{c}.json")):
            if not m.get("fen"):
                continue
            spec = m["verdict"]
            cls = {0: m.get("verdict_recheck") or spec, 1: m.get("final_verdict") or spec,
                   2: {"depth": "depth-fixed"}.get(m.get("verdict_robust"), m.get("verdict_robust"))}[c]
            rows.append({**m, "key": f"{m['game']}:{m['ply']}", "chunk": c, "cls": cls, "spec": spec})
    games = {g["id"]: g for g in map(json.loads, open("games_since_0927.ndjson"))}
    return rows, games


def main() -> None:
    rows, games = load()
    us = {gid: ("white" if g["players"]["white"].get("user", {}).get("id") == "tidymazebot" else "black")
          for gid, g in games.items()}
    res = {gid: "win" if g.get("winner") == us[gid] else "draw" if not g.get("winner") else "loss"
           for gid, g in games.items()}
    nonwin = sorted(gid for gid in games if res[gid] != "win")
    print(f"games {len(games)}: {Counter(res.values())}; non-wins {len(nonwin)}")
    print("non-wins by speed:", dict(Counter((games[g]['speed'], res[g]) for g in nonwin)))
    print("all games by speed:", dict(Counter(g["speed"] for g in games.values())))
    print("\nmistakes", len(rows), "class:", dict(Counter(r["cls"] for r in rows)),
          "task-spec:", dict(Counter(r["spec"] for r in rows)))
    for field in ("phase", "speed"):
        t = defaultdict(Counter)
        for r in rows:
            t[r[field]][r["cls"]] += 1
        print(field, {k: dict(v) for k, v in t.items()})
    missing = [r["key"] for r in rows if r["key"] not in PATTERN]
    assert not missing, missing
    print("\npattern | mistakes | eval | depth+timing | games (L/D) | games with an eval mistake")
    by = defaultdict(list)
    for r in rows:
        by[PATTERN[r["key"]]].append(r)
    for p, rs in sorted(by.items(), key=lambda kv: -len({r["game"] for r in kv[1]})):
        gs = sorted({r["game"] for r in rs})
        ev = sorted({r["game"] for r in rs if r["cls"] == "eval"})
        ld = Counter(res[g] for g in gs)
        print(f"{p} | {len(rs)} | {sum(r['cls'] == 'eval' for r in rs)} | {sum(r['cls'] != 'eval' for r in rs)}"
              f" | {len(gs)} ({ld['loss']}L/{ld['draw']}D) | {len(ev)}: {' '.join(ev)}")
    per_game = defaultdict(list)
    for r in rows:
        per_game[r["game"]].append(r)
    depth_only = sorted(g for g, rs in per_game.items() if all(r["cls"] != "eval" for r in rs))
    print("\ngames whose every probed mistake is depth/timing:", len(depth_only), depth_only,
          "of", len(per_game), "games with a probed mistake")
    print("\n| game | result | speed | our colour | status | mistakes (ply: class, pattern) |")
    print("| --- | --- | --- | --- | --- | --- |")
    for gid in nonwin:
        g = games[gid]
        ms = "; ".join(f"{r['ply']}: {r['cls']}, {PATTERN[r['key']]}"
                       for r in sorted(per_game.get(gid, []), key=lambda r: r["ply"])) or "none probed"
        print(f"| [{gid}](https://lichess.org/{gid}) | {res[gid]} | {g['speed']} | {us[gid]} | {g['status']} | {ms} |")


if __name__ == "__main__":
    main()

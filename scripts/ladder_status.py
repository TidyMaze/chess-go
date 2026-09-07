#!/usr/bin/env python3
"""Turn the ladder's log into JSON the browser can poll.

The ladder is a shell script and its progress lives in a log. The browser
needs structure: which rung is running, what each finished rung measured,
and whether the climb is still going. Parsing the log rather than having the
script write JSON keeps the two independent, so this can be changed while
the ladder is mid-rung, which matters because editing a running bash script
corrupts it.

Run it in a loop:
    while :; do scripts/ladder_status.py; sleep 5; done
"""

import json
import os
import re
import sys
import time
from pathlib import Path

LOG = Path("/tmp/chesslogs/ladder.log")
OUT = Path("ladder.json")

RUNG = re.compile(r"^(\d\d:\d\d:\d\d)\s+rung (\d+): (.*)$")
POOLED = re.compile(r"pooled (\d+) games: W-D-L (\d+)-(\d+)-(\d+), score ([\d.]+), Elo ([-+]\d+) \+/- (\d+)")
ADOPTED = re.compile(r"ADOPTED \(\+?([-+]\d+), lower bound ([-+]\d+)\)")
REJECTED = re.compile(r"not adopted \(lower bound ([-+]?\d+)\)")
EXPLAINS = re.compile(r"trained, explains ([\d.]+)%")
POOLSIZE = re.compile(r"holds (\d+) positions")
GENERATING = re.compile(r"generating (\d+) more positions \(have (\d+) of (\d+)\)")


def parse():
    if not LOG.exists():
        return {"rungs": [], "phase": "not started"}

    rungs, order = {}, []
    phase, current = "idle", None
    for line in LOG.read_text().splitlines():
        m = RUNG.match(line)
        if not m:
            continue
        stamp, num, rest = m.group(1), int(m.group(2)), m.group(3)
        r = rungs.setdefault(num, {"rung": num, "state": "running"})
        if num not in order:
            order.append(num)
        current, r["at"] = num, stamp

        if g := GENERATING.search(rest):
            phase, r["phase"] = "generating", "generating"
            r["have"], r["target"] = int(g.group(2)), int(g.group(3))
        elif g := POOLSIZE.search(rest):
            r["positions"] = int(g.group(1))
        elif rest.startswith("training on"):
            phase, r["phase"] = "training", "training"
        elif g := EXPLAINS.search(rest):
            r["explains"] = float(g.group(1))
            phase, r["phase"] = "racing", "racing"
        elif g := POOLED.search(rest):
            r["games"] = int(g.group(1))
            r["wdl"] = [int(g.group(2)), int(g.group(3)), int(g.group(4))]
            r["elo"] = int(g.group(6))
            r["margin"] = int(g.group(7))
        elif g := ADOPTED.search(rest):
            r["state"], r["lower"] = "adopted", int(g.group(2))
            phase = "idle"
        elif g := REJECTED.search(rest):
            r["state"], r["lower"] = "rejected", int(g.group(1))
            phase = "stopped"

    out = [rungs[n] for n in sorted(order)]
    # A rung with no verdict yet is the one in flight.
    running = [r for r in out if r["state"] == "running"]
    return {
        "rungs": out,
        "current": running[-1]["rung"] if running else None,
        "phase": phase if running else ("stopped" if any(r["state"] == "rejected" for r in out) else "finished"),
        "adopted": sum(1 for r in out if r["state"] == "adopted"),
        "at": int(time.time()),
        "log_age_sec": int(time.time() - LOG.stat().st_mtime),
    }


def main():
    st = parse()
    try:
        champ = json.loads(Path("champion.json").read_text())
        st["champion"] = {"label": champ.get("label"), "elo": champ.get("elo"),
                          "margin": champ.get("margin")}
    except Exception:
        pass
    tmp = OUT.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(st))
    os.replace(tmp, OUT)
    if "--print" in sys.argv:
        print(json.dumps(st, indent=1))


if __name__ == "__main__":
    main()

"""SPSA tuning of the search margins, champion against itself at a short clock.

Each iteration perturbs every parameter up or down at random, races theta+
against theta- for a few games, and steps each parameter toward the side that
won. Every iteration is appended to the journal before the next one starts, and
a restart resumes from its last line.

Each invocation stops by itself before its time budget (default 25 minutes) and
the next one resumes, so no single process runs for hours.

Usage: spsa.py JOURNAL [iterations] [games-per-iteration] [budget-minutes]
"""
import json
import random
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import NamedTuple

REPO = Path(__file__).resolve().parents[2]


class Param(NamedTuple):
    start: float
    c: float  # perturbation size, in the parameter's own units


# Starting points are the engine's current literals (engine/searchtune.go).
PARAMS: dict[str, Param] = {
    "RFPBase": Param(0.5, 0.15),
    "RFPSlope": Param(0.35, 0.08),
    "RazorBase": Param(2.0, 0.4),
    "RazorSlope": Param(1.5, 0.3),
    "FutilityPerDepth": Param(1.0, 0.2),
    "LMRBase": Param(0.5, 0.15),
    "LMRDiv": Param(2.5, 0.3),
    "LMPBase": Param(3.0, 1.0),
    "NullBase": Param(3.0, 0.5),
    "NullBonusPer": Param(1.5, 0.3),
    "AspDelta": Param(0.5, 0.12),
    "DeltaMargin": Param(2.0, 0.4),
}


def parse_wdl(out: str) -> tuple[int, int, int]:
    m = re.search(r"W-D-L (\d+)-(\d+)-(\d+)", out)
    if not m:
        raise ValueError("no W-D-L record in gauntlet output:\n" + out[-500:])
    return int(m.group(1)), int(m.group(2)), int(m.group(3))


def step(
    theta: dict[str, float], params: dict[str, Param], delta: dict[str, int],
    wins: int, draws: int, losses: int, lr: float,
) -> dict[str, float]:
    """Move each parameter by lr * c * (net result of theta+) * its perturbation sign."""
    games = wins + draws + losses
    result = (wins - losses) / games if games else 0.0
    return {k: theta[k] + lr * params[k].c * result * delta[k] for k in theta}


def resume(journal: Path, params: dict[str, Param]) -> tuple[int, dict[str, float]]:
    if not journal.exists():
        return 0, {k: p.start for k, p in params.items()}
    lines = [ln for ln in journal.read_text().splitlines() if ln.strip()]
    if not lines:
        return 0, {k: p.start for k, p in params.items()}
    last = json.loads(lines[-1])
    return int(last["iter"]) + 1, {k: float(v) for k, v in last["theta"].items()}


def has_time_for_another(elapsed_s: float, last_round_s: float, budget_s: float) -> bool:
    """Whether one more round, as long as the last one, still fits in the budget."""
    return elapsed_s + last_round_s <= budget_s


def tune_string(theta: dict[str, float]) -> str:
    return ",".join(f"{k}={v:.4f}" for k, v in sorted(theta.items()))


def race(plus: dict[str, float], minus: dict[str, float], games: int, offset: int) -> tuple[int, int, int]:
    cmd = [
        str(REPO / "gauntlet-bin"), "-games", str(games), "-time-ms", "100", "-threads", "1",
        "-match-openings", "openings.txt", "-opening-offset", str(offset),
        "-champion", "champion.json", "-ref-champion", "champion.json",
        "-tune", tune_string(plus), "-ref-tune", tune_string(minus),
    ]
    out = subprocess.run(cmd, cwd=REPO, capture_output=True, text=True, check=False).stdout
    return parse_wdl(out)


def main() -> None:
    journal = Path(sys.argv[1])
    iterations = int(sys.argv[2]) if len(sys.argv) > 2 else 300
    games = int(sys.argv[3]) if len(sys.argv) > 3 else 20
    budget_s = 60 * float(sys.argv[4]) if len(sys.argv) > 4 else 25 * 60
    journal.parent.mkdir(parents=True, exist_ok=True)
    start, theta = resume(journal, PARAMS)
    began, last_round = time.monotonic(), 0.0
    for k in range(start, iterations):
        if not has_time_for_another(time.monotonic() - began, last_round, budget_s):
            print(f"stopping at iteration {k}: time budget reached, rerun to resume", flush=True)
            break
        round_start = time.monotonic()
        rng = random.Random(k)
        delta = {name: rng.choice((-1, 1)) for name in theta}
        # Standard SPSA gain schedules, perturbation shrinking slower than the step.
        ck = (1 + k) ** -0.101
        # 0.5: a 20-game result has a spread near 0.2, so one iteration moves a
        # parameter about a tenth of its perturbation and noise cannot walk it far.
        lr = 0.5 * (1 + k / 50) ** -0.602
        plus = {n: theta[n] + ck * PARAMS[n].c * delta[n] for n in theta}
        minus = {n: theta[n] - ck * PARAMS[n].c * delta[n] for n in theta}
        w, d, lo = race(plus, minus, games, offset=110000 + k * games)
        theta = step(theta, PARAMS, delta, w, d, lo, lr)
        with journal.open("a") as f:
            f.write(json.dumps({"iter": k, "wdl": [w, d, lo], "theta": theta}) + "\n")
        print(f"iter {k}: {w}-{d}-{lo}  {tune_string(theta)}", flush=True)
        last_round = time.monotonic() - round_start


if __name__ == "__main__":
    main()

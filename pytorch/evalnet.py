"""Score an already-trained network on a pool, without training it.

Reports the two numbers that matter and are not the same thing: how well it
predicts (held-out error against a constant predictor) and how smoothly it
moves between positions one move apart.

Smoothness is the one that has predicted Elo in this project. Three networks
ranked exactly opposite to their accuracy, and the adopted one was the least
accurate and least jumpy of them. The search prunes on pawn-sized thresholds,
so an evaluation that jumps a pawn between neighbouring positions fires those
thresholds on noise however accurate it is.

Usage:
    .venv/bin/python pytorch/evalnet.py --pool games_d5.bin nets_torch/*.json
"""

import argparse
import json
import sys
from pathlib import Path

import torch

from train import load_pool, pack


def evaluate(net_path: Path, own_t, opp_t, y, games_t, device, batch: int):
    net = json.loads(net_path.read_text())
    h = net["h"]
    inputs = len(net["w1"]) // h
    w1 = torch.tensor(net["w1"], dtype=torch.float32, device=device).view(inputs, h)
    # The padding row the trainer used is dropped on export, so add a zero
    # row back at the same index the packed tensors point at.
    w1 = torch.cat([w1, torch.zeros(1, h, device=device)], dim=0)
    b1 = torch.tensor(net["b1"], dtype=torch.float32, device=device)
    w2 = torch.tensor(net["w2"], dtype=torch.float32, device=device)
    b2 = float(net["b2"])

    preds = []
    with torch.no_grad():
        for s0 in range(0, own_t.shape[0], batch):
            o = own_t[s0 : s0 + batch].long()
            p = opp_t[s0 : s0 + batch].long()
            a = w1[o].sum(dim=1) + b1
            b = w1[p].sum(dim=1) + b1
            acc = torch.cat([a, b], dim=1).clamp(0.0, 1.0)
            preds.append(acc @ w2 + b2)
    pred = torch.cat(preds)

    mse = float(((pred - y) ** 2).mean())
    same = games_t[1:] == games_t[:-1]
    jump = float((pred[1:] - pred[:-1]).abs()[same].mean()) if bool(same.any()) else 0.0
    return mse, jump, h, inputs


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pool", required=True)
    ap.add_argument("--limit", type=int, default=200000)
    ap.add_argument("--batch", type=int, default=16384)
    ap.add_argument("--device", default="mps")
    ap.add_argument("nets", nargs="+")
    args = ap.parse_args()

    device = torch.device(
        args.device if (args.device != "mps" or torch.backends.mps.is_available()) else "cpu"
    )
    own, opp, targets, games = load_pool(Path(args.pool), args.limit)
    y = torch.tensor(targets, dtype=torch.float32, device=device)
    games_t = torch.tensor(games, dtype=torch.long, device=device)
    baseline = float(((y - y.mean()) ** 2).mean())
    print("%s: %d positions, constant predictor %.4f" % (args.pool, len(targets), baseline))
    print("%-42s %9s %9s %8s %8s" % ("network", "MSE", "explains", "jump", "hidden"))

    # Packed once against the widest network, so every net is scored on the
    # identical tensors and the comparison is exact.
    def inputs_of(path):
        """The input count a network file declares, or None if it is not a
        network we can read; such a file is reported as skipped below rather
        than taking the whole comparison down."""
        try:
            net = json.loads(Path(path).read_text())
            return len(net["w1"]) // net["h"]
        except Exception:
            return None

    widths = [w for w in (inputs_of(n) for n in args.nets) if w]
    if not widths:
        sys.exit("none of the networks could be read")
    pad = max(widths)
    own_t = pack(own, pad, device)
    opp_t = pack(opp, pad, device)

    for n in args.nets:
        try:
            mse, jump, h, inputs = evaluate(Path(n), own_t, opp_t, y, games_t, device, args.batch)
        except Exception as exc:  # a net for a different feature set
            print("%-42s  skipped: %s" % (Path(n).name, exc))
            continue
        print("%-42s %9.4f %8.1f%% %8.3f %8d"
              % (Path(n).name, mse, 100 * (1 - mse / baseline), jump, h))


if __name__ == "__main__":  # pragma: no cover
    main()

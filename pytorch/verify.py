"""Check that a PyTorch-trained network evaluates identically in Go.

Training happens here, inference happens in the engine. That split is only
safe if both sides agree on what the weights mean. The first layer is
flattened feature-major, so column f must occupy w1[f*h : f*h+h]; a
transposed or mis-ordered export would train one function and play another
while every training number stayed perfectly healthy. This project has
already paid for one bug of exactly that shape, where the engine read a
win probability as a score and "Black is a rook up" evaluated as +0.245.

Usage:
    ./nnue-bin -emit-eval-check /tmp/evalcheck.json -net-file net.json
    .venv/bin/python pytorch/verify.py --net net.json --cases /tmp/evalcheck.json
"""

import argparse
import json
import sys
from pathlib import Path

import torch


def forward(net, own, opp):
    """The engine's forward pass, in tensors.

    Deliberately written from engine/halfkp.go rather than reusing the
    training module: if both sides shared a bug this check would pass.
    """
    h = net["h"]
    w1 = torch.tensor(net["w1"], dtype=torch.float64).view(-1, h)
    b1 = torch.tensor(net["b1"], dtype=torch.float64)
    w2 = torch.tensor(net["w2"], dtype=torch.float64)
    b2 = float(net["b2"])

    acc_own = b1.clone()
    for f in own:
        acc_own += w1[f]
    acc_opp = b1.clone()
    for f in opp:
        acc_opp += w1[f]

    acc = torch.cat([acc_own, acc_opp]).clamp(0.0, 1.0)
    return float((w2 * acc).sum()) + b2


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--net", required=True)
    ap.add_argument("--cases", required=True)
    ap.add_argument("--tolerance", type=float, default=1e-4)
    args = ap.parse_args()

    net = json.loads(Path(args.net).read_text())
    cases = json.loads(Path(args.cases).read_text())

    worst, failures = 0.0, 0
    for c in cases:
        mine = forward(net, c["own"], c["opp"])
        theirs = c["eval"]
        diff = abs(mine - theirs)
        worst = max(worst, diff)
        flag = ""
        if diff > args.tolerance:
            failures += 1
            flag = "   <- MISMATCH"
        print("%-50s torch %+9.5f   go %+9.5f   diff %.2e%s"
              % (c["fen"][:50], mine, theirs, diff, flag))

    print("\nworst difference %.3e over %d positions" % (worst, len(cases)))
    if failures:
        sys.exit("%d positions disagree: the weights do not mean the same thing "
                 "in both, so the trained network is not the played network" % failures)
    print("the exported network evaluates identically in Go")


if __name__ == "__main__":  # pragma: no cover
    main()

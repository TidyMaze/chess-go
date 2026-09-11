"""Train the HalfKP evaluation network in PyTorch, for inference in Go.

This is the split Stockfish uses: nnue-pytorch trains, the engine infers.
The engine keeps its own forward pass because alpha-beta evaluates one
position at a time and is latency-bound, which is the wrong shape for a
GPU; training is a batched throughput problem, which is the right shape.

It reads the same pool file the Go trainer reads, and writes the same JSON
the Go engine loads, so the two are interchangeable and can be compared
directly. `verify.py` checks that the exported network evaluates
identically under both, which is the contract that makes this safe: a
divergence between the trained function and the played function is the
worst bug available here, and this project has already paid for one.

Nothing external labels the positions. The pool is produced either by
self-play or by replaying a games database, and every target is computed
by the engine's own search.
"""

import argparse
import json
import struct
import sys
import time
from pathlib import Path

import torch
import torch.nn as nn

# Must match engine/halfkp.go: 8 king buckets x 10 piece kinds x 64 squares.
PIECE_KINDS = 10
SQUARES = 64
PER_KING = PIECE_KINDS * SQUARES


def inputs_for(buckets: int) -> int:
    return buckets * PER_KING


def load_pool(path: Path, limit: int = 0):
    """Read the Go pool format.

    Record layout, little endian, from nnue/persist.go:
        int32 game, float32 target, float32 static, uint8 nOwn, uint8 nOpp
        then (nOwn + nOpp) uint16 feature indices, own first.
    """
    data = memoryview(path.read_bytes())
    pos, n = 0, len(data)
    own_idx, opp_idx, targets, games = [], [], [], []
    head = struct.Struct("<iffBB")  # game, target, static, nOwn, nOpp
    assert head.size == 14
    unpack_head = head.unpack_from
    while pos + 14 <= n:
        game, target, _static, n_own, n_opp = unpack_head(data, pos)
        pos += 14
        total = n_own + n_opp
        need = 2 * total
        if pos + need > n:
            break  # a truncated tail is a crash mid-write, not an error
        feats = struct.unpack_from("<%dH" % total, data, pos)
        pos += need
        own_idx.append(feats[:n_own])
        opp_idx.append(feats[n_own:])
        targets.append(target)
        games.append(game)
        if limit and len(targets) >= limit:
            break
    return own_idx, opp_idx, targets, games


def neighbour_index(inputs: int, device):
    """For every feature, the indices of the same piece on adjacent squares.

    A HalfKP feature is (king slot, piece kind, square) and the square is the
    low 6 bits, so neighbours share everything above them and differ only in
    file and rank. Returns an [inputs, 8] index tensor and a matching mask,
    with off-board neighbours pointing at the feature itself and masked out.
    """
    idx = torch.zeros(inputs, 8, dtype=torch.long)
    mask = torch.zeros(inputs, 8, dtype=torch.float32)
    for feat in range(inputs):
        sq = feat % 64
        base = feat - sq
        file, rank = sq % 8, sq // 8
        k = 0
        for df in (-1, 0, 1):
            for dr in (-1, 0, 1):
                if df == 0 and dr == 0:
                    continue
                f, r = file + df, rank + dr
                if 0 <= f < 8 and 0 <= r < 8:
                    idx[feat, k] = base + r * 8 + f
                    mask[feat, k] = 1.0
                else:
                    idx[feat, k] = feat
                k += 1
    return idx.to(device), mask.to(device)


def smooth_(weight, idx, mask, alpha: float):
    """Pull each square's weights toward its neighbours', in place.

    This is a prior, not a constraint: a knight on e4 and one on e5 are worth
    nearly the same, and the exceptions (a pawn on the seventh) stay learnable
    because the pull is partial. It attacks the measured blocker directly.
    Across every network this project has raced, Elo has tracked how much the
    evaluation moves between two positions one move apart, and has not tracked
    accuracy at all: the three most accurate networks were the three jumpiest
    and all lost. The jumpiness comes from adjacent squares learning
    independent weights, so a piece stepping one square swaps in an unrelated
    column.
    """
    if alpha <= 0:
        return
    with torch.no_grad():
        w = weight[: idx.shape[0]]
        neigh = w[idx]                      # [inputs, 8, h]
        m = mask.unsqueeze(-1)              # [inputs, 8, 1]
        count = m.sum(dim=1).clamp(min=1.0)
        avg = (neigh * m).sum(dim=1) / count
        w.mul_(1 - alpha).add_(avg, alpha=alpha)


def pack(index_lists, pad_index, device):
    """Pack variable-length feature lists into one dense [N, K] tensor.

    Built once for the whole pool and kept on the device, so a batch is a
    GPU gather rather than Python work. The first version assembled index
    tensors per batch in Python and that was the bottleneck: 49,535
    positions per second with the CPU busy against 360,330 with it idle,
    because feeding the GPU competed with the matches and the data
    generation for the same cores.

    Padding uses one extra embedding row, held at zero and excluded from
    gradients, so a short list contributes nothing.
    """
    width = max((len(l) for l in index_lists), default=1)
    n = len(index_lists)
    out = torch.full((n, width), pad_index, dtype=torch.int32)
    for i, lst in enumerate(index_lists):
        if lst:
            out[i, : len(lst)] = torch.tensor(lst, dtype=torch.int32)
    return out.to(device)


class HalfKP(nn.Module):
    """5120 -> h (shared, applied to both perspectives) -> 2h -> 1.

    The first layer is one embedding table summed over the ~30 active
    features, which is exactly what the Go accumulator does by adding one
    weight column per piece.
    """

    def __init__(self, hidden: int, buckets: int):
        super().__init__()
        self.hidden = hidden
        self.buckets = buckets
        self.inputs = inputs_for(buckets)
        # One extra row is the padding slot, pinned at zero and excluded
        # from gradients so short feature lists contribute nothing.
        self.embed = nn.Embedding(self.inputs + 1, hidden, padding_idx=self.inputs)
        self.b1 = nn.Parameter(torch.full((hidden,), 0.5))
        self.out = nn.Linear(2 * hidden, 1)
        with torch.no_grad():
            nn.init.normal_(self.embed.weight, std=0.02)
            self.embed.weight[self.inputs].zero_()
        nn.init.normal_(self.out.weight, std=0.4)
        nn.init.zeros_(self.out.bias)

    def forward(self, own, opp):
        a = self.embed(own).sum(dim=1) + self.b1
        b = self.embed(opp).sum(dim=1) + self.b1
        # Clipped ReLU, the same activation the engine applies.
        acc = torch.cat([a, b], dim=1).clamp(0.0, 1.0)
        return self.out(acc).squeeze(1)


def export(model: HalfKP, path: Path):
    """Write the JSON engine/halfkp.go reads.

    w1 is flattened feature-major so that column f occupies
    w1[f*h : f*h+h], which is how the Go accumulator indexes it.
    """
    # Drop the padding row: the engine has no such feature.
    w1 = model.embed.weight.detach()[: model.inputs].cpu().contiguous().view(-1).tolist()
    net = {
        "h": model.hidden,
        "w1": w1,
        "b1": model.b1.detach().cpu().tolist(),
        "w2": model.out.weight.detach().cpu().view(-1).tolist(),
        "b2": float(model.out.bias.detach().cpu().item()),
        "scale": 1.0,
        "sigmoid": False,
        "k": 0.30,
        "buckets": model.buckets,
    }
    path.write_text(json.dumps(net))
    return net


def average_states(states):
    """Elementwise mean of several state dicts.

    Two networks from the identical recipe, differing only in weight
    initialisation and batch order, measured 18 +/- 16 Elo apart over 1750
    games on two independent sets of openings. The spread is real playing
    strength and it is wider than any gain a ladder rung has ever shown, so
    a rung is mostly a draw from that distribution. Averaging the weights of
    the best epochs takes the middle of it instead, and costs nothing when
    the network is evaluated.
    """
    return {k: sum(s[k].float() for s in states) / len(states) for k in states[0]}


def pick_weights(averaged_state, averaged_loss, best_state, best_loss):
    """Keep averaged weights only when they hold out better than the best
    single epoch, and say which was kept: an average that is worse must not
    be shipped silently."""
    if averaged_loss < best_loss:
        return averaged_state, averaged_loss, "keeping it"
    return best_state, best_loss, "keeping the single best epoch"



def load_net(model: HalfKP, path: Path) -> None:
    """Load an exported network back into a model, the inverse of export.

    A rung that starts from random weights has to rediscover everything the
    rung below it already knew, and rediscovers it slightly differently,
    because a network explains about 91% of its teacher whatever you do.
    Starting from the previous rung keeps what works and moves only where the
    new labels disagree.
    """
    net = json.loads(Path(path).read_text())
    if net["h"] != model.hidden or net.get("buckets", 8) != model.buckets:
        raise ValueError(
            "network is %d hidden and %d buckets, the model is %d and %d"
            % (net["h"], net.get("buckets", 8), model.hidden, model.buckets))
    with torch.no_grad():
        w1 = torch.tensor(net["w1"], dtype=torch.float32).view(model.inputs, model.hidden)
        model.embed.weight[: model.inputs].copy_(w1)
        model.embed.weight[model.inputs].zero_()
        model.b1.copy_(torch.tensor(net["b1"], dtype=torch.float32))
        model.out.weight.copy_(
            torch.tensor(net["w2"], dtype=torch.float32).view(1, 2 * model.hidden))
        model.out.bias.fill_(float(net["b2"]))

def counts_as_improvement(test: float, best: float, min_delta: float, baseline: float) -> bool:
    """Whether a held-out loss has improved enough to count.

    The threshold is a share of the constant-predictor loss rather than an
    absolute number, because how big the loss is depends on which loss it
    is: on the same positions, squared error in pawns and win probability
    are a few hundred apart, and one absolute threshold would stop one run
    far sooner than the other for no reason to do with the networks.
    """
    return test < best - min_delta * baseline


def win_prob_error(pred, target, k: float):
    """Squared error in win-probability space, elementwise.

    Squared error in pawns treats a pawn of error in a position worth +8
    exactly like a pawn of error at equality, and only one of them can
    change a move. Search makes it worse by taking maxima over noisy
    leaves, so the error that matters is the one near zero. Passing both
    sides through a sigmoid first is what Stockfish trains on.

    The network still emits pawns; only the comparison moves. A network
    that emitted probabilities would have to be inverted by the search,
    and inverting amplifies: 0.11 at p=0.95 is four pawns.
    """
    return (torch.sigmoid(k * pred) - torch.sigmoid(k * target)) ** 2


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pool", required=True, nargs="+",
                    help="one or more pool files; game ids are offset per file so the "
                         "by-game split cannot pair a self-play game with a real one")
    ap.add_argument("--out", default="pytorch_net.json")
    ap.add_argument("--hidden", type=int, default=32)
    ap.add_argument("--buckets", type=int, default=8)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--epochs", type=int, default=30)
    ap.add_argument("--batch", type=int, default=8192)
    ap.add_argument("--lr", type=float, default=0.001)
    ap.add_argument("--holdout-games", type=float, default=0.15)
    ap.add_argument("--device", default="mps")
    ap.add_argument("--patience", type=int, default=8,
                    help="stop when the held-out loss has not improved for this many epochs")
    ap.add_argument("--min-delta", type=float, default=1e-5,
                    help="an improvement counts when it clears this fraction of the "
                         "constant-predictor loss. A fraction rather than an absolute "
                         "number because the size of the loss depends on the loss: on the "
                         "same 7.5M positions, squared error in pawns starts at 15.099 and "
                         "win probability at 0.0446, so one absolute threshold is 340 times "
                         "stricter than the other and stops training that much sooner.")
    ap.add_argument("--status", default="nnue_status.json",
                    help="status file the browser UI polls; empty to disable")
    ap.add_argument("--label", default="", help="name shown in the UI")
    ap.add_argument("--checkpoint", default="",
                    help="resume from and save to this file; defaults to <out>.ckpt")
    ap.add_argument("--checkpoint-every", type=int, default=10,
                    help="also checkpoint every N epochs, not only on improvement")
    ap.add_argument("--fresh", action="store_true", help="ignore any checkpoint")
    ap.add_argument("--init-from", default="",
                    help="start from this exported network instead of random weights, so a "
                         "rung keeps what the rung below it learned and moves only where the "
                         "new labels disagree")
    ap.add_argument("--average-best", type=int, default=1,
                    help="average the weights of this many best epochs, and keep the "
                         "average only when it holds out better than the single best")
    ap.add_argument("--loss", choices=("mse", "sigmoid"), default="mse",
                    help="mse compares pawns; sigmoid compares win probabilities, which "
                         "stops the network spending its capacity on positions that are "
                         "already decided")
    ap.add_argument("--k", type=float, default=0.3,
                    help="steepness of the sigmoid used by --loss sigmoid")
    ap.add_argument("--smooth", type=float, default=0.0,
                    help="after each epoch, pull every square's weights this far toward its "
                         "neighbours'. Targets jumpiness, which is what predicts Elo here; "
                         "accuracy does not. The Go trainer used 0.05.")
    ap.add_argument("--lr-decay", type=float, default=0.0,
                    help="halve the learning rate after this many epochs without improvement "
                         "(0 disables). A plateau can be the optimiser stalling rather than the "
                         "data running out, and the two look identical from the loss alone.")
    args = ap.parse_args()

    def log(msg):
        print("%s  %s" % (time.strftime("%H:%M:%S"), msg), flush=True)

    device = torch.device(
        args.device if (args.device != "mps" or torch.backends.mps.is_available()) else "cpu"
    )
    log("device %s, torch %s" % (device, torch.__version__))

    t0 = time.time()
    own, opp, targets, games = [], [], [], []
    offset = 0
    for path in args.pool:
        remaining = 0 if not args.limit else max(0, args.limit - len(targets))
        if args.limit and remaining == 0:
            break
        o, p_, t_, g_ = load_pool(Path(path), remaining)
        # Game ids restart from a low number in every pool, so without an
        # offset a self-play game and a real game share an id and the
        # by-game split puts one in training and the other in held-out
        # believing they are the same game.
        if g_:
            own.extend(o)
            opp.extend(p_)
            targets.extend(t_)
            games.extend(x + offset for x in g_)
            offset += max(g_) + 1
        log("  %s: %d positions" % (path, len(t_)))
    log("loaded %d positions from %d pool(s) in %.0fs"
        % (len(targets), len(args.pool), time.time() - t0))
    if not targets:
        sys.exit("pool is empty")

    # Split by game, never by position. Consecutive positions in a game
    # differ by one move, so a position split leaks near-copies into the
    # held-out set: the same network once measured 1.36 held-out against a
    # hand-written evaluation's 5.89 and lost 60 games out of 60.
    uniq = sorted(set(games))
    g = torch.Generator().manual_seed(23)
    perm = torch.randperm(len(uniq), generator=g).tolist()
    n_hold = max(1, int(len(uniq) * args.holdout_games))
    held = {uniq[i] for i in perm[:n_hold]}
    tr = [i for i, gi in enumerate(games) if gi not in held]
    te = [i for i, gi in enumerate(games) if gi in held]
    log("%d training positions, %d held out over %d games" % (len(tr), len(te), len(uniq)))

    arch = {
        "features": "HalfKP (%d king slots x piece x square)" % args.buckets,
        "inputs": inputs_for(args.buckets),
        "hidden": args.hidden,
        "layers": "%d -> %d (shared, both perspectives) -> %d -> 1"
                  % (inputs_for(args.buckets), args.hidden, 2 * args.hidden),
        "activation": "clipped ReLU [0,1]",
        "params": inputs_for(args.buckets) * args.hidden + args.hidden + 2 * args.hidden + 1,
        "trainer": "PyTorch %s on %s" % (torch.__version__, device),
        "optimiser": "Adam lr %g" % args.lr,
        "patience": args.patience,
    }

    def write_status(**extra):
        """The browser polls this file. Without it a PyTorch run shows as
        'training is not running', because the UI cannot tell a stopped
        process from one that never writes. The `at` field is what
        separates slow from dead."""
        if not args.status:
            return
        st = {"phase": "training", "at": int(time.time()), "arch": arch,
              "pool": len(targets), "generation": 1, "generations": 1,
              "label": args.label or ("PyTorch, %d hidden" % args.hidden)}
        st.update(extra)
        Path(args.status).write_text(json.dumps(st))

    model = HalfKP(args.hidden, args.buckets).to(device)
    if args.init_from:
        load_net(model, Path(args.init_from))
        model.to(device)
        log("starting from %s" % args.init_from)
    smooth_idx = smooth_mask = None
    if args.smooth > 0:
        smooth_idx, smooth_mask = neighbour_index(model.inputs, device)
    # Packed once, kept on the device. This is what lets the GPU run at
    # full rate while every CPU core is busy with matches and self-play.
    t_pack = time.time()
    own_t = pack(own, model.inputs, device)
    opp_t = pack(opp, model.inputs, device)
    del own, opp
    log("packed features onto %s in %.0fs, %s and %s"
        % (device, time.time() - t_pack, tuple(own_t.shape), tuple(opp_t.shape)))
    opt = torch.optim.Adam(model.parameters(), lr=args.lr)
    sched = None
    if args.lr_decay > 0:
        sched = torch.optim.lr_scheduler.ReduceLROnPlateau(
            opt, mode="min", factor=0.5, patience=int(args.lr_decay))
    # Everything that compares a prediction with a target goes through the
    # same space, so the held-out number and "explains" stay consistent
    # within a run. They are not comparable across losses; Elo is.
    if args.loss == "sigmoid":
        def space(t):
            return torch.sigmoid(args.k * t)
    else:
        def space(t):
            return t

    def lossf(pred, target):
        return ((space(pred) - space(target)) ** 2).mean()

    y = torch.tensor(targets, dtype=torch.float32, device=device)
    # The only honest reference for "is it learning": a model that cannot
    # beat the mean of its own targets has learned nothing, whatever else
    # it beats.
    baseline = float(((space(y[te]) - space(y[te]).mean()) ** 2).mean())
    log("constant-predictor held-out MSE %.4f" % baseline)

    # Checkpointing. A run told to train until it plateaus has no fixed
    # end, so "stopped" and "finished" look the same from outside and an
    # interrupted run must cost minutes rather than everything. The
    # optimiser state goes in too: Adam's second moment records how far
    # each weight has already been tuned, and dropping it restarts every
    # per-weight step size from scratch.
    ckpt_path = Path(args.checkpoint or (args.out + ".ckpt"))
    start_epoch = 1
    if ckpt_path.exists() and not args.fresh:
        # A checkpoint that will not load is ignored the same way one of
        # the wrong shape is: the run starts fresh and says so, rather than
        # dying on a file it was only ever meant to speed things up with.
        try:
            ck = torch.load(ckpt_path, map_location=device, weights_only=False)
        except Exception as exc:
            log("ignoring %s: it does not load (%s)" % (ckpt_path, exc))
            ck = {}
        if ck.get("hidden") == args.hidden and ck.get("buckets") == args.buckets:
            try:
                model.load_state_dict(ck["model"])
                opt.load_state_dict(ck["opt"])
                start_epoch = ck["epoch"] + 1
                log("resumed from %s at epoch %d (best held out %.4f at epoch %d)"
                    % (ckpt_path, ck["epoch"], ck["best"], ck["best_epoch"]))
            except Exception as exc:
                log("ignoring %s: it does not restore (%s)" % (ckpt_path, exc))
                start_epoch = 1
        else:
            # A checkpoint from another architecture would load weights
            # that mean something else, and the run would look healthy
            # while learning nothing.
            log("ignoring %s: it holds %s hidden units at %s buckets, this run wants %d at %d"
                % (ckpt_path, ck.get("hidden"), ck.get("buckets"), args.hidden, args.buckets))

    def save_checkpoint(epoch, best, best_epoch, best_state):
        tmp = ckpt_path.with_suffix(ckpt_path.suffix + ".tmp")
        torch.save({"model": model.state_dict(), "opt": opt.state_dict(),
                    "epoch": epoch, "best": best, "best_epoch": best_epoch,
                    "best_state": best_state,
                    "hidden": args.hidden, "buckets": args.buckets}, tmp)
        # Renamed into place so a kill during the write cannot leave a
        # half-written checkpoint where a good one used to be.
        tmp.replace(ckpt_path)

    # Game ids on the device, so smoothness can be computed without leaving it.
    games_t = torch.tensor(games, dtype=torch.long, device=device)
    tr_t = torch.tensor(tr, dtype=torch.long, device=device)
    te_t = torch.tensor(te, dtype=torch.long, device=device)

    def batches(idx_t, size, shuffle):
        if shuffle:
            idx_t = idx_t[torch.randperm(idx_t.numel(), device=idx_t.device)]
        for s0 in range(0, idx_t.numel(), size):
            yield idx_t[s0 : s0 + size]

    def smoothness(idx):
        """How much the evaluation moves between two positions one move apart.

        This is the quantity that has predicted Elo throughout this project,
        and accuracy has not: three networks ranked exactly opposite to their
        held-out loss, and the one that was adopted was the least accurate and
        the least jumpy. The search prunes on pawn-sized thresholds
        (aspiration window 0.5, futility margins 1 to 3), so an evaluation
        that jumps a pawn between neighbouring positions fires them on noise.

        Consecutive entries in a pool are consecutive positions in a game, so
        a pair is only counted when both sides share a game id.
        """
        model.eval()
        total, pairs = 0.0, 0
        with torch.no_grad():
            for b in batches(idx, args.batch, False):
                pred = model(own_t[b], opp_t[b])
                gid = games_t[b]
                same = gid[1:] == gid[:-1]
                if same.any():
                    d = (pred[1:] - pred[:-1]).abs()[same]
                    total += float(d.sum())
                    pairs += int(same.sum())
        return total / max(pairs, 1)

    def evaluate(idx):
        model.eval()
        total, n = 0.0, 0
        with torch.no_grad():
            for b in batches(idx, args.batch, False):
                pred = model(own_t[b], opp_t[b])
                total += float(((space(pred) - space(y[b])) ** 2).sum())
                n += b.numel()
        return total / max(n, 1)

    best, best_epoch, best_state = float("inf"), 0, None
    # The best few epochs by held-out loss, newest first on ties.
    top: list = []
    if ckpt_path.exists() and not args.fresh:
        try:
            ck = torch.load(ckpt_path, map_location=device, weights_only=False)
            if ck.get("hidden") == args.hidden:
                best, best_epoch = ck["best"], ck["best_epoch"]
                best_state = ck.get("best_state")
        except Exception:
            pass

    # epochs 0 means run until the held-out loss stops improving.
    epoch = start_epoch - 1
    limit = args.epochs if args.epochs > 0 else 10**9
    while epoch < start_epoch - 1 + limit:
        epoch += 1
        model.train()
        est, seen = time.time(), 0
        for b in batches(tr_t, args.batch, True):
            pred = model(own_t[b], opp_t[b])
            loss = lossf(pred, y[b])
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            seen += b.numel()
        if smooth_idx is not None:
            smooth_(model.embed.weight, smooth_idx, smooth_mask, args.smooth)
        test = evaluate(te_t)
        jump = smoothness(te_t)
        explained = 100 * (1 - test / baseline) if baseline > 0 else 0.0
        rate = seen / max(time.time() - est, 1e-6)

        improved = counts_as_improvement(test, best, args.min_delta, baseline)
        if improved:
            best, best_epoch = test, epoch
            # Keep the best weights, not the last: past the plateau the
            # held-out loss drifts back up, and the final epoch is then
            # worse than one seen twenty epochs earlier.
            best_state = {k: v.detach().clone() for k, v in model.state_dict().items()}
        if args.average_best > 1:
            top.append((test, {k: v.detach().clone() for k, v in model.state_dict().items()}))
            top.sort(key=lambda kept: kept[0])
            del top[args.average_best:]
        stale = epoch - best_epoch

        where = "epoch %d/%d" % (epoch, args.epochs) if args.epochs > 0 \
            else "epoch %d (until plateau)" % epoch
        log("%-26s held out %.4f  constant %.4f  explains %.1f%%  jump %.3f  %.0f pos/s%s"
            % (where, test, baseline, explained, jump, rate,
               "" if improved else "  (no improvement for %d)" % stale))
        if improved or epoch % max(args.checkpoint_every, 1) == 0:
            save_checkpoint(epoch, best, best_epoch, best_state)
        write_status(epoch=epoch, epochs=args.epochs, test_loss=test,
                     explains=explained, smoothness=jump, positions_per_sec=rate,
                     epoch_positions=seen, epoch_total=len(tr),
                     epoch_eta_sec=0, best_epoch=best_epoch, stale_epochs=stale)

        if sched is not None:
            sched.step(test)
        if args.patience > 0 and stale >= args.patience:
            log("stopping early: no improvement for %d epochs, best was %.4f at epoch %d"
                % (stale, best, best_epoch))
            break

    save_checkpoint(epoch, best, best_epoch, best_state)
    if best_state is not None:
        model.load_state_dict(best_state)
        log("restored the best weights, from epoch %d (held out %.4f, explains %.1f%%)"
            % (best_epoch, best, 100 * (1 - best / baseline)))
    if len(top) > 1:
        averaged_state = average_states([state for _, state in top])
        model.load_state_dict(averaged_state)
        averaged = evaluate(te_t)
        chosen, best, kept = pick_weights(averaged_state, averaged, best_state, best)
        log("averaged the best %d epochs: held out %.4f against %.4f, %s"
            % (len(top), averaged, best, kept))
        if chosen is not None:
            model.load_state_dict(chosen)

    write_status(phase="done", epoch=best_epoch, epochs=args.epochs,
                 test_loss=best, explains=100 * (1 - best / baseline))

    export(model, Path(args.out))
    log("wrote %s" % args.out)


if __name__ == "__main__":  # pragma: no cover
    main()

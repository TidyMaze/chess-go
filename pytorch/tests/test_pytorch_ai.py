"""Tests for the PyTorch side of the AI: the trainer, the exporter, the
Go-agreement check and the network comparison tool. Everything runs on
the CPU on a pool small enough to build by hand in the Go pool layout."""
import json
import os
import struct
import subprocess
import sys
from pathlib import Path

import pytest
import torch

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import train  # noqa: E402
import verify  # noqa: E402
import evalnet  # noqa: E402

REPO = HERE.parent.parent


def write_pool(path: Path, games=6, per_game=8, seed=1):
    """The Go layout: game int32, target f32, static f32, nOwn u8, nOpp u8,
    then uint16 features for own then opp."""
    g = torch.Generator().manual_seed(seed)
    inputs = train.inputs_for(8)
    with open(path, "wb") as f:
        for game in range(games):
            for _ in range(per_game):
                n_own = int(torch.randint(4, 12, (1,), generator=g))
                n_opp = int(torch.randint(4, 12, (1,), generator=g))
                own = torch.randint(0, inputs, (n_own,), generator=g).tolist()
                opp = torch.randint(0, inputs, (n_opp,), generator=g).tolist()
                target = float(torch.randn(1, generator=g)) * 0.5
                f.write(struct.pack("<iffBB", game, target, target * 0.5, n_own, n_opp))
                f.write(struct.pack("<%dH" % (n_own + n_opp), *(own + opp)))
    return games * per_game


def test_inputs_for_matches_go():
    assert train.inputs_for(8) == 5120
    assert train.inputs_for(32) == 20480


def test_load_pool_reads_the_go_layout(tmp_path):
    pool = tmp_path / "pool.bin"
    n = write_pool(pool)
    own, opp, y, games = train.load_pool(pool)
    assert len(own) == n and len(opp) == n and len(y) == n and len(games) == n
    own2, *_ = train.load_pool(pool, limit=5)
    assert len(own2) == 5
    # A truncated tail is a crash mid-write, not an error: reading stops.
    data = pool.read_bytes()
    cut = tmp_path / "cut.bin"
    cut.write_bytes(data[:-7])
    own3, *_ = train.load_pool(cut)
    assert len(own3) == n - 1


def test_neighbour_smoothing_pulls_toward_neighbours():
    inputs = train.inputs_for(8)
    idx, mask = train.neighbour_index(inputs, "cpu")
    assert idx.shape[0] == inputs and mask.shape[0] == inputs
    w = torch.zeros(inputs, 4)
    w[0] = 10.0  # one hot square, neighbours at zero
    before = w.clone()
    train.smooth_(w, idx, mask, alpha=0.0)
    assert torch.equal(w, before)  # alpha 0 is a no-op
    train.smooth_(w, idx, mask, alpha=0.5)
    assert w[0].sum() < before[0].sum()  # pulled toward its zero neighbours


def test_pack_and_forward_and_export_round_trip(tmp_path):
    inputs = train.inputs_for(8)
    lists = [[1, 2, 3], [4], [5, 6, 7, 8, 9]]
    packed = train.pack(lists, inputs, "cpu")
    assert packed.shape == (3, 5)
    model = train.HalfKP(hidden=4, buckets=8)
    own = train.pack([[1, 2], [3]], inputs, "cpu")
    opp = train.pack([[4], [5, 6]], inputs, "cpu")
    out = model(own, opp)
    assert out.shape[0] == 2 and torch.isfinite(out).all()
    path = tmp_path / "net.json"
    train.export(model, path)
    net = json.loads(path.read_text())
    assert net["h"] == 4 and len(net["b1"]) == 4 and len(net["w2"]) == 8
    # verify.forward recomputes the same number from the JSON weights.
    torch_val = float(out[0])
    check = verify.forward(net, [1, 2], [4])
    assert abs(check - torch_val) < 1e-4


def run_main(module, argv):
    old = sys.argv
    sys.argv = [module.__name__] + argv
    try:
        module.main()
    finally:
        sys.argv = old


def test_trainer_end_to_end_with_checkpoint_resume_and_plateau(tmp_path):
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=8, per_game=10)
    out, status, ckpt = tmp_path / "net.json", tmp_path / "status.json", tmp_path / "net.ckpt"
    common = ["--pool", str(pool), "--out", str(out), "--status", str(status), "--device", "cpu",
              "--hidden", "4", "--batch", "16", "--holdout-games", "0.25", "--checkpoint", str(ckpt),
              "--checkpoint-every", "1", "--smooth", "0.5", "--lr-decay", "1", "--label", "test"]
    run_main(train, common + ["--epochs", "2"])
    assert out.exists() and status.exists() and ckpt.exists()
    st = json.loads(status.read_text())
    assert st.get("label") == "test"
    # Resume from the checkpoint, then run until the plateau with patience 1.
    run_main(train, common + ["--epochs", "3"])
    run_main(train, common + ["--epochs", "0", "--patience", "1"])
    # And ignore it.
    run_main(train, common + ["--epochs", "1", "--fresh"])
    # Two pools with game-id offsets.
    pool2 = tmp_path / "pool2.bin"
    write_pool(pool2, games=3, per_game=6, seed=2)
    run_main(train, common[:1] + [str(pool), str(pool2)] + common[2:] + ["--epochs", "1", "--limit", "40"])


@pytest.mark.skipif(not (REPO / "nnue-bin").exists(), reason="no Go binary")
def test_exported_net_agrees_with_go(tmp_path):
    pool = tmp_path / "pool.bin"
    write_pool(pool)
    out = tmp_path / "net.json"
    run_main(train, ["--pool", str(pool), "--out", str(out), "--status", str(tmp_path / "s.json"),
                     "--device", "cpu", "--hidden", "4", "--epochs", "1", "--batch", "16"])
    cases = tmp_path / "cases.json"
    subprocess.run([str(REPO / "nnue-bin"), "-emit-eval-check", str(cases), "-net-file", str(out)],
                   check=True, cwd=REPO, capture_output=True)
    run_main(verify, ["--net", str(out), "--cases", str(cases)])
    # A wrong net against the same cases must disagree and exit non-zero.
    bad = json.loads(out.read_text())
    bad["b2"] = bad["b2"] + 5.0
    badpath = tmp_path / "bad.json"
    badpath.write_text(json.dumps(bad))
    with pytest.raises(SystemExit):
        run_main(verify, ["--net", str(badpath), "--cases", str(cases)])
    # evalnet compares networks on the pool.
    run_main(evalnet, ["--pool", str(pool), "--device", "cpu", "--batch", "16", "--limit", "100", str(out), str(badpath)])


def test_trainer_edge_cases(tmp_path):
    empty = tmp_path / "empty.bin"
    empty.write_bytes(b"")
    with pytest.raises(SystemExit):
        run_main(train, ["--pool", str(empty), "--device", "cpu", "--epochs", "1", "--out", str(tmp_path / "n.json")])
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=4, per_game=6)
    out, ckpt = tmp_path / "net.json", tmp_path / "net.ckpt"
    base = ["--pool", str(pool), "--out", str(out), "--device", "cpu", "--batch", "8", "--epochs", "1",
            "--checkpoint", str(ckpt), "--checkpoint-every", "1"]
    # No status file at all.
    run_main(train, base + ["--hidden", "4", "--status", ""])
    # A checkpoint from another width is ignored, not loaded.
    run_main(train, base + ["--hidden", "8", "--status", str(tmp_path / "s.json")])
    # A corrupt checkpoint is ignored too.
    ckpt.write_bytes(b"not a checkpoint")
    run_main(train, base + ["--hidden", "8", "--status", str(tmp_path / "s.json")])
    # A checkpoint of the right width but missing the best-so-far record:
    # the shape check passes, the restore of the record is skipped.
    torch.save({"hidden": 8, "buckets": 8}, ckpt)
    run_main(train, base + ["--hidden", "8", "--status", str(tmp_path / "s.json")])


def test_evalnet_skips_a_net_of_another_feature_set(tmp_path):
    pool = tmp_path / "pool.bin"
    write_pool(pool)
    other = tmp_path / "other.json"
    other.write_text(json.dumps({"h": 4, "b1": [0, 0, 0, 0]}))  # no first-layer weights at all
    good = tmp_path / "good.json"
    train.export(train.HalfKP(hidden=4, buckets=8), good)
    run_main(evalnet, ["--pool", str(pool), "--device", "cpu", "--batch", "16", "--limit", "50", str(good), str(other)])
    with pytest.raises(SystemExit):
        run_main(evalnet, ["--pool", str(pool), "--device", "cpu", "--limit", "50", str(other)])


def test_sigmoid_loss_weighs_close_positions_far_above_decided_ones():
    """Squared error in pawns spends the network's capacity where it cannot
    matter. A pawn of error in a position worth +8 changes nothing: that side
    is winning either way. The same pawn near equality decides which move is
    played, and search amplifies it by taking maxima over noisy leaves.

    The Go trainer measured this idea correct in principle and dropped it
    because its own hand-written optimiser could not follow a gradient about
    twenty times smaller. Adam divides each step by that gradient's own
    running magnitude, so the objection does not carry over."""
    k = 0.3
    pred = torch.tensor([1.0, 9.0])
    target = torch.tensor([0.0, 8.0])
    err = train.win_prob_error(pred, target, k)
    assert float(err[0]) > 5 * float(err[1])
    # The same two errors are indistinguishable to plain squared error.
    plain = (pred - target) ** 2
    assert float(plain[0]) == pytest.approx(float(plain[1]))
    # And it stays a proper distance: zero exactly when the prediction is.
    assert float(train.win_prob_error(target, target, k).sum()) == pytest.approx(0.0)


def test_trainer_optimises_win_probability_but_still_exports_pawns(tmp_path):
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=8, per_game=10)
    out, status = tmp_path / "net.json", tmp_path / "status.json"
    run_main(train, ["--pool", str(pool), "--out", str(out), "--status", str(status),
                     "--device", "cpu", "--hidden", "4", "--batch", "16",
                     "--holdout-games", "0.25", "--epochs", "2",
                     "--loss", "sigmoid", "--k", "0.3"])
    net = json.loads(out.read_text())
    # Only the loss moves to probability space. A network that emitted
    # probabilities would have to be inverted by the search, and inverting
    # amplifies: 0.11 at p=0.95 is four pawns.
    assert net["sigmoid"] is False
    assert net["h"] == 4



def test_an_improvement_counts_by_its_share_of_the_loss_not_its_size():
    """Stopping on a fixed absolute improvement stops sooner the smaller the
    loss happens to be, so the threshold, not the recipe, decides how long a
    network trains. Measured on the same 7.5M positions: squared error in
    pawns starts at 15.099 and win probability at 0.0446, 340 apart, and one
    --min-delta made the second run stop at epoch 24 against the first's 62.

    The rule has to see the same improvement in both, so it must be
    invariant when the loss and the baseline are scaled together."""
    cases = [(1.4872, 1.4880), (1.4872, 1.4873), (1.4872, 1.4872)]
    for test, best in cases:
        want = train.counts_as_improvement(test, best, 1e-5, 15.099)
        for c in (0.0029, 340.0):
            got = train.counts_as_improvement(test * c, best * c, 1e-5, 15.099 * c)
            assert got == want, (
                "an improvement from %.6f to %.6f counts as %s, but the same improvement "
                "on a loss %.4g times the size counts as %s" % (best, test, want, c, got))
    # It is still an improvement test: a loss that went up never counts, and
    # one that beat the threshold always does.
    assert train.counts_as_improvement(1.0, 2.0, 1e-5, 15.099)
    assert not train.counts_as_improvement(2.0, 1.0, 1e-5, 15.099)


def test_average_states_is_the_elementwise_mean():
    """Two networks from the identical recipe, differing only in weight
    initialisation and batch order, measured 18 +/- 16 Elo apart over 1750
    games on two independent sets of openings. That spread is real playing
    strength, not the ruler, and it is larger than anything the ladder has
    ever gained in a rung. Averaging the best epochs' weights is the cheap
    way to take the average of that distribution instead of a draw from it,
    and it costs nothing at evaluation time."""
    a = {"w": torch.tensor([0.0, 2.0]), "b": torch.tensor([4.0])}
    b = {"w": torch.tensor([2.0, 4.0]), "b": torch.tensor([0.0])}
    m = train.average_states([a, b])
    assert torch.allclose(m["w"], torch.tensor([1.0, 3.0]))
    assert torch.allclose(m["b"], torch.tensor([2.0]))
    # A single state averages to itself, which is what --average-best 1 means.
    assert torch.allclose(train.average_states([a])["w"], a["w"])


def test_trainer_averages_the_best_epochs_and_keeps_whichever_holds_out_better(tmp_path, capsys):
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=10, per_game=10)
    out = tmp_path / "net.json"
    run_main(train, ["--pool", str(pool), "--out", str(out), "--status", str(tmp_path / "s.json"),
                     "--device", "cpu", "--hidden", "4", "--batch", "16",
                     "--holdout-games", "0.3", "--epochs", "4", "--average-best", "3"])
    said = capsys.readouterr().out
    assert out.exists()
    assert "averaged the best" in said

    # And an average that holds out worse than the single best epoch must be
    # dropped rather than shipped silently. A large learning rate moves the
    # weights far enough between epochs that their mean is worse than any of
    # them, which is exactly the case the fallback exists for.
    run_main(train, ["--pool", str(pool), "--out", str(out), "--status", str(tmp_path / "s.json"),
                     "--device", "cpu", "--hidden", "4", "--batch", "16",
                     "--holdout-games", "0.3", "--epochs", "6", "--average-best", "3",
                     "--lr", "0.5", "--fresh"])
    assert "keeping the single best epoch" in capsys.readouterr().out


def test_averaged_weights_are_kept_only_when_they_hold_out_better():
    avg, single = {"w": torch.zeros(1)}, {"w": torch.ones(1)}
    chosen, loss, kept = train.pick_weights(avg, 0.5, single, 0.6)
    assert chosen is avg and loss == 0.5 and kept == "keeping it"
    chosen, loss, kept = train.pick_weights(avg, 0.7, single, 0.6)
    assert chosen is single and loss == 0.6 and kept == "keeping the single best epoch"

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

    # And the same check with a second hidden layer, which is where a
    # transposed export would show up: wh2 is input-major here and in the
    # engine, and the two would still agree on every training number if it
    # were not.
    deep = tmp_path / "deep.json"
    run_main(train, ["--pool", str(pool), "--out", str(deep), "--status", str(tmp_path / "s2.json"),
                     "--device", "cpu", "--hidden", "4", "--hidden2", "3", "--epochs", "1",
                     "--batch", "16"])
    assert json.loads(deep.read_text())["h2"] == 3
    deep_cases = tmp_path / "deep_cases.json"
    subprocess.run([str(REPO / "nnue-bin"), "-emit-eval-check", str(deep_cases), "-net-file", str(deep)],
                   check=True, cwd=REPO, capture_output=True)
    run_main(verify, ["--net", str(deep), "--cases", str(deep_cases)])


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

    # Which of the two it keeps depends on the run, so the decision itself is
    # tested directly in test_averaged_weights_are_kept_only_when_they_hold_out_better
    # rather than by hoping a training run lands on the branch.


def test_averaged_weights_are_kept_only_when_they_hold_out_better():
    avg, single = {"w": torch.zeros(1)}, {"w": torch.ones(1)}
    chosen, loss, kept = train.pick_weights(avg, 0.5, single, 0.6)
    assert chosen is avg and loss == 0.5 and kept == "keeping it"
    chosen, loss, kept = train.pick_weights(avg, 0.7, single, 0.6)
    assert chosen is single and loss == 0.6 and kept == "keeping the single best epoch"


def test_load_net_is_the_inverse_of_export(tmp_path):
    """A rung that starts from random weights has to rediscover everything the
    rung below it already knew, and rediscovers it slightly differently: a
    network explains about 91% of its teacher whatever you do, and relearning
    from scratch spends that budget afresh every time. Measured, rung 2 came
    out level with rung 1 rather than above it.

    Starting rung 2 from rung 1's weights keeps what already works and moves
    only where the new labels disagree, so export and load have to round-trip
    exactly."""
    src = train.HalfKP(hidden=6, buckets=8)
    with torch.no_grad():
        src.embed.weight.normal_()
        src.embed.weight[src.inputs].zero_()
        src.b1.normal_()
        src.out.weight.normal_()
        src.out.bias.fill_(0.25)
    path = tmp_path / "net.json"
    train.export(src, path)

    dst = train.HalfKP(hidden=6, buckets=8)
    train.load_net(dst, path)
    for name, a, b in [
        ("embed", src.embed.weight, dst.embed.weight),
        ("b1", src.b1, dst.b1),
        ("w2", src.out.weight, dst.out.weight),
        ("b2", src.out.bias, dst.out.bias),
    ]:
        assert torch.allclose(a, b, atol=1e-6), name

    # The two must now agree on any position, which is the property that
    # makes a warm start worth anything.
    own = torch.randint(0, src.inputs, (4, 7))
    opp = torch.randint(0, src.inputs, (4, 7))
    assert torch.allclose(src(own, opp), dst(own, opp), atol=1e-5)


def test_load_net_refuses_a_network_of_another_shape(tmp_path):
    src = train.HalfKP(hidden=6, buckets=8)
    path = tmp_path / "net.json"
    train.export(src, path)
    with pytest.raises(ValueError):
        train.load_net(train.HalfKP(hidden=8, buckets=8), path)


def test_a_rung_can_start_from_the_rung_below_it(tmp_path, capsys):
    """Rung 2 came out level with rung 1 when it started from random weights,
    because it had to rediscover everything rung 1 knew and rediscovered it
    slightly differently. Starting from the previous network is the cheap way
    to keep the part that already works."""
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=8, per_game=10)
    first, second = tmp_path / "first.json", tmp_path / "second.json"
    common = ["--pool", str(pool), "--status", str(tmp_path / "s.json"), "--device", "cpu",
              "--hidden", "4", "--batch", "16", "--holdout-games", "0.3", "--epochs", "1"]
    run_main(train, common + ["--out", str(first)])
    capsys.readouterr()
    run_main(train, common + ["--out", str(second), "--init-from", str(first), "--fresh"])
    assert "starting from" in capsys.readouterr().out
    assert json.loads(second.read_text())["h"] == 4


def test_ensemble_of_two_nets_is_one_net_that_outputs_their_mean(tmp_path):
    """Eight ways of training rung 2 all land level with rung 1, and the reason
    is in the labels, not the student: rung 2's teacher carries rung 1's 9%
    leaf error, search amplifies it by taking maxima, and a 91%-faithful copy
    of a noisier target gains nothing.

    Averaging two independently trained networks attacks that noise directly,
    and it fits the engine unchanged: two hidden-64 networks side by side are
    exactly one hidden-128 network whose output weights are halved."""
    a, b = train.HalfKP(hidden=5, buckets=8), train.HalfKP(hidden=5, buckets=8)
    for m in (a, b):
        with torch.no_grad():
            m.embed.weight.normal_()
            m.embed.weight[m.inputs].zero_()
            m.b1.normal_()
            m.out.weight.normal_()
            m.out.bias.normal_()
    pa, pb, pe = tmp_path / "a.json", tmp_path / "b.json", tmp_path / "e.json"
    train.export(a, pa)
    train.export(b, pb)

    merged = train.ensemble([pa, pb], pe)
    assert merged["h"] == 10

    e = train.HalfKP(hidden=10, buckets=8)
    train.load_net(e, pe)
    own = torch.randint(0, a.inputs, (6, 9))
    opp = torch.randint(0, a.inputs, (6, 9))
    want = (a(own, opp) + b(own, opp)) / 2
    assert torch.allclose(e(own, opp), want, atol=1e-4)


def test_ensemble_refuses_mismatched_nets(tmp_path):
    a, b = train.HalfKP(hidden=4, buckets=8), train.HalfKP(hidden=4, buckets=32)
    pa, pb = tmp_path / "a.json", tmp_path / "b.json"
    train.export(a, pa)
    train.export(b, pb)
    with pytest.raises(ValueError):
        train.ensemble([pa, pb], tmp_path / "e.json")
    # And a network that emits probabilities cannot be averaged in pawns.
    prob = json.loads(pa.read_text())
    prob["sigmoid"] = True
    pp = tmp_path / "p.json"
    pp.write_text(json.dumps(prob))
    with pytest.raises(ValueError):
        train.ensemble([pa, pp], tmp_path / "e2.json")


def test_a_second_hidden_layer_is_exported_and_read_back_by_the_go_forward_pass(tmp_path):
    """Width, king buckets and averaging all stopped at the same 91% of the
    teacher explained, because each of them keeps the network a single
    clipped-linear layer that can only add up one opinion per piece. A second
    layer is the one change that alters what the network can express, and it
    is what real NNUE does (256x2 -> 32 -> 32 -> 1).

    The engine has to read exactly the function that was fitted. wh2 is
    input-major, so the weights leaving accumulator unit i occupy
    wh2[i*h2 : i*h2+h2]; a transposed export would train one function and
    play another with every training number staying healthy."""
    model = train.HalfKP(hidden=4, buckets=8, hidden2=3)
    with torch.no_grad():
        model.embed.weight.normal_()
        model.embed.weight[model.inputs].zero_()
        model.mid.weight.normal_()
        model.mid.bias.normal_()
    own = train.pack([[1, 2], [3]], model.inputs, "cpu")
    opp = train.pack([[4], [5, 6]], model.inputs, "cpu")
    out = model(own, opp)

    path = tmp_path / "net.json"
    net = train.export(model, path)
    assert net["h"] == 4 and net["h2"] == 3
    assert len(net["wh2"]) == 2 * 4 * 3 and len(net["bh2"]) == 3
    # The last layer now reads the second hidden layer, not the accumulator.
    assert len(net["w2"]) == 3
    assert verify.forward(net, [1, 2], [4]) == pytest.approx(float(out[0].detach()), abs=1e-4)
    assert verify.forward(net, [3], [5, 6]) == pytest.approx(float(out[1].detach()), abs=1e-4)


def test_a_single_layer_export_says_nothing_about_a_second_one(tmp_path):
    """Every network on disk was written by the old exporter. The new fields
    must be absent from a single-layer file, not present and zero, so those
    files keep loading and keep evaluating to the bit."""
    path = tmp_path / "net.json"
    net = train.export(train.HalfKP(hidden=4, buckets=8), path)
    for key in ("h2", "wh2", "bh2"):
        assert key not in net
        assert key not in json.loads(path.read_text())


def test_load_net_round_trips_a_second_layer(tmp_path):
    src = train.HalfKP(hidden=5, buckets=8, hidden2=3)
    with torch.no_grad():
        src.embed.weight.normal_()
        src.embed.weight[src.inputs].zero_()
        src.mid.weight.normal_()
        src.mid.bias.normal_()
        src.out.weight.normal_()
    path = tmp_path / "net.json"
    train.export(src, path)
    dst = train.HalfKP(hidden=5, buckets=8, hidden2=3)
    train.load_net(dst, path)
    own = torch.randint(0, src.inputs, (4, 7))
    opp = torch.randint(0, src.inputs, (4, 7))
    assert torch.allclose(src(own, opp), dst(own, opp), atol=1e-5)
    # A warm start across a shape change would load weights that mean
    # something else, and the run would look healthy while learning nothing.
    with pytest.raises(ValueError):
        train.load_net(train.HalfKP(hidden=5, buckets=8, hidden2=4), path)
    with pytest.raises(ValueError):
        train.load_net(train.HalfKP(hidden=5, buckets=8), path)


def test_ensemble_refuses_a_network_with_a_second_layer(tmp_path):
    """Averaging works by putting hidden layers side by side, which only
    gives the mean because everything above them is linear. A second layer is
    not, so two of them side by side is a different function, not an
    average."""
    a = tmp_path / "a.json"
    train.export(train.HalfKP(hidden=4, buckets=8, hidden2=2), a)
    b = tmp_path / "b.json"
    train.export(train.HalfKP(hidden=4, buckets=8), b)
    with pytest.raises(ValueError):
        train.ensemble([a, b], tmp_path / "e.json")


def test_the_trainer_trains_and_exports_a_second_layer(tmp_path):
    pool = tmp_path / "pool.bin"
    write_pool(pool, games=8, per_game=10)
    out, ckpt = tmp_path / "net.json", tmp_path / "net.ckpt"
    common = ["--pool", str(pool), "--out", str(out), "--status", str(tmp_path / "s.json"),
              "--device", "cpu", "--hidden", "4", "--batch", "16", "--epochs", "1",
              "--holdout-games", "0.25", "--checkpoint", str(ckpt), "--checkpoint-every", "1"]
    run_main(train, common + ["--hidden2", "3"])
    net = json.loads(out.read_text())
    assert net["h2"] == 3 and len(net["w2"]) == 3
    st = json.loads((tmp_path / "s.json").read_text())
    assert "-> 3 -> 1" in st["arch"]["layers"]
    # Resuming keeps the shape.
    run_main(train, common + ["--hidden2", "3"])
    # A checkpoint holding a second layer is ignored by a run without one,
    # rather than loaded over weights that mean something else.
    run_main(train, common)
    assert "h2" not in json.loads(out.read_text())

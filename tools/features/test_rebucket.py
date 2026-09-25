"""Tests for rebucket.py: the per-scheme maps on hand-made features, the
cross-check against the engine's own 8 and 32 slot features, and a pool
round trip through the trainer's reader."""

import importlib.util
import json
import random
import struct
from collections.abc import Callable
from pathlib import Path
from typing import NamedTuple, TypedDict

import pytest

import rebucket

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
GO_FIXTURE = HERE / "testdata" / "go_features.json"

PAWN, KNIGHT = 0, 1
C1, D1, E1, G1, A2, H2, D4, E4, E8 = 2, 3, 4, 6, 8, 15, 27, 28, 60


def feature(slot: int, kind: int, sq: int) -> int:
    return slot * 640 + kind * 64 + sq


# --- 1. the schemes on hand-made features ---------------------------------


def test_king_on_g1_is_slot_1_in_b8_and_b32() -> None:
    raw = [feature(G1, PAWN, 12)]
    assert rebucket.rebucket_features(raw, "b8") == [feature(1, PAWN, 12)]
    assert rebucket.rebucket_features(raw, "b32") == [feature(1, PAWN, 12)]


def test_king_on_e8_is_the_last_slot() -> None:
    raw = [feature(E8, KNIGHT, 3)]
    assert rebucket.rebucket_features(raw, "b8") == [feature(7, KNIGHT, 3)]
    assert rebucket.rebucket_features(raw, "b32") == [feature(31, KNIGHT, 3)]


def test_mirrored_schemes_flip_the_board_with_a_kingside_king() -> None:
    raw = [feature(G1, PAWN, H2), feature(G1, KNIGHT, E4)]
    assert rebucket.rebucket_features(raw, "b8m") == [feature(1, PAWN, A2), feature(1, KNIGHT, D4)]
    assert rebucket.rebucket_features(raw, "b32m") == [feature(1, PAWN, A2), feature(1, KNIGHT, D4)]


def test_a_queenside_king_mirrors_nothing() -> None:
    raw = [feature(C1, PAWN, H2)]
    assert rebucket.rebucket_features(raw, "b8m") == [feature(2, PAWN, H2)]
    assert rebucket.rebucket_features(raw, "b32m") == [feature(2, PAWN, H2)]


def test_the_mirror_starts_on_the_e_file() -> None:
    assert rebucket.rebucket_features([feature(D1, PAWN, H2)], "b8m") == [feature(3, PAWN, H2)]
    assert rebucket.rebucket_features([feature(E1, PAWN, H2)], "b8m") == [feature(3, PAWN, A2)]


def test_b64_is_the_identity() -> None:
    every = list(range(rebucket.RAW_INPUTS))
    assert rebucket.rebucket_features(every, "b64") == every


@pytest.mark.parametrize("name", sorted(rebucket.SCHEMES))
def test_every_scheme_stays_in_range_and_keeps_pieces_apart(name: str) -> None:
    """Within one king square the map must be injective, or two pieces collapse."""
    scheme = rebucket.SCHEMES[name]
    for king in range(rebucket.RAW_SLOTS):
        raw = [feature(king, kind, sq) for kind in range(10) for sq in range(64)]
        mapped = rebucket.rebucket_features(raw, name)
        assert len(set(mapped)) == len(raw)
        assert all(0 <= f < scheme.slots * rebucket.PER_KING for f in mapped)


def test_an_unknown_scheme_or_a_feature_past_64_slots_is_refused() -> None:
    with pytest.raises(ValueError, match="scheme"):
        rebucket.rebucket_features([0], "b16")
    with pytest.raises(ValueError, match="64 king slots"):
        rebucket.rebucket_features([rebucket.RAW_INPUTS], "b8")


# --- 2. cross-check against the engine ------------------------------------


class Perspectives(TypedDict):
    white: list[int]
    black: list[int]


class FixturePosition(TypedDict):
    fen: str
    features: dict[str, Perspectives]


def go_positions() -> list[FixturePosition]:
    fixture: dict[str, list[FixturePosition]] = json.loads(GO_FIXTURE.read_text())
    return fixture["positions"]


def sides(p: Perspectives) -> list[tuple[str, list[int]]]:
    return [("white", p["white"]), ("black", p["black"])]


def test_the_go_fixture_puts_kings_on_both_wings_and_both_halves() -> None:
    """A fixture with every king on one wing would pass a broken mirror."""
    seen: set[tuple[str, bool, bool]] = set()
    for pos in go_positions():
        for side, features in sides(pos["features"]["64"]):
            for f in features:
                kr, kf = divmod(f // rebucket.PER_KING, 8)
                seen.add((side, kf > 3, kr >= 4))
    assert len(go_positions()) >= 40
    assert seen == {(s, wing, half) for s in ("white", "black") for wing in (False, True) for half in (False, True)}


@pytest.mark.parametrize(("scheme", "slots"), [("b8", "8"), ("b32", "32"), ("b64", "64")])
def test_rebucketed_64_slot_features_equal_the_engines(scheme: str, slots: str) -> None:
    compared = 0
    for pos in go_positions():
        for (side, raw), (_, expected) in zip(sides(pos["features"]["64"]), sides(pos["features"][slots])):
            got = rebucket.rebucket_features(raw, scheme)
            assert sorted(got) == sorted(expected), f"{pos['fen']} {side}"
            compared += len(expected)
    assert compared > 0


# --- 3. pools --------------------------------------------------------------


class Record(NamedTuple):
    game: int
    target: float
    static: float
    own: list[int]
    opp: list[int]


LoadPool = Callable[[Path], tuple[list[tuple[int, ...]], list[tuple[int, ...]], list[float], list[int]]]


def trainer_load_pool() -> LoadPool:
    """pytorch/train.py's own reader, so the round trip proves the trainer can read the output."""
    spec = importlib.util.spec_from_file_location("train", REPO / "pytorch" / "train.py")
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    load: LoadPool = module.load_pool
    return load


def raw_records(n: int, seed: int = 3) -> list[Record]:
    rng = random.Random(seed)
    records = []
    for i in range(n):
        own_king, opp_king = rng.randrange(64), rng.randrange(64)
        own = [feature(own_king, rng.randrange(10), rng.randrange(64)) for _ in range(rng.randint(2, 30))]
        opp = [feature(opp_king, rng.randrange(10), rng.randrange(64)) for _ in range(rng.randint(2, 30))]
        records.append(Record(i // 4, rng.uniform(-12, 12), rng.uniform(-12, 12), own, opp))
    return records


def write_pool(path: Path, records: list[Record]) -> None:
    with path.open("wb") as f:
        for r in records:
            f.write(struct.pack("<iffBB", r.game, r.target, r.static, len(r.own), len(r.opp)))
            f.write(struct.pack("<%dH" % (len(r.own) + len(r.opp)), *r.own, *r.opp))


def record_heads(data: bytes) -> list[bytes]:
    """Each record's game, target, static and counts, as raw bytes."""
    heads, pos = [], 0
    while pos < len(data):
        n_own, n_opp = data[pos + 12], data[pos + 13]
        heads.append(data[pos : pos + 14])
        pos += 14 + 2 * (n_own + n_opp)
    return heads


def test_a_rebucketed_pool_reads_back_through_the_trainer(tmp_path: Path) -> None:
    records = raw_records(200)
    source = tmp_path / "raw_64.bin"
    write_pool(source, records)
    written = rebucket.rebucket_pool(source, tmp_path / "raw", ["b8", "b32m"], assume_raw64=True)
    assert written == [tmp_path / "raw_b8.bin", tmp_path / "raw_b32m.bin"]

    load_pool = trainer_load_pool()
    _, _, want_targets, want_games = load_pool(source)
    for out, name in zip(written, ["b8", "b32m"]):
        own, opp, targets, games = load_pool(out)
        assert targets == want_targets and games == want_games
        assert [list(o) for o in own] == [rebucket.rebucket_features(r.own, name) for r in records]
        assert [list(o) for o in opp] == [rebucket.rebucket_features(r.opp, name) for r in records]
        assert record_heads(out.read_bytes()) == record_heads(source.read_bytes())


def test_the_meta_names_the_source_the_scheme_and_the_slot_count(tmp_path: Path) -> None:
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(5))
    (tmp_path / "raw_64.bin.meta.json").write_text(json.dumps({"positions": "pgn", "labeller": "self", "buckets": 64}))
    (out,) = rebucket.rebucket_pool(source, tmp_path / "raw", ["b8m"])
    meta = json.loads((tmp_path / "raw_b8m.bin.meta.json").read_text())
    assert out == tmp_path / "raw_b8m.bin"
    assert meta == {
        "positions": "pgn",
        "labeller": "self",
        "buckets": 8,
        "king_mirror": True,
        "scheme": "b8m",
        "rebucketed_from": str(source),
        "records": 5,
    }


@pytest.mark.parametrize("name", sorted(rebucket.SCHEMES))
def test_every_meta_says_whether_the_board_is_mirrored(tmp_path: Path, name: str) -> None:
    """The trainer reads king_mirror, never the scheme name, to tell a mirrored pool apart."""
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(5))
    (tmp_path / "raw_64.bin.meta.json").write_text(json.dumps({"buckets": 64, "king_mirror": False}))
    (out,) = rebucket.rebucket_pool(source, tmp_path / "raw", [name])
    meta = json.loads(Path(f"{out}.meta.json").read_text())
    assert meta["king_mirror"] is rebucket.SCHEMES[name].mirrors_board
    assert meta["buckets"] == rebucket.SCHEMES[name].slots


def test_a_pool_that_is_not_64_slots_is_refused_and_nothing_written(tmp_path: Path) -> None:
    source = tmp_path / "bad.bin"
    bad = raw_records(3)
    bad[2].opp.append(rebucket.RAW_INPUTS)
    write_pool(source, bad)
    with pytest.raises(ValueError, match="64 king slots"):
        rebucket.rebucket_pool(source, tmp_path / "bad", ["b8"], assume_raw64=True)

    labelled_8 = tmp_path / "eight.bin"
    write_pool(labelled_8, raw_records(3))
    (tmp_path / "eight.bin.meta.json").write_text(json.dumps({"buckets": 8}))
    with pytest.raises(ValueError, match="buckets 8"):
        rebucket.rebucket_pool(labelled_8, tmp_path / "eight", ["b8"])
    assert sorted(p.name for p in tmp_path.iterdir()) == ["bad.bin", "eight.bin", "eight.bin.meta.json"]


def test_a_truncated_tail_is_dropped_like_the_trainer_does(tmp_path: Path) -> None:
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(4))
    source.write_bytes(source.read_bytes()[:-3])
    (out,) = rebucket.rebucket_pool(source, tmp_path / "raw", ["b32"], assume_raw64=True)
    assert len(trainer_load_pool()(out)[2]) == 3
    assert json.loads(Path(f"{out}.meta.json").read_text())["records"] == 3


def test_the_command_line_writes_one_pool_per_scheme(tmp_path: Path) -> None:
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(4))
    assert rebucket.main([str(source), str(tmp_path / "out"), "b8", "b64", "--assume-raw64"]) == 0
    assert (tmp_path / "out_b64.bin").read_bytes() == source.read_bytes()
    assert (tmp_path / "out_b8.bin.meta.json").exists()


@pytest.mark.parametrize(
    ("sidecar", "match"),
    [({"buckets": 64, "king_mirror": True}, "king_mirror True"), ({"positions": "pgn"}, "buckets None")],
)
def test_a_sidecar_must_say_64_unmirrored_slots(tmp_path: Path, sidecar: dict[str, object], match: str) -> None:
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(3))
    (tmp_path / "raw_64.bin.meta.json").write_text(json.dumps(sidecar))
    with pytest.raises(ValueError, match=match):
        rebucket.rebucket_pool(source, tmp_path / "out", ["b8"], assume_raw64=True)
    assert sorted(p.name for p in tmp_path.iterdir()) == ["raw_64.bin", "raw_64.bin.meta.json"]


def test_a_pool_without_a_sidecar_is_refused_unless_assumed_raw(tmp_path: Path) -> None:
    source = tmp_path / "raw_64.bin"
    write_pool(source, raw_records(20))
    with pytest.raises(ValueError, match="--assume-raw64"):
        rebucket.rebucket_pool(source, tmp_path / "out", ["b8"])
    with pytest.raises(ValueError, match="--assume-raw64"):
        rebucket.main([str(source), str(tmp_path / "out"), "b8"])
    assert sorted(p.name for p in tmp_path.iterdir()) == ["raw_64.bin"]


def test_assuming_raw_still_refuses_a_pool_no_feature_of_which_passes_8_slots(tmp_path: Path) -> None:
    """Every 8-slot feature is below 8 x 640, so a pool with none above it is not a 64-slot one."""
    eight_slots = rebucket.PER_KING * 8
    eight = tmp_path / "eight.bin"
    write_pool(eight, [Record(0, 0.5, 0.5, [feature(7, KNIGHT, 63), eight_slots - 1], [feature(0, PAWN, 12)])])
    with pytest.raises(ValueError, match=str(eight_slots)):
        rebucket.rebucket_pool(eight, tmp_path / "eight", ["b8"], assume_raw64=True)
    assert sorted(p.name for p in tmp_path.iterdir()) == ["eight.bin"]

    raw = tmp_path / "raw.bin"
    write_pool(raw, [Record(0, 0.5, 0.5, [feature(8, PAWN, 0)], [feature(0, PAWN, 12)])])
    assert feature(8, PAWN, 0) == eight_slots
    (out,) = rebucket.rebucket_pool(raw, tmp_path / "raw", ["b8"], assume_raw64=True)
    assert out.exists()

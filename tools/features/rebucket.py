"""Derive coarser king-slot feature schemes from a pool written with 64 king slots.

usage: rebucket.py IN_64.bin OUT_PREFIX scheme [scheme ...] [--assume-raw64]

IN_64.bin's sidecar must say buckets 64 and no king_mirror; --assume-raw64 vouches
for a pool that has no sidecar, which must then hold a feature past 8 king slots.

A 64-slot feature is slot*640 + kind*64 + sq with slot the perspective's exact
king square, so every scheme below is a pure function of each feature. Reading
order: the schemes, the per-feature map, the pool rewrite, the command line.
"""

import json
import struct
import sys
from array import array
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import NamedTuple

PER_KING = 10 * 64  # 5 piece kinds per side, kings excluded, times 64 squares
RAW_SLOTS = 64
RAW_INPUTS = RAW_SLOTS * PER_KING
EIGHT_SLOT_INPUTS = 8 * PER_KING  # no 8-slot pool holds a feature at or past this
ASSUME_RAW64 = "--assume-raw64"
HEAD = struct.Struct("<iffBB")  # game, target, static, nOwn, nOpp; features follow
WORD = 2  # every field is a whole number of uint16 words, so features sit on word boundaries


class Scheme(NamedTuple):
    slots: int
    slot_of: Callable[[int, int], int]  # (king rank, king file) -> slot
    mirrors_board: bool  # a kingside king flips every piece file with it


def _queenside_file(file: int) -> int:
    return file if file <= 3 else 7 - file


def _bucket8(rank: int, file: int) -> int:
    return (rank // 4) * 4 + _queenside_file(file)


def _square32(rank: int, file: int) -> int:
    return rank * 4 + _queenside_file(file)


def _raw_square(rank: int, file: int) -> int:
    return rank * 8 + file


SCHEMES: dict[str, Scheme] = {
    "b8": Scheme(8, _bucket8, False),  # engine kingBucket
    "b32": Scheme(32, _square32, False),  # engine kingCanonicalSquare
    "b8m": Scheme(8, _bucket8, True),
    "b32m": Scheme(32, _square32, True),
    "b64": Scheme(64, _raw_square, False),
}


def rebucket_features(features: Sequence[int], name: str) -> list[int]:
    """One perspective's 64-slot features in scheme `name`, in the same order."""
    scheme = _scheme(name)
    return [_rebucket_feature(f, scheme) for f in _checked(features)]


def rebucket_pool(source: Path, out_prefix: Path, names: Sequence[str], assume_raw64: bool = False) -> list[Path]:
    """Write OUT_PREFIX_<scheme>.bin and its .meta.json for each scheme; headers are copied byte for byte.

    The source must be a raw 64-slot pool: its sidecar says so, or with none, assume_raw64 vouches for it."""
    schemes = [(name, _scheme(name)) for name in names]
    provenance = _source_provenance(source, assume_raw64)
    data = source.read_bytes()
    spans, end = _feature_spans(data)
    words = array("H")
    words.frombytes(data[:end])
    if sys.byteorder == "big":
        words.byteswap()
    top = max((max(words[s:e]) for s, e in spans if e > s), default=0)
    _refuse_past_64_slots(top)
    if provenance is None:
        _refuse_within_8_slots(source, top)
    written = []
    for name, scheme in schemes:
        out = Path(f"{out_prefix}_{name}.bin")
        out.write_bytes(_remapped(words, spans, _feature_table(scheme)))
        meta = {
            **(provenance or {}),
            "buckets": scheme.slots,
            "king_mirror": scheme.mirrors_board,
            "scheme": name,
            "rebucketed_from": str(source),
            "records": len(spans),
        }
        Path(f"{out}.meta.json").write_text(json.dumps(meta, indent=2) + "\n")
        written.append(out)
    return written


def main(argv: Sequence[str]) -> int:
    assume_raw64 = ASSUME_RAW64 in argv
    args = [a for a in argv if a != ASSUME_RAW64]
    if len(args) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    for out in rebucket_pool(Path(args[0]), Path(args[1]), args[2:], assume_raw64):
        print(out)
    return 0


# --- the per-feature map ----------------------------------------------------


def _scheme(name: str) -> Scheme:
    if name not in SCHEMES:
        raise ValueError(f"unknown scheme {name!r}, expected one of {sorted(SCHEMES)}")
    return SCHEMES[name]


def _checked(features: Sequence[int]) -> Sequence[int]:
    for f in features:
        _refuse_past_64_slots(f)
    return features


def _refuse_past_64_slots(feature: int) -> None:
    if not 0 <= feature < RAW_INPUTS:
        raise ValueError(f"feature {feature} is not one of 64 king slots x {PER_KING}")


def _rebucket_feature(feature: int, scheme: Scheme) -> int:
    king, rest = divmod(feature, PER_KING)
    kind, sq = divmod(rest, 64)
    king_rank, king_file = divmod(king, 8)
    if scheme.mirrors_board and king_file > 3:
        rank, file = divmod(sq, 8)
        sq = rank * 8 + 7 - file
    return scheme.slot_of(king_rank, king_file) * PER_KING + kind * 64 + sq


def _feature_table(scheme: Scheme) -> list[int]:
    return [_rebucket_feature(f, scheme) for f in range(RAW_INPUTS)]


# --- the pool rewrite -------------------------------------------------------


def _source_provenance(source: Path, assume_raw64: bool) -> dict[str, object] | None:
    """The source's sidecar, carried forward, or None for a pool that has none and is assumed raw.

    Anything but 64 unmirrored king slots is refused: mapping those features again yields nonsense."""
    meta_path = Path(f"{source}.meta.json")
    if not meta_path.exists():
        if not assume_raw64:
            raise ValueError(f"{source} has no {meta_path.name}; pass {ASSUME_RAW64} if it holds 64 unmirrored king slots")
        return None
    meta: dict[str, object] = json.loads(meta_path.read_text())
    buckets, mirror = meta.get("buckets"), meta.get("king_mirror", False)
    if buckets != RAW_SLOTS or mirror is not False:
        raise ValueError(f"{meta_path} says buckets {buckets} and king_mirror {mirror}, not 64 unmirrored king slots")
    return meta


def _refuse_within_8_slots(source: Path, top: int) -> None:
    if top < EIGHT_SLOT_INPUTS:
        raise ValueError(f"{source}: no feature reaches {EIGHT_SLOT_INPUTS}, so it is no 64-slot pool (an 8-slot one never does)")


def _feature_spans(data: bytes) -> tuple[list[tuple[int, int]], int]:
    """Each complete record's feature range in words, and the byte length they cover.

    A truncated tail is a crash mid-write and is dropped, as the trainer's reader does.
    """
    spans, pos = [], 0
    while pos + HEAD.size <= len(data):
        *_, n_own, n_opp = HEAD.unpack_from(data, pos)
        start, stop = pos + HEAD.size, pos + HEAD.size + WORD * (n_own + n_opp)
        if stop > len(data):
            break
        spans.append((start // WORD, stop // WORD))
        pos = stop
    return spans, pos


def _remapped(words: array[int], spans: list[tuple[int, int]], table: list[int]) -> bytes:
    out = array("H", words)
    for start, stop in spans:
        out[start:stop] = array("H", map(table.__getitem__, out[start:stop]))
    if sys.byteorder == "big":
        out.byteswap()
    return out.tobytes()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

import balanced_openings


def test_keeps_level_positions_and_drops_decided_ones() -> None:
    assert balanced_openings.is_balanced(30, 0.5)
    assert balanced_openings.is_balanced(-49, 0.5)
    assert not balanced_openings.is_balanced(50, 0.5)
    assert not balanced_openings.is_balanced(-120, 0.5)


def test_an_invalid_start_is_not_usable() -> None:
    # openings.txt line 757: castling rights Qq with no rook left to castle with.
    assert not balanced_openings.usable("r2qk3/2p1p1pr/1p1pPp1p/p1nPb1N1/3B4/2N5/PPP2PPP/R2Q1RK1 b Qq - 0 1")
    assert balanced_openings.usable("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
    assert not balanced_openings.usable("not a fen")


def test_a_mate_score_is_never_balanced() -> None:
    # python-chess returns None from .score() for a mate without mate_score.
    assert not balanced_openings.is_balanced(None, 0.5)

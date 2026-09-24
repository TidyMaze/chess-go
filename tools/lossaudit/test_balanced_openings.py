import balanced_openings


def test_keeps_level_positions_and_drops_decided_ones() -> None:
    assert balanced_openings.is_balanced(30, 0.5)
    assert balanced_openings.is_balanced(-49, 0.5)
    assert not balanced_openings.is_balanced(50, 0.5)
    assert not balanced_openings.is_balanced(-120, 0.5)


def test_a_mate_score_is_never_balanced() -> None:
    # python-chess returns None from .score() for a mate without mate_score.
    assert not balanced_openings.is_balanced(None, 0.5)

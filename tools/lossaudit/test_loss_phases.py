from typing import Any

import loss_phases

WHITE_TO_MOVE = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
BLACK_TO_MOVE = "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq - 0 1"


def record(start: str, we_white: bool, winner: int, decisive: bool, scores: list[float]) -> dict[str, Any]:
    us, them = "challenger: champion", "reference: stockfish"
    return {
        "start_fen": start,
        "white": us if we_white else them,
        "black": them if we_white else us,
        "winner": winner,
        "decisive": decisive,
        "moves": ["e2e4", "e7e5", "g1f3", "b8c6"][: len(scores)],
        "scores": scores,
    }


def test_our_scores_follow_the_side_to_move_of_the_start_position() -> None:
    # Black to move at the start and we are Black: our moves are the even plies.
    assert loss_phases.our_parity(record(BLACK_TO_MOVE, False, 0, True, [])) == 0
    # White to move and we are Black: our moves are the odd plies.
    assert loss_phases.our_parity(record(WHITE_TO_MOVE, False, 0, True, [])) == 1


def test_a_loss_is_a_decisive_game_won_by_the_other_colour() -> None:
    assert loss_phases.is_our_loss(record(WHITE_TO_MOVE, True, 1, True, []))  # we White, Black won
    assert not loss_phases.is_our_loss(record(WHITE_TO_MOVE, True, 0, True, []))  # we won
    assert not loss_phases.is_our_loss(record(WHITE_TO_MOVE, True, 0, False, []))  # draw


def test_a_collapse_we_noticed_with_stockfish_is_sudden() -> None:
    # Plies: ours 0, sf 1, ours 2, sf 3. Stockfish +2 at ply 1, us -2 at ply 2.
    loss = loss_phases.classify(record(WHITE_TO_MOVE, True, 1, True, [0.1, 2.0, -2.0, 3.0]))
    assert (loss.kind, loss.ply) == ("sudden", 2)


def test_a_loss_our_score_never_saw_is_unseen() -> None:
    loss = loss_phases.classify(record(WHITE_TO_MOVE, True, 1, True, [0.1, 2.0, 0.2, 3.0]))
    assert loss.kind == "unseen"

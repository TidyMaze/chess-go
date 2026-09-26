import json
from pathlib import Path
from typing import Any

import chess
import chess.engine

import convert_check

PGN = Path(__file__).resolve().parents[2] / "analyses" / "lichess-games" / "lhT8MqvU.pgn"
MATE_IN_ONE_AT_99 = "7k/8/6K1/8/8/8/8/R7 w - - 99 80"


class ScriptedOurs:
    """Our engine's interface, playing the given UCI moves in order."""

    def __init__(self, moves: list[str], score: str = "?") -> None:
        self.moves, self.score, self.games = list(moves), score, 0

    def new_game(self) -> None:
        self.games += 1

    def play(self, board: chess.Board, movetime_ms: int) -> tuple[chess.Move, str]:
        return chess.Move.from_uci(self.moves.pop(0)), self.score


class ScriptedStockfish:
    """python-chess's engine interface, playing the given UCI moves in order."""

    def __init__(self, moves: list[str]) -> None:
        self.moves = list(moves)

    def play(self, board: chess.Board, limit: chess.engine.Limit, **_: Any) -> chess.engine.PlayResult:
        return chess.engine.PlayResult(chess.Move.from_uci(self.moves.pop(0)), None)


def test_a_promotion_printed_without_its_piece_becomes_a_queen() -> None:
    board = chess.Board("8/3P1R2/k3K1p1/6P1/1p1B4/1P6/1P6/8 w - - 3 109")
    lines = ["info string cutoff 0", "info depth 14 score mate 2 nodes 9 nps 9 time 9", "bestmove d7d8"]
    assert convert_check.parse_bestmove(board, lines) == (chess.Move.from_uci("d7d8q"), "mate 2 d14")
    black = chess.Board("8/8/8/8/8/k7/4p3/K7 b - - 0 1")
    assert convert_check.parse_bestmove(black, ["bestmove e2e1"])[0] == chess.Move.from_uci("e2e1q")


def test_other_moves_and_the_last_score_are_read_as_printed() -> None:
    board = chess.Board("2R5/1K4k1/3B2p1/3P2P1/1p6/1P6/1P6/8 w - - 1 56")
    lines = ["info depth 15 score cp 100500 nodes 1 nps 1 time 1", "info depth 16 score cp 100700 nodes 2 nps 1 time 1",
             "bestmove c8c7"]
    assert convert_check.parse_bestmove(board, lines) == (chess.Move.from_uci("c8c7"), "cp 100700 d16")
    assert convert_check.parse_bestmove(board, ["bestmove b7c6"])[1] == "?"


def test_the_clock_field_is_replaced_and_nothing_else() -> None:
    assert convert_check.with_clock("8/8/8/3k4/8/8/8/KQ6 w - - 0 1", 80) == "8/8/8/3k4/8/8/8/KQ6 w - - 80 1"


def test_a_mate_on_the_hundredth_halfmove_is_a_mate_not_a_draw() -> None:
    board = chess.Board(MATE_IN_ONE_AT_99)
    board.push_uci("a1a8")
    assert board.halfmove_clock == 100
    assert convert_check.termination(board) == "mate"


def test_the_fifty_move_draw_comes_at_clock_100_not_one_move_early() -> None:
    board = chess.Board(MATE_IN_ONE_AT_99)
    assert convert_check.termination(board) is None
    board.push_uci("a1b1")
    assert convert_check.termination(board) == "fifty"


def test_threefold_stalemate_and_bare_kings_are_draws() -> None:
    board = chess.Board("7k/8/6K1/8/8/8/8/R7 w - - 0 1")
    for move in ["a1b1", "h8g8", "b1a1", "g8h8"] * 2:
        board.push_uci(move)
    assert convert_check.termination(board) == "threefold"
    assert convert_check.termination(chess.Board("7k/5Q2/6K1/8/8/8/8/8 b - - 0 1")) == "stalemate"
    assert convert_check.termination(chess.Board("7k/8/6K1/8/8/8/8/8 w - - 0 1")) == "insufficient"


def test_the_games_white_starts_run_from_ply_110_to_208_with_their_own_clock() -> None:
    starts = convert_check.game_starts(PGN.read_text(), 109, chess.WHITE)
    assert len(starts) == 50
    assert starts[0] == convert_check.Start("ply 110", "2R5/1K4k1/3B2p1/3P2P1/1p6/1P6/1P6/8 w - - 1 56")
    assert starts[-1] == convert_check.Start("ply 208", "8/6B1/1k2KRp1/3P2P1/1p6/1P6/1P6/8 w - - 99 105")


def test_every_tenth_start_and_the_last_are_sampled() -> None:
    starts = convert_check.game_starts(PGN.read_text(), 109, chess.WHITE)
    picked = [s.name for s in convert_check.sample(starts, 10)]
    assert picked == ["ply 110", "ply 130", "ply 150", "ply 170", "ply 190", "ply 208"]


def test_recorded_games_are_skipped_and_a_torn_last_line_is_ignored(tmp_path: Path) -> None:
    out = tmp_path / "runs.jsonl"
    assert convert_check.done_keys(out) == set()
    out.write_text(json.dumps({"key": "game|ply 110|1|fixed"}) + "\n" + '{"key": "game|ply 1')
    assert convert_check.done_keys(out) == {"game|ply 110|1|fixed"}


def test_a_quiet_mate_at_clock_99_is_converted_in_one_move() -> None:
    ours = ScriptedOurs(["a1a8"], "mate 1 d3")
    row = convert_check.play_game(ours, ScriptedStockfish([]), MATE_IN_ONE_AT_99, "k", 1, 0.2, 100)  # type: ignore[arg-type]
    assert (row["result"], row["converted"], row["our_moves"], row["max_clock"]) == ("mate", True, 1, 100)
    assert (row["scores"], ours.games) == (["mate 1 d3"], 1)


def test_a_quiet_non_mating_move_at_clock_99_is_the_fifty_move_draw() -> None:
    ours, sf = ScriptedOurs(["a1b1"]), ScriptedStockfish([])
    row = convert_check.play_game(ours, sf, MATE_IN_ONE_AT_99, "k", 1, 0.2, 100)  # type: ignore[arg-type]
    assert (row["result"], row["converted"], row["plies"], row["resets"]) == ("fifty", False, 1, 0)


def test_the_ply_cap_ends_a_game_and_each_side_moves_in_turn() -> None:
    ours, sf = ScriptedOurs(["a1b1", "b1a1"]), ScriptedStockfish(["h8g8", "g8h8"])
    row = convert_check.play_game(ours, sf, "7k/8/6K1/8/8/8/8/R7 w - - 0 1", "k", 1, 0.2, 3)  # type: ignore[arg-type]
    assert (row["result"], row["plies"], row["our_moves"], row["moves"]) == ("cap", 3, 2, ["a1b1", "h8g8", "b1a1"])
    assert convert_check.cell(row) == "no mate in 3 plies (clock 3)"


def test_the_report_puts_engines_side_by_side_and_counts_conversions() -> None:
    base = {"mode": "suite", "name": "KRK", "sf_start": "mate 16", "lost": False, "plies": 31}
    rows = [
        {**base, "key": "suite|KRK|0|master", "clock": 0, "engine": "master", "converted": True, "our_moves": 16,
         "result": "mate"},
        {**base, "key": "suite|KRK|0|fixed", "clock": 0, "engine": "fixed", "converted": True, "our_moves": 15,
         "result": "mate"},
        {**base, "key": "suite|KRK|80|master", "clock": 80, "engine": "master", "converted": False, "our_moves": 10,
         "result": "fifty", "plies": 20},
        {**base, "key": "suite|KRK|80|fixed", "clock": 80, "engine": "fixed", "converted": True, "our_moves": 9,
         "result": "mate"},
    ]
    text = convert_check.report(rows)
    assert "| KRK | 0 | mate 16 | mate in 16 | mate in 15 | no | no |" in text
    assert "| KRK | 80 | mate 16 | draw: fifty after 20 plies | mate in 9 | yes | no |" in text
    assert "| master | 80 | 0 | 1 |" in text and "| fixed | 80 | 1 | 1 |" in text

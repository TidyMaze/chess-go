import json
from pathlib import Path

import pytest

import spsa


def test_reads_the_record_from_gauntlet_output() -> None:
    out = "challenger (depth 4) vs reference (depth 4), 20 games\n  W-D-L 9-4-7   score 0.550\n"
    assert spsa.parse_wdl(out) == (9, 4, 7)


def test_refuses_output_without_a_record() -> None:
    with pytest.raises(ValueError):
        spsa.parse_wdl("champion: net_file did not load\n")


def test_a_win_for_the_plus_side_moves_each_parameter_toward_its_perturbation() -> None:
    params = {"A": spsa.Param(1.0, 0.1), "B": spsa.Param(2.0, 0.5)}
    theta = {"A": 1.0, "B": 2.0}
    delta = {"A": 1, "B": -1}
    moved = spsa.step(theta, params, delta, wins=12, draws=4, losses=4, lr=1.0)
    assert moved["A"] > 1.0  # plus side had A higher, and won
    assert moved["B"] < 2.0  # plus side had B lower, and won


def test_an_even_result_leaves_the_parameters_alone() -> None:
    params = {"A": spsa.Param(1.0, 0.1)}
    moved = spsa.step({"A": 1.0}, params, {"A": 1}, wins=5, draws=10, losses=5, lr=1.0)
    assert moved == {"A": 1.0}


def test_resumes_from_the_last_journal_line(tmp_path: Path) -> None:
    journal = tmp_path / "spsa.jsonl"
    journal.write_text(
        json.dumps({"iter": 0, "theta": {"A": 1.1}}) + "\n" + json.dumps({"iter": 1, "theta": {"A": 1.2}}) + "\n"
    )
    start, theta = spsa.resume(journal, {"A": spsa.Param(1.0, 0.1)})
    assert (start, theta) == (2, {"A": 1.2})


def test_stops_before_a_round_that_would_overrun_the_budget() -> None:
    # 25 minutes left and rounds of 80 s: keep going.
    assert spsa.has_time_for_another(elapsed_s=300, last_round_s=80, budget_s=1800)
    # 20 s left, a round takes 80 s: stop now rather than overrun.
    assert not spsa.has_time_for_another(elapsed_s=1780, last_round_s=80, budget_s=1800)
    # The first round has no duration yet and always runs.
    assert spsa.has_time_for_another(elapsed_s=0, last_round_s=0, budget_s=1800)


def test_starts_from_the_defaults_without_a_journal(tmp_path: Path) -> None:
    start, theta = spsa.resume(tmp_path / "none.jsonl", {"A": spsa.Param(1.0, 0.1)})
    assert (start, theta) == (0, {"A": 1.0})

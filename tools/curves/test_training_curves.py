import json
import re
from pathlib import Path

import pytest

import training_curves

SAMPLE = Path(__file__).resolve().parent / "testdata" / "sample_curve.json"


def curve(label: str, loss: str = "mse", units: str = "pawns^2") -> dict[str, object]:
    return {
        "label": label,
        "loss": loss,
        "units": units,
        "baselines": {"constant": 0.5, "material": 0.3},
        "best_epoch": 2,
        "epochs": [
            {"epoch": 1, "train": 0.2, "validation": 0.1, "test": 0.11},
            {"epoch": 2, "train": 0.1, "validation": 0.06, "test": 0.05},
            {"epoch": 3, "train": 0.05, "validation": 0.07, "test": 0.08},
        ],
    }


def write(tmp_path: Path, name: str, content: dict[str, object]) -> Path:
    path = tmp_path / name
    path.write_text(json.dumps(content))
    return path


def embedded_runs(page: str) -> list[dict[str, object]]:
    match = re.search(r'<script type="application/json" id="runs">(.*?)</script>', page, re.S)
    assert match is not None
    runs: list[dict[str, object]] = json.loads(match.group(1))
    return runs


def test_loads_a_valid_curve(tmp_path: Path) -> None:
    loaded = training_curves.load_curve(write(tmp_path, "a.json", curve("run a")))
    assert loaded["label"] == "run a"
    assert loaded["baselines"]["constant"] == 0.5
    assert [e["epoch"] for e in loaded["epochs"]] == [1, 2, 3]


def test_rejects_a_file_missing_epochs(tmp_path: Path) -> None:
    broken = curve("run a")
    del broken["epochs"]
    with pytest.raises(training_curves.CurveError, match="missing 'epochs'"):
        training_curves.load_curve(write(tmp_path, "a.json", broken))


@pytest.mark.parametrize(
    "field, value",
    [
        ("loss", "mae"),
        ("units", "centipawns"),
        ("best_epoch", 7),
        ("baselines", {"constant": 0.5}),
        ("epochs", [{"epoch": 1, "train": 0.2, "validation": 0.1}]),
        ("epochs", [{"epoch": 1, "train": 0.0, "validation": 0.1, "test": 0.1}]),
        ("epochs", [{"epoch": 1, "train": float("nan"), "validation": 0.1, "test": 0.1}]),
        ("label", 3),
    ],
)
def test_rejects_a_contract_violation(tmp_path: Path, field: str, value: object) -> None:
    broken = curve("run a")
    broken[field] = value
    with pytest.raises(training_curves.CurveError):
        training_curves.load_curve(write(tmp_path, "a.json", broken))


def test_explained_variance_is_measured_at_the_best_epoch(tmp_path: Path) -> None:
    loaded = training_curves.load_curve(write(tmp_path, "a.json", curve("run a")))
    summary = training_curves.summarize(loaded)
    assert summary["final_test"] == 0.08
    assert summary["test_at_best"] == 0.05
    assert summary["explained_pct"] == pytest.approx(90.0)


def test_page_holds_every_run_label_and_every_epoch(tmp_path: Path) -> None:
    out = tmp_path / "curves.html"
    paths = [
        write(tmp_path, "a.json", curve("mse run")),
        write(tmp_path, "b.json", curve("sigmoid run", "sigmoid", "win-probability^2")),
    ]
    training_curves.main([str(out), *map(str, paths)])
    page = out.read_text()
    assert "<title>Training curves</title>" in page
    assert "mse run" in page and "sigmoid run" in page
    runs = embedded_runs(page)
    assert [r["curve"] for r in runs] == [curve("mse run"), curve("sigmoid run", "sigmoid", "win-probability^2")]
    assert "\N{EM DASH}" not in page


def test_a_label_cannot_close_the_data_script(tmp_path: Path) -> None:
    loaded = training_curves.load_curve(write(tmp_path, "a.json", curve("x</script><b>y")))
    page = training_curves.render_page([loaded])
    assert "</script><b>" not in page
    assert embedded_runs(page)[0]["curve"] == curve("x</script><b>y")


def test_the_sample_curve_honours_the_contract() -> None:
    loaded = training_curves.load_curve(SAMPLE)
    assert len(loaded["epochs"]) == 10

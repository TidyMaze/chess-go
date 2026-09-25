import functools
import json
import re
import subprocess
import tempfile
import threading
from pathlib import Path
from typing import NamedTuple, TypedDict

import pytest

import training_curves

TESTDATA = Path(__file__).resolve().parent / "testdata"
SAMPLE = TESTDATA / "sample_curve.json"
SAMPLES = tuple(TESTDATA / n for n in ("sample_curve.json", "sample_curve_lr_small.json", "sample_curve_lr_large.json"))
CHROME = Path("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")


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


def load_all(tmp_path: Path, *contents: dict[str, object]) -> list[training_curves.Curve]:
    return [training_curves.load_curve(write(tmp_path, f"{i}.json", c)) for i, c in enumerate(contents)]


def embedded_json(page: str, element_id: str) -> object:
    match = re.search(rf'<script type="application/json" id="{element_id}">(.*?)</script>', page, re.S)
    assert match is not None
    value: object = json.loads(match.group(1))
    return value


def embedded_comparisons(page: str) -> object:
    return embedded_json(page, "comparison-data")


def sigmoid(label: str) -> dict[str, object]:
    return curve(label, "sigmoid", "win-probability^2")


def test_runs_sharing_units_get_one_comparison_and_a_lone_run_gets_none(tmp_path: Path) -> None:
    curves = load_all(tmp_path, curve("mse a"), sigmoid("sigmoid b"), curve("mse c"))
    comparisons = training_curves.compare(curves)
    assert [c["units"] for c in comparisons] == ["pawns^2"]
    assert [r["label"] for r in comparisons[0]["runs"]] == ["mse a", "mse c"]


def test_run_colour_follows_input_position_not_rank(tmp_path: Path) -> None:
    better = curve("mse c")
    better["epochs"] = [
        {"epoch": 1, "train": 0.1, "validation": 0.05, "test": 0.05},
        {"epoch": 2, "train": 0.05, "validation": 0.02, "test": 0.02},
    ]
    curves = load_all(tmp_path, curve("mse a"), sigmoid("sigmoid b"), better, sigmoid("sigmoid d"))
    mse_group, sigmoid_group = training_curves.compare(curves)
    assert [r["label"] for r in mse_group["runs"]] == ["mse a", "mse c"]
    assert [r["colour"] for r in mse_group["runs"]] == ["--run-1", "--run-3"]
    assert [r["colour"] for r in sigmoid_group["runs"]] == ["--run-2", "--run-4"]


def test_each_run_panel_carries_the_colour_of_its_position(tmp_path: Path) -> None:
    curves = load_all(tmp_path, *(curve(f"run {i}") for i in range(1, 7)))
    colours = [r["colour"] for r in embedded_runs(training_curves.render_page(curves))]
    assert colours == ["--run-1", "--run-2", "--run-3", "--run-4", "--run-5", "--run-other"]


def test_run_colours_are_css_tokens_redefined_for_both_dark_guards(tmp_path: Path) -> None:
    page = training_curves.render_page(load_all(tmp_path, curve("a")))
    light = [f"--run-{i + 1}: {light};" for i, (light, _) in enumerate(training_curves.RUN_COLOURS)]
    dark = [f"--run-{i + 1}: {dark};" for i, (_, dark) in enumerate(training_curves.RUN_COLOURS)]
    assert [page.count(token) for token in light] == [1] * 5
    assert [page.count(token) for token in dark] == [2] * 5
    media = page.index("@media (prefers-color-scheme: dark)")
    guarded = page.index(':root:not([data-theme="light"])')
    forced = page.index(':root[data-theme="dark"]')
    assert media < guarded < page.index(dark[0]) < forced < page.rindex(dark[0])
    assert ".matches" not in page
    assert all(f"--{split}:" not in page for split in ("train", "validation", "test"))


@pytest.mark.parametrize(
    "constant, material, drawn",
    [(0.504, 0.3, True), (0.506, 0.3, False), (0.5, 0.304, False)],
)
def test_baselines_are_drawn_only_when_the_runs_agree_within_1pct(
    tmp_path: Path, constant: float, material: float, drawn: bool
) -> None:
    other = curve("mse b")
    other["baselines"] = {"constant": constant, "material": material}
    (comparison,) = training_curves.compare(load_all(tmp_path, curve("mse a"), other))
    assert (comparison["baselines"] is not None) == drawn
    assert any("baselines differ" in note for note in comparison["notes"]) == (not drawn)


def test_a_sixth_run_is_folded_out_with_a_caption(tmp_path: Path) -> None:
    curves = load_all(tmp_path, *(curve(f"run {i}") for i in range(1, 7)))
    (comparison,) = training_curves.compare(curves)
    assert [r["label"] for r in comparison["runs"]] == [f"run {i}" for i in range(1, 6)]
    assert comparison["folded"] == ["run 6"]
    assert any("run 6" in note for note in comparison["notes"])
    assert all("\N{EM DASH}" not in note for note in comparison["notes"])


def test_a_comparison_row_holds_the_numbers_at_the_best_epoch(tmp_path: Path) -> None:
    (comparison,) = training_curves.compare(load_all(tmp_path, curve("a"), curve("b")))
    row = comparison["runs"][0]
    assert (row["best_epoch"], row["validation_at_best"], row["test_at_best"], row["epochs_run"]) == (2, 0.06, 0.05, 3)


@pytest.mark.parametrize(
    "labels, short",
    [
        (
            ["from scratch, 3.2M positions, mse, lr 0.0003", "from scratch, 3.2M positions, mse, lr 0.001"],
            ["lr 0.0003", "lr 0.001"],
        ),
        (
            ["from scratch, 3.2M positions, mse, lr 0.0003", "from scratch, 3.2M mid-opening positions, mse loss"],
            ["3.2M positions, mse, lr 0.0003", "3.2M mid-opening positions, mse loss"],
        ),
        (
            ["sample (hand-made, not real)", "sample, lr 0.0001 (hand-made, not real)", "sample, lr 0.003 (hand-made, not real)"],
            ["sample (hand-made, not real)", "lr 0.0001", "lr 0.003"],
        ),
        (["run (seed 1), mse", "run (seed 2), mse"], ["(seed 1)", "(seed 2)"]),
        (["same", "same"], ["same", "same"]),
    ],
)
def test_short_labels_keep_every_part_that_differs_in_full(tmp_path: Path, labels: list[str], short: list[str]) -> None:
    (comparison,) = training_curves.compare(load_all(tmp_path, *map(curve, labels)))
    assert [r["short"] for r in comparison["runs"]] == short
    assert [r["label"] for r in comparison["runs"]] == labels


@pytest.mark.parametrize(
    "contents",
    [
        [*(curve(f"mse {i}") for i in range(1, 6)), sigmoid("sigmoid 6"), sigmoid("sigmoid 7")],
        [sigmoid("sigmoid 1"), *(curve(f"mse {i}") for i in range(2, 6)), sigmoid("sigmoid 6")],
    ],
)
def test_a_group_left_with_fewer_than_two_coloured_runs_gets_a_page_note_not_a_panel(
    tmp_path: Path, contents: list[dict[str, object]]
) -> None:
    curves = load_all(tmp_path, *contents)
    assert [c["units"] for c in training_curves.compare(curves)] == ["pawns^2"]
    (note,) = training_curves.page_notes(curves)
    assert "sigmoid 6" in note and "\N{EM DASH}" not in note
    assert embedded_json(training_curves.render_page(curves), "page-notes") == [note]


def test_a_lone_run_gets_neither_a_panel_nor_a_note(tmp_path: Path) -> None:
    curves = load_all(tmp_path, curve("mse a"), sigmoid("sigmoid b"))
    assert training_curves.compare(curves) == [] and training_curves.page_notes(curves) == []


def test_page_embeds_the_comparisons_above_the_run_panels(tmp_path: Path) -> None:
    curves = load_all(tmp_path, curve("a"), curve("b"))
    page = training_curves.render_page(curves)
    assert embedded_comparisons(page) == training_curves.compare(curves)
    assert page.index('id="comparisons"') < page.index('id="panels"')


# The chart code, run for real: the three sample curves in headless Chrome, at two widths and
# three themes. A probe script appended to the page reads back what Chart.js drew.


class Label(TypedDict):
    text: str
    lines: list[str]
    kind: str
    leader: bool
    x: float
    y: float
    w: float
    h: float


class Area(TypedDict):
    left: float
    right: float
    top: float
    bottom: float


class Line(TypedDict):
    run: int
    series: str
    color: str
    dash: list[float]
    width: float


class Marker(TypedDict):
    """A data point drawn with a radius, and the region Chart.js leaves visible around its dataset."""

    run: int
    series: str
    epoch: int
    x: float
    y: float
    radius: float
    style: str
    borderColor: str
    borderWidth: float
    backgroundColor: str
    seen: Area


class ChartProbe(TypedDict):
    kind: str
    width: float
    height: float
    area: Area
    labels: list[Label]
    datasets: list[Line]
    drawOrder: list[str]
    markers: list[Marker]
    strokes: list[list[float]] | None


class TableProbe(TypedDict):
    overflow: bool
    image: str
    attachment: str


class LegendEntry(TypedDict):
    text: str
    clipped: bool


class Probe(TypedDict):
    errors: list[str]
    chartError: str | None
    surface: str
    charts: list[ChartProbe | None]
    tables: list[TableProbe]
    legends: list[list[LegendEntry]]


class View(NamedTuple):
    name: str
    flags: tuple[str, ...]
    theme: str | None
    phone: bool
    colours: tuple[str, ...]


class Shown(NamedTuple):
    rendered: str | None
    canvases: int
    probe: Probe


LIGHT = tuple(light for light, _ in training_curves.RUN_COLOURS)
DARK = tuple(dark for _, dark in training_curves.RUN_COLOURS)
VIEWS = (
    View("desktop", (), None, False, LIGHT),
    View("phone", (), None, True, LIGHT),
    View("os-dark", ("--force-dark-mode",), None, False, DARK),
    View("theme-dark", (), "dark", True, DARK),
    View("theme-light-over-os-dark", ("--force-dark-mode",), "light", False, LIGHT),
)
LINE_GAP = 14
ERRORS = '<script>window.pageErrors = []; addEventListener("error", (e) => pageErrors.push(String(e.message)));</script>'
PROBE = """<script>
// The region Chart.js clips a dataset to (meta._clip holds its margins), cut to the canvas.
const seenRegion = (chart, meta) => {
  const c = meta._clip, a = chart.chartArea, w = chart.width, h = chart.height;
  if (!c || c.disabled) return { left: 0, right: w, top: 0, bottom: h };
  return {
    left: Math.max(0, c.left === false ? 0 : a.left - c.left),
    right: Math.min(w, c.right === false ? w : a.right + c.right),
    top: Math.max(0, c.top === false ? 0 : a.top - c.top),
    bottom: Math.min(h, c.bottom === false ? h : a.bottom + c.bottom),
  };
};
// Every segment the overlay plugin strokes or fills after the datasets, from one more redraw.
const overlayStrokes = (chart) => {
  const plugin = (chart.config.plugins || []).find((p) => p.id && p.id.endsWith("Overlay"));
  if (!plugin || !plugin.afterDatasetsDraw) return null;
  const ctx = chart.ctx, proto = CanvasRenderingContext2D.prototype, segments = [];
  let recording = false, path = [], at = null;
  const spy = (name, before) => { ctx[name] = (...a) => { before(...a); return proto[name].apply(ctx, a); }; };
  spy("beginPath", () => { path = []; at = null; });
  spy("moveTo", (x, y) => { at = [x, y]; });
  spy("lineTo", (x, y) => { if (at) path.push([...at, x, y]); at = [x, y]; });
  spy("arc", (x, y) => { path.push([x, y, x, y]); at = [x, y]; });
  const paint = () => { if (recording) segments.push(...path); };
  spy("stroke", paint);
  spy("fill", paint);
  const original = plugin.afterDatasetsDraw;
  plugin.afterDatasetsDraw = function (...a) {
    recording = true;
    try { return original.apply(this, a); } finally { recording = false; }
  };
  try { chart.draw(); } finally {
    plugin.afterDatasetsDraw = original;
    ["beginPath", "moveTo", "lineTo", "arc", "stroke", "fill"].forEach((name) => delete ctx[name]);
  }
  return segments;
};
setTimeout(() => {
  const surface = getComputedStyle(document.documentElement).getPropertyValue("--surface").trim();
  const out = { errors: window.pageErrors, chartError: document.body.dataset.chartError || null, surface, charts: [], tables: [], legends: [] };
  if (window.Chart) out.charts = [...document.querySelectorAll("canvas")].map((canvas) => {
    const chart = Chart.getChart(canvas);
    if (!chart) return null;
    const { left, right, top, bottom } = chart.chartArea;
    const markers = chart.data.datasets.flatMap((d, i) => {
      const meta = chart.getDatasetMeta(i), seen = seenRegion(chart, meta);
      return meta.data.flatMap((p, k) => (p.options.radius > 0 ? [{
        run: d.runIndex, series: d.series, epoch: d.data[k].x, x: p.x, y: p.y, radius: p.options.radius,
        style: p.options.pointStyle, borderColor: p.options.borderColor, borderWidth: p.options.borderWidth,
        backgroundColor: p.options.backgroundColor, seen,
      }] : []));
    });
    return {
      kind: canvas.closest("#comparisons") ? "comparison" : "run",
      width: chart.width, height: chart.height, area: { left, right, top, bottom },
      labels: chart.$labels || [],
      datasets: chart.data.datasets.map((d) => ({ run: d.runIndex, series: d.series, color: d.borderColor, dash: d.borderDash || [], width: d.borderWidth })),
      drawOrder: chart.getSortedVisibleDatasetMetas().map((m) => chart.data.datasets[m.index].series).reverse(),
      markers,
      strokes: overlayStrokes(chart),
    };
  });
  out.tables = [...document.querySelectorAll(".tablewrap")].map((w) => {
    const s = getComputedStyle(w);
    return { overflow: w.scrollWidth > w.clientWidth, image: s.backgroundImage, attachment: s.backgroundAttachment };
  });
  out.legends = [...document.querySelectorAll(".legend")].map((l) =>
    [...l.querySelectorAll("button")].map((b) => ({ text: b.textContent, clipped: b.scrollWidth > b.clientWidth })));
  const node = document.createElement("script");
  node.type = "application/json";
  node.id = "probe";
  node.textContent = JSON.stringify(out);
  document.body.append(node);
}, 2000);
</script>"""

chrome = pytest.mark.skipif(not CHROME.exists(), reason="Google Chrome is not installed")
views = pytest.mark.parametrize("view", VIEWS, ids=[v.name for v in VIEWS])


def sample_curves() -> list[training_curves.Curve]:
    return [training_curves.load_curve(p) for p in SAMPLES]


@functools.cache
def shown(view: View) -> Shown:
    """The sample page as Chrome rendered it for this view; Chart.js must come from its CDN."""
    html = training_curves.render_page(sample_curves())
    html = html.replace("<head>", f"<head>{ERRORS}", 1).replace("</body>", f"{PROBE}</body>", 1)
    if view.phone:
        html = html.replace("</head>", "<style>body { width: 375px; }</style></head>", 1)
    if view.theme:
        html = html.replace('<html lang="en">', f'<html lang="en" data-theme="{view.theme}">', 1)
    folder = Path(tempfile.mkdtemp())
    page = folder / f"{view.name}.html"
    page.write_text(html)
    dom = dump_dom([
        str(CHROME), "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
        "--disable-extensions", f"--user-data-dir={folder / 'profile'}", "--window-size=1400,3000",
        "--virtual-time-budget=8000", *view.flags, "--dump-dom", page.as_uri(),
    ])
    probe: Probe = json.loads(_probe_text(dom))
    if probe["chartError"]:
        pytest.skip(f"Chart.js CDN unreachable: {probe['chartError']}")
    body = re.search(r"<body[^>]*>", dom)
    rendered = re.search(r'data-rendered="(\d+)"', body.group(0)) if body else None
    return Shown(rendered.group(1) if rendered else None, dom.count("<canvas"), probe)


def _probe_text(dom: str) -> str:
    match = re.search(r'<script type="application/json" id="probe">(.*?)</script>', dom, re.S)
    assert match is not None, "the probe never ran"
    return match.group(1)


def dump_dom(args: list[str]) -> str:
    """Chrome's serialized DOM; Chrome can linger after printing it, so stop reading at </html>."""
    proc = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    watchdog = threading.Timer(60, proc.kill)
    watchdog.start()
    lines: list[str] = []
    try:
        assert proc.stdout is not None
        for line in proc.stdout:
            lines.append(line)
            if "</html>" in line:
                break
    finally:
        watchdog.cancel()
        proc.kill()
        proc.wait()
    return "".join(lines)


def drawn(view: View) -> list[ChartProbe]:
    charts = shown(view).probe["charts"]
    assert all(c is not None for c in charts)
    return [c for c in charts if c is not None]


@chrome
@views
def test_every_canvas_gets_a_chart(view: View) -> None:
    result = shown(view)
    assert result.probe["errors"] == []
    assert result.canvases == len(SAMPLES) + len(training_curves.compare(sample_curves()))
    assert result.rendered == str(result.canvases)


@chrome
@views
def test_no_label_lands_on_the_plot(view: View) -> None:
    for chart in drawn(view):
        area = chart["area"]
        gutter = sorted((l for l in chart["labels"] if l["kind"] != "top"), key=lambda l: l["y"])
        assert gutter, "every chart labels its line ends"
        for label in gutter:
            assert label["x"] > area["right"]
            assert label["x"] + label["w"] <= chart["width"] + 0.5
            assert label["y"] - label["h"] / 2 >= -0.5 and label["y"] + label["h"] / 2 <= chart["height"] + 0.5
        for above, below in zip(gutter, gutter[1:]):
            assert below["y"] - above["y"] >= max(LINE_GAP, (above["h"] + below["h"]) / 2) - 0.01
        for label in (l for l in chart["labels"] if l["kind"] == "top"):
            assert label["y"] + label["h"] / 2 <= area["top"]
            assert area["left"] - 0.5 <= label["x"] and label["x"] + label["w"] <= area["right"] + 0.5


def strictly_inside(area: Area, x: float, y: float) -> bool:
    return area["left"] < x < area["right"] and area["top"] < y < area["bottom"]


@chrome
@views
def test_the_label_plugin_strokes_nothing_on_the_plot(view: View) -> None:
    for chart in drawn(view):
        strokes = chart["strokes"]
        assert strokes, "the probe caught the label plugin's strokes"
        for x1, y1, x2, y2 in strokes:
            steps = (k / 50 for k in range(51))
            assert not any(strictly_inside(chart["area"], x1 + (x2 - x1) * t, y1 + (y2 - y1) * t) for t in steps), (
                f"{chart['kind']} chart: stroke ({x1:.1f}, {y1:.1f}) to ({x2:.1f}, {y2:.1f}) crosses the plot")


def compared(view: View) -> list[ChartProbe]:
    return [c for c in drawn(view) if c["kind"] == "comparison"]


def validation_markers(chart: ChartProbe) -> list[Marker]:
    return [m for m in chart["markers"] if m["series"] == "validation"]


@chrome
@views
def test_each_best_epoch_dot_is_a_plain_disc_in_its_run_colour(view: View) -> None:
    best = {r["index"]: r["best_epoch"] for c in training_curves.compare(sample_curves()) for r in c["runs"]}
    for chart in compared(view):
        dots = {m["run"]: m for m in validation_markers(chart) if m["epoch"] == best[m["run"]]}
        assert sorted(dots) == sorted(best)
        for run, dot in dots.items():
            assert dot["borderColor"] != shown(view).probe["surface"], "a surface ring cuts the lines under the dot"
            colour = view.colours[run]
            assert (dot["style"], dot["radius"], dot["borderColor"], dot["backgroundColor"]) == ("circle", 4, colour, colour)


@chrome
@views
def test_no_marker_is_cut_at_the_plot_edge(view: View) -> None:
    for chart in compared(view):
        markers = validation_markers(chart)
        assert any(abs(m["x"] - chart["area"]["right"]) < 0.5 for m in markers), "a sample best epoch is its last"
        for m in markers:
            seen, r = m["seen"], m["radius"]
            assert seen["left"] <= m["x"] - r and m["x"] + r <= seen["right"], m
            assert seen["top"] <= m["y"] - r and m["y"] + r <= seen["bottom"], m


@chrome
@views
def test_a_run_that_stops_early_just_ends_and_its_label_has_no_leader(view: View) -> None:
    # A mark at the end of an early run sits inside the plot and paints over
    # whatever line passes there; the line simply ending is enough.
    curves = sample_curves()
    (comparison,) = training_curves.compare(curves)
    last = {r["index"]: curves[r["index"]]["epochs"][-1]["epoch"] for r in comparison["runs"]}
    early = {run for run, epoch in last.items() if epoch < max(last.values())}
    assert early, "a sample run stops before the others"
    for chart in compared(view):
        assert [m for m in validation_markers(chart) if m["style"] == "line"] == []
        leaders = {l["text"]: l["leader"] for l in chart["labels"] if l["kind"] == "end"}
        assert leaders == {r["short"]: r["index"] not in early for r in comparison["runs"]}


@chrome
@views
def test_comparison_labels_and_legend_name_what_differs_in_full(view: View) -> None:
    shorts = [[r["short"] for r in c["runs"]] for c in training_curves.compare(sample_curves())]
    assert shorts == [["sample (hand-made values, not a real run)", "lr 0.0001", "lr 0.003"]]
    compared = [c for c in drawn(view) if c["kind"] == "comparison"]
    for chart, expected in zip(compared, shorts, strict=True):
        ends = [l for l in chart["labels"] if l["kind"] == "end"]
        assert sorted(l["text"] for l in ends) == sorted(expected)
        assert all(" ".join(l["lines"]) == l["text"] for l in chart["labels"])
    legends = shown(view).probe["legends"]
    assert [[e["text"] for e in legend] for legend in legends] == shorts
    assert not any(e["clipped"] for legend in legends for e in legend)


@chrome
@views
def test_one_colour_is_one_run_and_the_line_style_is_the_split(view: View) -> None:
    colours: dict[int, set[str]] = {}
    for chart in drawn(view):
        for line in chart["datasets"]:
            colours.setdefault(line["run"], set()).add(line["color"])
            style = {"validation": ([], 2), "train": ([6, 4], 2), "test": ([0, 5], 2.5)}[line["series"]]
            assert (line["dash"], line["width"]) == style
        if chart["kind"] == "run":
            assert chart["drawOrder"][-1] == "test"
    assert colours == {i: {view.colours[i]} for i in range(len(SAMPLES))}


@chrome
@views
def test_every_table_box_shows_a_scroll_shadow_when_its_table_overflows(view: View) -> None:
    # Shadows attached to the scrolling box, covers attached to its content: the shadow shows only on overflow.
    tables = shown(view).probe["tables"]
    assert tables
    for table in tables:
        assert "gradient" in table["image"] and "local" in table["attachment"]

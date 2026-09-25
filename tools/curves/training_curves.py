"""Turn the curve files written by pytorch/train.py --curve into one HTML page.

Each run gets its own panel. Runs in the same units are also drawn together,
one comparison panel per units value; runs in different units never share an
axis. A colour always names one run, on every panel; the line style names the
data split. The data is embedded in the page, so the file opens anywhere; only
Chart.js comes from a CDN.

Reading order: the contract types, load_curve, summarize, compare, page_notes,
render_page, main, then the comparison and validation helpers and the page
template.

Usage: training_curves.py OUT.html CURVE.json [CURVE.json ...]
"""

import json
import math
import re
import sys
from pathlib import Path
from typing import TypedDict

LOSSES = ("mse", "sigmoid")
UNITS = ("pawns^2", "win-probability^2")
UNIT_TEXT = {"pawns^2": "pawns²", "win-probability^2": "win-probability²"}
SERIES = ("train", "validation", "test")
USAGE = "usage: training_curves.py OUT.html CURVE.json [CURVE.json ...]"
# One colour per run, by the run's position on the command line; (light, dark). The page
# reads them as the CSS tokens --run-1.. so a theme switch never needs the page script.
RUN_COLOURS = (
    ("#2a78d6", "#3987e5"),
    ("#eb6834", "#d95926"),
    ("#1baf7a", "#199e70"),
    ("#eda100", "#c98500"),
    ("#e87ba4", "#d55181"),
)
# A run past the palette never shares a panel, so it takes the text colour rather than a reused hue.
OTHER_RUN = "--run-other"
BASELINE_TOLERANCE = 0.01


class CurveError(ValueError):
    """A curve file that does not follow the contract."""


class Baselines(TypedDict):
    constant: float
    material: float


class Epoch(TypedDict):
    epoch: int
    train: float
    validation: float
    test: float


class Curve(TypedDict):
    label: str
    loss: str
    units: str
    baselines: Baselines
    best_epoch: int
    epochs: list[Epoch]


class Summary(TypedDict):
    final_epoch: int
    final_test: float
    test_at_best: float
    explained_pct: float


class ComparedRun(TypedDict):
    index: int
    label: str
    short: str
    colour: str
    best_epoch: int
    validation_at_best: float
    test_at_best: float
    epochs_run: int


class Comparison(TypedDict):
    units: str
    runs: list[ComparedRun]
    folded: list[str]
    baselines: Baselines | None
    notes: list[str]


def load_curve(path: Path) -> Curve:
    """Read and validate one curve file; any contract violation raises CurveError naming the file."""
    try:
        raw: object = json.loads(path.read_text())
    except json.JSONDecodeError as err:
        raise CurveError(f"{path}: not valid JSON: {err}") from err
    return _parse_curve(raw, str(path))


def summarize(curve: Curve) -> Summary:
    """The caption numbers. A constant predictor's loss is the target variance, hence the ratio."""
    final = curve["epochs"][-1]
    best = _best(curve)
    return {
        "final_epoch": final["epoch"],
        "final_test": final["test"],
        "test_at_best": best["test"],
        "explained_pct": 100 * (1 - best["test"] / curve["baselines"]["constant"]),
    }


def compare(curves: list[Curve]) -> list[Comparison]:
    """One comparison per units value with two coloured runs or more, in order of first appearance.

    A run keeps the colour of its command-line position; a run past the last colour is folded
    out and named in a note. Baselines are drawn only when every drawn run agrees on them,
    since runs measured on different test sets must not look comparable.
    """
    return [_comparison(units, drawn, folded, curves) for units, (drawn, folded) in _groups(curves).items() if len(drawn) >= 2]


def page_notes(curves: list[Curve]) -> list[str]:
    """Why a units group of two runs or more got no comparison panel: folding left under two runs."""
    notes = []
    for units, (drawn, folded) in _groups(curves).items():
        if len(drawn) < 2 and len(drawn) + len(folded) >= 2:
            names = ", ".join(curves[i]["label"] for i in folded)
            verb = "has" if len(folded) == 1 else "have"
            notes.append(
                f"No {UNIT_TEXT[units]} comparison: {names} {verb} no run colour (the palette has "
                f"{len(RUN_COLOURS)}), which leaves fewer than two runs to draw together. Each run has its own panel below."
            )
    return notes


def run_colour(index: int) -> str:
    """The CSS token of the run at this command-line position, on every panel of the page."""
    return f"--run-{index + 1}" if index < len(RUN_COLOURS) else OTHER_RUN


def render_page(curves: list[Curve]) -> str:
    runs = [{"curve": c, "summary": summarize(c), "colour": run_colour(i)} for i, c in enumerate(curves)]
    return (
        _TEMPLATE.replace("__LIGHT_RUN_TOKENS__", _run_tokens(0))
        .replace("__DARK_TOKENS__", _DARK_TOKENS + _run_tokens(1))
        .replace("__RUNS_JSON__", _script_safe_json(runs))
        .replace("__COMPARISONS_JSON__", _script_safe_json(compare(curves)))
        .replace("__PAGE_NOTES_JSON__", _script_safe_json(page_notes(curves)))
    )


def main(argv: list[str] | None = None) -> None:
    args = sys.argv[1:] if argv is None else argv
    if len(args) < 2:
        raise SystemExit(USAGE)
    out = Path(args[0])
    curves = [load_curve(Path(p)) for p in args[1:]]
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(render_page(curves))
    epochs = sum(len(c["epochs"]) for c in curves)
    print(f"wrote {out}: {len(curves)} run(s), {epochs} epoch(s), {len(compare(curves))} comparison(s)")


# Comparison: grouping, colours, shared baselines and the direct-label text.


def _best(curve: Curve) -> Epoch:
    return next(e for e in curve["epochs"] if e["epoch"] == curve["best_epoch"])


def _groups(curves: list[Curve]) -> dict[str, tuple[list[int], list[int]]]:
    """Run positions per units value, split into those with a run colour and those folded out."""
    groups: dict[str, tuple[list[int], list[int]]] = {}
    for index, curve in enumerate(curves):
        drawn, folded = groups.setdefault(curve["units"], ([], []))
        (drawn if index < len(RUN_COLOURS) else folded).append(index)
    return groups


def _comparison(units: str, drawn: list[int], folded_at: list[int], curves: list[Curve]) -> Comparison:
    folded = [curves[i]["label"] for i in folded_at]
    shorts = _short_labels([curves[i]["label"] for i in drawn])
    runs: list[ComparedRun] = []
    for i, short in zip(drawn, shorts):
        curve, best = curves[i], _best(curves[i])
        runs.append({
            "index": i,
            "label": curve["label"],
            "short": short,
            "colour": run_colour(i),
            "best_epoch": curve["best_epoch"],
            "validation_at_best": best["validation"],
            "test_at_best": best["test"],
            "epochs_run": len(curve["epochs"]),
        })
    notes: list[str] = []
    baselines: Baselines | None = curves[drawn[0]]["baselines"] if drawn else None
    if not _baselines_agree([curves[i]["baselines"] for i in drawn]):
        baselines = None
        notes.append(
            "The runs' baselines differ by more than 1%, so they were measured on different test "
            "positions: no baseline line is drawn and the losses are not directly comparable."
        )
    if folded:
        notes.append(
            f"Not drawn here, the palette has {len(RUN_COLOURS)} run colours: {', '.join(folded)}. "
            "Each still has its own panel below."
        )
    return {"units": units, "runs": runs, "folded": folded, "baselines": baselines, "notes": notes}


def _baselines_agree(baselines: list[Baselines]) -> bool:
    for key in ("constant", "material"):
        values = [b[key] for b in baselines]
        if values and max(values) - min(values) > BASELINE_TOLERANCE * min(values):
            return False
    return True


def _short_labels(labels: list[str]) -> list[str]:
    """Each label's parts that not every label has, whole; a label with no such part stays whole."""
    parts = [_label_parts(label) for label in labels]
    shared = set.intersection(*({text for _, text in p} for p in parts)) if parts else set()
    kept = [[(sep, text) for sep, text in p if text not in shared] for p in parts]
    return [_joined(k) if k else label for k, label in zip(kept, labels)]


def _joined(parts: list[tuple[str, str]]) -> str:
    """The parts back as text, without the separator the first one had in its label."""
    return parts[0][1] + "".join(sep + text for sep, text in parts[1:])


def _label_parts(label: str) -> list[tuple[str, str]]:
    """(separator, text) pairs cut at ', ' and before ' (', never inside parentheses."""
    masked = re.sub(r"\([^()]*\)", lambda m: m.group().replace(",", "\0"), label)
    pieces = re.split(r"(, | (?=\())", masked)
    return [(sep, text.replace("\0", ",")) for sep, text in zip(["", *pieces[1::2]], pieces[::2])]


def _run_tokens(scheme: int) -> str:
    """The --run-N custom properties for one scheme of RUN_COLOURS: 0 light, 1 dark."""
    return "".join(f"    --run-{i + 1}: {pair[scheme]};\n" for i, pair in enumerate(RUN_COLOURS))


# Validation: every field of the contract, checked once at the file boundary.


def _parse_curve(raw: object, where: str) -> Curve:
    obj = _object(raw, where)
    baselines = _object(_field(obj, "baselines", where), f"{where}: baselines")
    epochs_raw = _field(obj, "epochs", where)
    if not isinstance(epochs_raw, list):
        raise CurveError(f"{where}: 'epochs' must be a list")
    epochs = [_parse_epoch(e, f"{where}: epochs[{i}]") for i, e in enumerate(epochs_raw)]
    best_epoch = _integer(obj, "best_epoch", where)
    if best_epoch not in {e["epoch"] for e in epochs}:
        raise CurveError(f"{where}: best_epoch {best_epoch} is not one of the epochs")
    return {
        "label": _string(obj, "label", where),
        "loss": _choice(obj, "loss", LOSSES, where),
        "units": _choice(obj, "units", UNITS, where),
        "baselines": {
            "constant": _loss(baselines, "constant", f"{where}: baselines"),
            "material": _loss(baselines, "material", f"{where}: baselines"),
        },
        "best_epoch": best_epoch,
        "epochs": epochs,
    }


def _parse_epoch(raw: object, where: str) -> Epoch:
    obj = _object(raw, where)
    return {
        "epoch": _integer(obj, "epoch", where),
        "train": _loss(obj, "train", where),
        "validation": _loss(obj, "validation", where),
        "test": _loss(obj, "test", where),
    }


def _object(value: object, where: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise CurveError(f"{where}: expected a JSON object")
    return value


def _field(obj: dict[str, object], key: str, where: str) -> object:
    if key not in obj:
        raise CurveError(f"{where}: missing '{key}'")
    return obj[key]


def _string(obj: dict[str, object], key: str, where: str) -> str:
    value = _field(obj, key, where)
    if not isinstance(value, str):
        raise CurveError(f"{where}: '{key}' must be a string, got {value!r}")
    return value


def _choice(obj: dict[str, object], key: str, allowed: tuple[str, ...], where: str) -> str:
    value = _string(obj, key, where)
    if value not in allowed:
        raise CurveError(f"{where}: '{key}' must be one of {allowed}, got {value!r}")
    return value


def _integer(obj: dict[str, object], key: str, where: str) -> int:
    value = _field(obj, key, where)
    if isinstance(value, bool) or not isinstance(value, int):
        raise CurveError(f"{where}: '{key}' must be an integer, got {value!r}")
    return value


def _loss(obj: dict[str, object], key: str, where: str) -> float:
    """Positive and finite: the page plots losses on a log axis, where zero and NaN have no place."""
    value = _field(obj, key, where)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise CurveError(f"{where}: '{key}' must be a positive finite number, got {value!r}")
    return float(value)


def _script_safe_json(value: object) -> str:
    """JSON that cannot end its <script> element early: every '<' becomes its \\u escape."""
    return json.dumps(value, allow_nan=False).replace("<", "\\u003c")


_DARK_TOKENS = """    --bg: #1a1a19;
    --surface: #1a1a19;
    --text: #f0efe9;
    --text-2: #c3c2b7;
    --grid: #33322f;
    --border: #3d3c39;
    --shade: rgba(240, 239, 233, 0.2);
    color-scheme: dark;
"""

_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Training curves</title>
<style>
:root {
  --bg: #fcfcfb;
  --surface: #fcfcfb;
  --text: #1a1a19;
  --text-2: #52514e;
  --grid: #e6e5e1;
  --border: #dddcd7;
  --shade: rgba(26, 26, 25, 0.22);
__LIGHT_RUN_TOKENS__    --run-other: var(--text);
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
__DARK_TOKENS__  }
}
:root[data-theme="dark"] {
__DARK_TOKENS__}
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 24px 16px 48px;
  background: var(--bg);
  color: var(--text);
  font: 15px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
main { max-width: 1280px; margin: 0 auto; }
h1 { font-size: 24px; margin: 0 0 4px; }
.lede { color: var(--text-2); margin: 0 0 24px; max-width: 70ch; }
.panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 440px), 1fr));
  gap: 24px;
}
.comparisons { display: grid; gap: 24px; margin-bottom: 24px; }
.comparisons .plot { height: 380px; }
.comparisons > .caption { margin: 0; }
.swatch { display: inline-block; width: 16px; height: 3px; border-radius: 2px; margin-right: 8px; vertical-align: middle; background: var(--c); }
.panel { min-width: 0; border: 1px solid var(--border); border-radius: 8px; padding: 16px; background: var(--surface); }
.panel h2 { font-size: 16px; margin: 0; overflow-wrap: anywhere; }
.meta { color: var(--text-2); font-size: 13px; margin: 2px 0 8px; }
.key, .legend { display: flex; flex-wrap: wrap; gap: 2px 16px; margin: 0 0 8px; font-size: 13px; }
.key { color: var(--text-2); }
.key .line { display: inline-block; width: 24px; margin-right: 6px; vertical-align: middle; border-top: 2px solid var(--text-2); }
.key .dashed { border-top-style: dashed; }
.key .dotted { border-top: 3px dotted var(--text-2); }
.legend button {
  max-width: 100%;
  padding: 2px 0;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  text-align: left;
  overflow-wrap: anywhere;
  cursor: pointer;
}
.legend button[aria-pressed="false"] { color: var(--text-2); text-decoration: line-through; }
.legend button:focus-visible { outline: 2px solid var(--text); outline-offset: 2px; }
.plot { position: relative; height: 320px; }
.caption { color: var(--text-2); font-size: 13px; margin: 12px 0 0; }
.caption strong { color: var(--text); font-weight: 600; }
.nochart { color: var(--text-2); font-size: 13px; }
details { margin-top: 32px; }
summary { cursor: pointer; font-weight: 600; }
/* The covers scroll with the table and the shades stay on the box edges, so an edge is shaded
   only while more of the table lies past it. */
.tablewrap {
  overflow-x: auto;
  margin-top: 16px;
  background:
    linear-gradient(to right, var(--surface) 40%, transparent) left / 32px 100% no-repeat local,
    linear-gradient(to left, var(--surface) 40%, transparent) right / 32px 100% no-repeat local,
    linear-gradient(to right, var(--shade), transparent) left / 12px 100% no-repeat scroll,
    linear-gradient(to left, var(--shade), transparent) right / 12px 100% no-repeat scroll;
}
table { border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; }
caption { text-align: left; font-weight: 600; padding: 0 0 6px; overflow-wrap: anywhere; }
th, td { padding: 4px 12px 4px 0; text-align: right; border-bottom: 1px solid var(--grid); }
td { white-space: nowrap; }
.runcol { min-width: 11em; }
th:first-child, td:first-child { text-align: left; }
thead th { color: var(--text-2); font-weight: 600; }
tr.best td, tr.best th { font-weight: 600; }
</style>
</head>
<body>
<main>
<h1>Training curves</h1>
<p class="lede">Runs sharing loss units are compared first on one axis, then each run gets its own panel, log scale throughout. A colour names one run on every panel; the line style names the data split: solid validation, dashed training, dotted test. Baselines are measured on the test positions in the run's loss space; the best epoch is the one whose weights were restored, chosen on validation.</p>
<div class="comparisons" id="comparisons"></div>
<div class="panels" id="panels"></div>
<details>
<summary>Table view: every epoch, per run</summary>
<div id="tables"></div>
</details>
</main>
<script type="application/json" id="runs">__RUNS_JSON__</script>
<script type="application/json" id="comparison-data">__COMPARISONS_JSON__</script>
<script type="application/json" id="page-notes">__PAGE_NOTES_JSON__</script>
<script src="https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.1/chart.umd.min.js"></script>
<script>
(() => {
  const data = (id) => JSON.parse(document.getElementById(id).textContent);
  const runs = data("runs");
  const comparisons = data("comparison-data");
  const notes = data("page-notes");
  const SERIES = ["train", "validation", "test"];
  // The colour names the run and the line style names the data split, on every panel.
  // Chart.js draws the lowest order last, so the dotted test line stays on top of validation.
  const SPLITS = {
    validation: { name: "validation", key: "solid", style: { borderWidth: 2, borderDash: [], borderCapStyle: "round", order: 1 } },
    train: { name: "training", key: "dashed", style: { borderWidth: 2, borderDash: [6, 4], borderCapStyle: "butt", order: 2 } },
    test: { name: "test", key: "dotted", style: { borderWidth: 2.5, borderDash: [0, 5], borderCapStyle: "round", order: 0 } },
  };
  const UNITS = { "pawns^2": "pawns\\u00b2", "win-probability^2": "win-probability\\u00b2" };
  const BASELINES = [["constant", "constant (mean)"], ["material", "material only (P1 N3 B3 R5 Q9)"]];
  const LABEL_FONT = "12px system-ui, -apple-system, sans-serif";
  const LINE = 14; // label line height, and the least distance between two label centres
  const LEADER = 16; // room for the leader line between the plot and the gutter labels
  const MARK = 14; // a run-colour stroke before a comparison label, since not every label has a leader
  const DOT = 4; // best-epoch dot radius; a leader starts clear of a dot on the plot's right edge
  const fmt = (v) => Number(v.toPrecision(3)).toString();
  const lastEpoch = (curve) => curve.epochs[curve.epochs.length - 1].epoch;
  const token = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const el = (tag, attrs, text) => {
    const node = document.createElement(tag);
    Object.entries(attrs || {}).forEach(([k, v]) => node.setAttribute(k, v));
    if (text !== undefined) node.textContent = text;
    return node;
  };
  const measure = document.createElement("canvas").getContext("2d");
  const textWidth = (text) => {
    measure.font = LABEL_FONT;
    return measure.measureText(text).width;
  };
  const swatch = (colour) => el("span", { class: "swatch", style: `--c: var(${colour})` });
  const key = (splits) => {
    const p = el("p", { class: "key" });
    splits.forEach((s) => {
      const item = el("span");
      item.append(el("span", { class: `line ${SPLITS[s].key}` }), SPLITS[s].name);
      p.append(item);
    });
    return p;
  };

  const comparisonsRoot = document.getElementById("comparisons");
  const panelsRoot = document.getElementById("panels");
  const tablesRoot = document.getElementById("tables");

  // Runs the reader switched off, per comparison; kept when a theme change redraws the charts.
  const hidden = comparisons.map(() => new Set());
  let compareCharts = [];
  const toggleRun = (n, index, button) => {
    const off = !hidden[n].has(index);
    if (off) hidden[n].add(index);
    else hidden[n].delete(index);
    button.setAttribute("aria-pressed", String(!off));
    const chart = compareCharts[n];
    if (!chart) return;
    chart.data.datasets.forEach((ds, i) => { if (ds.runIndex === index) chart.setDatasetVisibility(i, !off); });
    chart.update();
  };

  const compareCanvases = comparisons.map((cmp, n) => {
    const units = UNITS[cmp.units];
    const panel = el("section", { class: "panel" });
    panel.append(el("h2", {}, `Validation and training loss, ${units}`));
    panel.append(el("p", { class: "meta" }, `${cmp.runs.length} runs on one axis, log scale; the dot marks each run's best epoch`));
    panel.append(key(["validation", "train"]));
    const legend = el("div", { class: "legend", role: "group", "aria-label": "Runs: press one to hide or show it" });
    cmp.runs.forEach((r) => {
      const button = el("button", { type: "button", "aria-pressed": "true", title: r.label });
      button.append(swatch(r.colour), r.short);
      button.addEventListener("click", () => toggleRun(n, r.index, button));
      legend.append(button);
    });
    panel.append(legend);
    const plot = el("div", { class: "plot" });
    const canvas = el("canvas", { role: "img", "aria-label": `Validation and training loss per epoch for ${cmp.runs.map((r) => r.label).join("; ")}` });
    plot.append(canvas);
    panel.append(plot);
    cmp.notes.forEach((note) => panel.append(el("p", { class: "caption" }, note)));
    const wrap = el("div", { class: "tablewrap" });
    const table = el("table");
    const head = el("tr");
    ["run", "best epoch", `validation at best (${units})`, `test at best (${units})`, "epochs run"]
      .forEach((h) => head.append(el("th", { scope: "col" }, h)));
    table.appendChild(el("thead")).append(head);
    const body = table.appendChild(el("tbody"));
    cmp.runs.forEach((r) => {
      const name = el("th", { scope: "row", class: "runcol" });
      name.append(swatch(r.colour), r.label);
      const row = el("tr");
      row.append(name, el("td", {}, String(r.best_epoch)), el("td", {}, fmt(r.validation_at_best)),
        el("td", {}, fmt(r.test_at_best)), el("td", {}, String(r.epochs_run)));
      body.append(row);
    });
    wrap.append(table);
    panel.append(wrap);
    comparisonsRoot.append(panel);
    return canvas;
  });
  notes.forEach((note) => comparisonsRoot.append(el("p", { class: "caption" }, note)));

  const canvases = runs.map((run) => {
    const c = run.curve, s = run.summary, units = UNITS[c.units];
    const panel = el("section", { class: "panel" });
    const title = el("h2");
    title.append(swatch(run.colour), c.label);
    panel.append(title);
    panel.append(el("p", { class: "meta" }, `loss ${c.loss}, ${units}, log scale`));
    panel.append(key(["validation", "train", "test"]));
    const plot = el("div", { class: "plot" });
    const canvas = el("canvas", { role: "img", "aria-label": `Train, validation and test loss per epoch for ${c.label}` });
    plot.append(canvas);
    panel.append(plot);
    const cap = el("p", { class: "caption" });
    const bold = (t) => el("strong", {}, t);
    cap.append(
      "Final test loss ", bold(fmt(s.final_test)), ` ${units} (epoch ${s.final_epoch}). Best epoch `,
      bold(String(c.best_epoch)), ", test loss there ", bold(fmt(s.test_at_best)),
      ". Network explains ", bold(`${s.explained_pct.toFixed(1)}%`), " of the test variance.");
    panel.append(cap);
    panelsRoot.append(panel);

    const wrap = el("div", { class: "tablewrap" });
    const table = el("table");
    const caption = el("caption");
    caption.append(swatch(run.colour), `${c.label} (${units})`);
    table.append(caption);
    const head = el("tr");
    ["epoch", ...SERIES].forEach((h) => head.append(el("th", { scope: "col" }, h)));
    table.appendChild(el("thead")).append(head);
    const body = table.appendChild(el("tbody"));
    c.epochs.forEach((e) => {
      const best = e.epoch === c.best_epoch;
      const row = el("tr", best ? { class: "best" } : {});
      row.append(el("th", { scope: "row" }, best ? `${e.epoch} (best)` : String(e.epoch)));
      SERIES.forEach((k) => row.append(el("td", {}, fmt(e[k]))));
      body.append(row);
    });
    wrap.append(table);
    tablesRoot.append(wrap);
    return canvas;
  });

  if (!window.Chart) {
    document.body.dataset.chartError = "Chart.js did not load";
    document.querySelectorAll(".plot").forEach((p) => p.replaceChildren(
      el("p", { class: "nochart" }, "Chart.js did not load (offline?). The table view below has every number.")));
    return;
  }

  const isLabelledTick = (v) => {
    const m = v / 10 ** Math.floor(Math.log10(v));
    return [1, 2, 5].some((k) => Math.abs(m - k) < 1e-6);
  };

  // Lines no wider than max: a parenthetical stays whole when it fits, a word is never cut.
  const wrapText = (text, max) => {
    const chunks = (text.match(/\\([^)]*\\)|[^\\s(]+/g) || [text])
      .flatMap((c) => (textWidth(c) > max ? c.split(" ") : [c]));
    return chunks.reduce((lines, c) => {
      const last = lines[lines.length - 1];
      if (last !== undefined && textWidth(`${last} ${c}`) <= max) lines[lines.length - 1] = `${last} ${c}`;
      else lines.push(c);
      return lines;
    }, []);
  };

  // Right padding that holds the widest label, at most 35% of the canvas but never below its longest word.
  const gutter = (texts, canvasWidth, indent) => {
    const longestWord = Math.max(...texts.flatMap((t) => t.split(" ")).map(textWidth));
    const cap = Math.max(longestWord, canvasWidth * 0.35);
    return Math.ceil(Math.min(Math.max(...texts.map(textWidth)), cap)) + LEADER + indent + 2;
  };

  // Heights y for labels sorted by anchor ay: as near their anchors as the others allow, never
  // closer than half their heights added, inside [top, bottom].
  const stack = (labels, top, bottom) => {
    labels.sort((a, b) => a.ay - b.ay);
    labels.forEach((l, i) => {
      const prev = labels[i - 1];
      l.y = Math.max(l.ay, prev ? prev.y + (prev.h + l.h) / 2 : top + l.h / 2);
    });
    for (let i = labels.length - 1; i >= 0; i -= 1) {
      const l = labels[i], next = labels[i + 1];
      l.y = Math.min(l.y, next ? next.y - (next.h + l.h) / 2 : bottom - l.h / 2);
    }
    return labels;
  };

  // Labels ({ text, ax, ay, color, mark, kind }) in the right gutter, off the plot. Nothing is
  // drawn on the plot after the data: a leader runs in the gutter only, so only a label whose
  // anchor sits on the plot's right edge gets one. The gutter stops above the x tick labels.
  const drawGutter = (chart, ink, items) => {
    const { ctx, chartArea: area } = chart;
    const x = area.right + LEADER;
    const labels = stack(items.map((item) => {
      const indent = item.mark ? MARK : 0;
      const lines = wrapText(item.text, chart.width - x - indent - 1);
      const leader = item.ax >= area.right - 0.5;
      return { ...item, indent, lines, leader, h: lines.length * LINE, w: indent + Math.max(...lines.map(textWidth)) };
    }), 0, area.bottom + 4);
    ctx.save();
    ctx.font = LABEL_FONT;
    ctx.setLineDash([]);
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    labels.forEach((l) => {
      const top = l.y - l.h / 2 + LINE / 2;
      if (l.leader) {
        ctx.globalAlpha = 0.6;
        ctx.lineWidth = 1;
        ctx.strokeStyle = ink.text2;
        ctx.beginPath(); ctx.moveTo(area.right + DOT + 2, l.ay); ctx.lineTo(x - 3, l.y); ctx.stroke();
        ctx.globalAlpha = 1;
      }
      if (l.mark) {
        ctx.lineWidth = 3;
        ctx.strokeStyle = l.mark;
        ctx.beginPath(); ctx.moveTo(x, top); ctx.lineTo(x + MARK - 4, top); ctx.stroke();
      }
      ctx.fillStyle = l.color;
      l.lines.forEach((text, k) => ctx.fillText(text, x + l.indent, top + LINE * k));
    });
    ctx.restore();
    return labels.map(({ text, lines, kind, leader, y, w, h }) => ({ text, lines, kind, leader, x, y, w, h }));
  };

  const endLabel = (chart, i, text, color, mark) => {
    const points = chart.getDatasetMeta(i).data;
    const p = points[points.length - 1];
    return { text, ax: p.x, ay: p.y, color, mark, kind: "end" };
  };

  const baselineLabels = (chart, ink, baselines) => {
    const { chartArea: area, scales: { y } } = chart;
    return BASELINES.flatMap(([k, text]) => {
      const py = y.getPixelForValue(baselines[k]);
      return py >= area.top && py <= area.bottom ? [{ text, ax: area.right, ay: py, color: ink.text2, kind: "baseline" }] : [];
    });
  };

  // Baselines and the best-epoch line, drawn under the data.
  const referenceLines = (chart, ink, baselines, bestEpoch) => {
    const { ctx, chartArea: area, scales: { x, y } } = chart;
    ctx.save();
    ctx.lineWidth = 1;
    ctx.strokeStyle = ink.text2;
    ctx.setLineDash([10, 4]);
    if (baselines) BASELINES.forEach(([k]) => {
      const py = y.getPixelForValue(baselines[k]);
      if (py < area.top || py > area.bottom) return;
      ctx.beginPath(); ctx.moveTo(area.left, py); ctx.lineTo(area.right, py); ctx.stroke();
    });
    if (bestEpoch !== undefined) {
      const bx = x.getPixelForValue(bestEpoch);
      ctx.beginPath(); ctx.moveTo(bx, area.top); ctx.lineTo(bx, area.bottom); ctx.stroke();
    }
    ctx.restore();
  };

  // The best-epoch caption, in the top padding above its line.
  const bestLabel = (chart, ink, epoch) => {
    const { ctx, chartArea: area, scales: { x } } = chart;
    const text = "best (validation)", w = textWidth(text);
    const left = Math.max(area.left, Math.min(x.getPixelForValue(epoch) - w / 2, area.right - w));
    const y = area.top - LINE / 2 - 2;
    ctx.save();
    ctx.font = LABEL_FONT;
    ctx.fillStyle = ink.text2;
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    ctx.fillText(text, left, y);
    ctx.restore();
    return { text, lines: [text], kind: "top", leader: false, x: left, y, w, h: LINE };
  };

  // Every placed label is kept on its chart, so a page test can read back where each one went.
  const runOverlay = (curve, ink) => ({
    id: "runOverlay",
    beforeDatasetsDraw(chart) { referenceLines(chart, ink, curve.baselines, curve.best_epoch); },
    afterDatasetsDraw(chart) {
      const ends = chart.data.datasets.map((ds, i) => endLabel(chart, i, SPLITS[ds.series].name, ink.text));
      const gutterLabels = drawGutter(chart, ink, [...baselineLabels(chart, ink, curve.baselines), ...ends]);
      chart.$labels = [bestLabel(chart, ink, curve.best_epoch), ...gutterLabels];
    },
  });

  const compareOverlay = (cmp, ink) => ({
    id: "compareOverlay",
    beforeDatasetsDraw(chart) { referenceLines(chart, ink, cmp.baselines); },
    afterDatasetsDraw(chart) {
      const ends = chart.data.datasets.map((ds, i) => ({ ds, i }))
        .filter(({ ds, i }) => ds.series === "validation" && chart.isDatasetVisible(i))
        .map(({ ds, i }) => endLabel(chart, i, ds.short, ink.text, ds.borderColor));
      const refs = cmp.baselines ? baselineLabels(chart, ink, cmp.baselines) : [];
      chart.$labels = drawGutter(chart, ink, [...refs, ...ends]);
    },
  });

  const line = (split, curve, colour, extra) => ({
    ...SPLITS[split].style,
    series: split,
    data: curve.epochs.map((e) => ({ x: e.epoch, y: e[split] })),
    borderColor: colour,
    backgroundColor: colour,
    borderJoinStyle: "round",
    pointRadius: 0,
    pointHoverRadius: 4,
    pointHitRadius: 8,
    ...extra,
  });

  const logAxis = (ink, units, min, max) => ({
    type: "logarithmic",
    min,
    max,
    title: { display: true, text: `loss (${UNITS[units]})` },
    ticks: { autoSkip: false, callback: (v) => (isLabelledTick(v) ? fmt(v) : "") },
    grid: { color: (ctx) => (ctx.tick && isLabelledTick(ctx.tick.value) ? ink.grid : "transparent") },
    border: { color: ink.grid },
  });

  const epochAxis = (ink, min, max) => ({
    type: "linear",
    min,
    max,
    title: { display: true, text: "epoch" },
    ticks: { precision: 0 },
    grid: { color: ink.grid },
    border: { color: ink.grid },
  });

  const byDataset = (a, b) => a.datasetIndex - b.datasetIndex;

  const compareChart = (canvas, cmp, n, ink) => {
    const curves = cmp.runs.map((r) => runs[r.index].curve);
    const end = Math.max(...curves.map(lastEpoch));
    const datasets = cmp.runs.flatMap((r, m) => {
      const c = curves[m], colour = token(r.colour);
      const best = c.epochs.findIndex((e) => e.epoch === r.best_epoch);
      // Only the best epoch gets a mark; a run that stops early simply ends.
      const run = { runIndex: r.index, runLabel: r.label, short: r.short, hidden: hidden[n].has(r.index) };
      return [
        line("validation", c, colour, {
          ...run,
          clip: false, // the x axis ends on the last epoch, where a clip would halve its dot
          pointStyle: "circle",
          pointRadius: (ctx) => (ctx.dataIndex === best ? DOT : 0),
          pointBorderColor: colour,
          pointBorderWidth: 0,
        }),
        line("train", c, colour, run),
      ];
    });
    const values = curves.flatMap((c) => c.epochs.flatMap((e) => [e.train, e.validation]));
    const refs = cmp.baselines ? [cmp.baselines.constant, cmp.baselines.material] : [];
    const texts = [...cmp.runs.map((r) => r.short), ...(cmp.baselines ? BASELINES.map(([, t]) => t) : [])];
    return new Chart(canvas, {
      type: "line",
      data: { datasets },
      options: {
        animation: false,
        maintainAspectRatio: false,
        layout: { padding: ({ chart }) => ({ top: 4, right: gutter(texts, chart.width, MARK) }) },
        interaction: { mode: "index", intersect: false },
        scales: {
          x: epochAxis(ink, Math.min(...curves.map((c) => c.epochs[0].epoch)), end),
          y: logAxis(ink, cmp.units, Math.min(...values) * 0.7, Math.max(...refs, ...values) * 3),
        },
        plugins: {
          legend: { display: false },
          tooltip: {
            itemSort: byDataset,
            callbacks: {
              title: (items) => `epoch ${items[0].parsed.x}`,
              label: (item) => {
                const ds = item.dataset;
                const best = ds.series === "validation" && item.parsed.x === runs[ds.runIndex].curve.best_epoch ? " (best)" : "";
                return ` ${ds.runLabel}, ${SPLITS[ds.series].name}: ${fmt(item.parsed.y)}${best}`;
              },
            },
          },
        },
      },
      plugins: [compareOverlay(cmp, ink)],
    });
  };

  const runChart = (canvas, run, index, ink) => {
    const c = run.curve, colour = token(run.colour);
    const datasets = ["validation", "train", "test"].map((k) => line(k, c, colour, { runIndex: index }));
    const values = c.epochs.flatMap((e) => SERIES.map((k) => e[k]));
    const texts = [...Object.values(SPLITS).map((s) => s.name), ...BASELINES.map(([, t]) => t)];
    return new Chart(canvas, {
      type: "line",
      data: { datasets },
      options: {
        animation: false,
        maintainAspectRatio: false,
        layout: { padding: ({ chart }) => ({ top: LINE + 4, right: gutter(texts, chart.width, 0) }) },
        interaction: { mode: "index", intersect: false },
        scales: {
          x: epochAxis(ink, c.epochs[0].epoch, c.epochs[c.epochs.length - 1].epoch),
          y: logAxis(ink, c.units, Math.min(...values) * 0.7, Math.max(c.baselines.constant, c.baselines.material, ...values) * 3),
        },
        plugins: {
          legend: { display: false },
          tooltip: {
            itemSort: byDataset,
            callbacks: {
              title: (items) => `epoch ${items[0].parsed.x}${items[0].parsed.x === c.best_epoch ? " (best)" : ""}`,
              label: (item) => ` ${SPLITS[item.dataset.series].name}: ${fmt(item.parsed.y)}`,
            },
          },
        },
      },
      plugins: [runOverlay(c, ink)],
    });
  };

  let charts = [];
  const build = () => {
    charts.forEach((c) => c.destroy());
    const ink = { text: token("--text"), text2: token("--text-2"), grid: token("--grid") };
    Chart.defaults.color = ink.text2;
    Chart.defaults.font.family = "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif";
    compareCharts = comparisons.map((cmp, n) => compareChart(compareCanvases[n], cmp, n, ink));
    const own = runs.map((run, i) => runChart(canvases[i], run, i, ink));
    charts = [...compareCharts, ...own];
    document.body.dataset.rendered = String(charts.length);
  };
  build();
  // Colours live in CSS tokens: a theme change only has to redraw with the values they now hold.
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", build);
  new MutationObserver(build).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
})();
</script>
</body>
</html>
"""


if __name__ == "__main__":
    main()

"""Turn the curve files written by pytorch/train.py --curve into one HTML page.

Each run gets its own panel because runs trained with different losses are in
different units and must never share an axis. The data is embedded in the page,
so the file opens anywhere; only Chart.js comes from a CDN.

Reading order: the contract types, load_curve, summarize, render_page, main,
then the validation helpers and the page template.

Usage: training_curves.py OUT.html CURVE.json [CURVE.json ...]
"""

import json
import math
import sys
from pathlib import Path
from typing import TypedDict

LOSSES = ("mse", "sigmoid")
UNITS = ("pawns^2", "win-probability^2")
SERIES = ("train", "validation", "test")
USAGE = "usage: training_curves.py OUT.html CURVE.json [CURVE.json ...]"


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
    best = next(e for e in curve["epochs"] if e["epoch"] == curve["best_epoch"])
    return {
        "final_epoch": final["epoch"],
        "final_test": final["test"],
        "test_at_best": best["test"],
        "explained_pct": 100 * (1 - best["test"] / curve["baselines"]["constant"]),
    }


def render_page(curves: list[Curve]) -> str:
    runs = [{"curve": c, "summary": summarize(c)} for c in curves]
    return _TEMPLATE.replace("__RUNS_JSON__", _script_safe_json(runs))


def main(argv: list[str] | None = None) -> None:
    args = sys.argv[1:] if argv is None else argv
    if len(args) < 2:
        raise SystemExit(USAGE)
    out = Path(args[0])
    curves = [load_curve(Path(p)) for p in args[1:]]
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(render_page(curves))
    epochs = sum(len(c["epochs"]) for c in curves)
    print(f"wrote {out}: {len(curves)} run(s), {epochs} epoch(s)")


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
  --train: #2a78d6;
  --validation: #eb6834;
  --test: #1baf7a;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #1a1a19;
    --surface: #1a1a19;
    --text: #f0efe9;
    --text-2: #c3c2b7;
    --grid: #33322f;
    --border: #3d3c39;
    --train: #3987e5;
    --validation: #d95926;
    --test: #199e70;
    color-scheme: dark;
  }
}
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
.panel { min-width: 0; border: 1px solid var(--border); border-radius: 8px; padding: 16px; background: var(--surface); }
.panel h2 { font-size: 16px; margin: 0; overflow-wrap: anywhere; }
.meta { color: var(--text-2); font-size: 13px; margin: 2px 0 12px; }
.plot { position: relative; height: 320px; }
.caption { color: var(--text-2); font-size: 13px; margin: 12px 0 0; }
.caption strong { color: var(--text); font-weight: 600; }
.nochart { color: var(--text-2); font-size: 13px; }
details { margin-top: 32px; }
summary { cursor: pointer; font-weight: 600; }
.tablewrap { overflow-x: auto; margin-top: 16px; }
table { border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; }
caption { text-align: left; font-weight: 600; padding: 0 0 6px; overflow-wrap: anywhere; }
th, td { padding: 4px 12px 4px 0; text-align: right; border-bottom: 1px solid var(--grid); }
th:first-child, td:first-child { text-align: left; }
thead th { color: var(--text-2); font-weight: 600; }
tr.best td, tr.best th { font-weight: 600; }
</style>
</head>
<body>
<main>
<h1>Training curves</h1>
<p class="lede">One panel per run, each in its own loss units, log scale. Baselines are measured on the test positions in the run's loss space; the best epoch is the one whose weights were restored, chosen on validation.</p>
<div class="panels" id="panels"></div>
<details>
<summary>Table view: every epoch, per run</summary>
<div id="tables"></div>
</details>
</main>
<script type="application/json" id="runs">__RUNS_JSON__</script>
<script src="https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.1/chart.umd.min.js"></script>
<script>
(() => {
  const runs = JSON.parse(document.getElementById("runs").textContent);
  const SERIES = ["train", "validation", "test"];
  const UNITS = { "pawns^2": "pawns\\u00b2", "win-probability^2": "win-probability\\u00b2" };
  const BASELINES = [["constant", "constant (mean)"], ["material", "material only (P1 N3 B3 R5 Q9)"]];
  const fmt = (v) => Number(v.toPrecision(3)).toString();
  const token = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const el = (tag, attrs, text) => {
    const node = document.createElement(tag);
    Object.entries(attrs || {}).forEach(([k, v]) => node.setAttribute(k, v));
    if (text !== undefined) node.textContent = text;
    return node;
  };

  const panelsRoot = document.getElementById("panels");
  const tablesRoot = document.getElementById("tables");
  const canvases = runs.map((run) => {
    const c = run.curve, s = run.summary, units = UNITS[c.units];
    const panel = el("section", { class: "panel" });
    panel.append(el("h2", {}, c.label));
    panel.append(el("p", { class: "meta" }, `loss ${c.loss}, ${units}, log scale`));
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
    table.append(el("caption", {}, `${c.label} (${units})`));
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
    panelsRoot.querySelectorAll(".plot").forEach((p) => p.replaceChildren(
      el("p", { class: "nochart" }, "Chart.js did not load (offline?). The table view below has every number.")));
    return;
  }

  const isLabelledTick = (v) => {
    const m = v / 10 ** Math.floor(Math.log10(v));
    return [1, 2, 5].some((k) => Math.abs(m - k) < 1e-6);
  };

  // Baselines, best epoch and end labels, drawn over the chart. Closes over its run.
  const overlay = (curve, ink) => ({
    id: "overlay",
    afterDatasetsDraw(chart) {
      const { ctx, chartArea: area, scales: { x, y } } = chart;
      ctx.save();
      ctx.font = "12px system-ui, -apple-system, sans-serif";
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = ink.text2;
      ctx.fillStyle = ink.text2;
      ctx.setLineDash([6, 4]);
      // Text on a surface-coloured knockout, so no dashed line strikes through it.
      const label = (text, lx, ly, align, baseline, color) => {
        const w = ctx.measureText(text).width, h = 15;
        const left = align === "right" ? lx - w : lx;
        const top = baseline === "bottom" ? ly - h : baseline === "top" ? ly : ly - h / 2;
        ctx.fillStyle = ink.surface;
        ctx.fillRect(left - 2, top, w + 4, h);
        ctx.fillStyle = color;
        ctx.textAlign = align;
        ctx.textBaseline = baseline;
        ctx.fillText(text, lx, ly);
      };
      const texts = [];

      const base = BASELINES.map(([k, text]) => ({ value: curve.baselines[k], text }))
        .sort((a, b) => b.value - a.value);
      base.forEach((b, i) => {
        const py = y.getPixelForValue(b.value);
        if (py < area.top || py > area.bottom) return;
        ctx.beginPath(); ctx.moveTo(area.left, py); ctx.lineTo(area.right, py); ctx.stroke();
        // On a narrow panel the long label spills into the right margin rather than over the y axis.
        const edge = Math.min(chart.width - 2, Math.max(area.right - 2, area.left + ctx.measureText(b.text).width + 8));
        texts.push([b.text, edge, i === 0 ? py - 3 : py + 3, "right", i === 0 ? "bottom" : "top", ink.text2]);
      });

      const bx = x.getPixelForValue(curve.best_epoch);
      ctx.beginPath(); ctx.moveTo(bx, area.top); ctx.lineTo(bx, area.bottom); ctx.stroke();
      const right = bx > (area.left + area.right) / 2;
      texts.push(["best (validation)", right ? bx - 4 : bx + 4, area.top + 2, right ? "right" : "left", "top", ink.text2]);
      texts.forEach((t) => label(...t));

      ctx.setLineDash([]);
      ctx.lineWidth = 1;
      ctx.strokeStyle = ink.text2;
      const ends = chart.data.datasets.map((ds, i) => {
        if (!chart.isDatasetVisible(i)) return null;
        const pts = chart.getDatasetMeta(i).data;
        const p = pts[pts.length - 1];
        return { text: ds.label, x: p.x, y: p.y, ly: p.y };
      }).filter(Boolean).sort((a, b) => a.y - b.y);
      const GAP = 14;
      ends.forEach((e, i) => { if (i > 0) e.ly = Math.max(e.y, ends[i - 1].ly + GAP); });
      const overflow = ends.length ? ends[ends.length - 1].ly - (area.bottom + 6) : 0;
      if (overflow > 0) ends.forEach((e) => { e.ly -= overflow; });
      ends.forEach((e) => {
        ctx.beginPath(); ctx.moveTo(e.x + 7, e.y); ctx.lineTo(e.x + 14, e.ly); ctx.stroke();
        label(e.text, e.x + 17, e.ly, "left", "middle", ink.text);
      });
      ctx.restore();
    },
  });

  let charts = [];
  const build = () => {
    charts.forEach((c) => c.destroy());
    const ink = { text: token("--text"), text2: token("--text-2"), grid: token("--grid"), surface: token("--surface") };
    Chart.defaults.color = ink.text2;
    Chart.defaults.font.family = "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif";
    charts = runs.map((run, i) => {
      const c = run.curve;
      const last = c.epochs.length - 1;
      const values = c.epochs.flatMap((e) => SERIES.map((k) => e[k]));
      const top = Math.max(c.baselines.constant, c.baselines.material, ...values) * 3;
      const bottom = Math.min(...values) * 0.7;
      return new Chart(canvases[i], {
        type: "line",
        data: {
          datasets: SERIES.map((k) => ({
            label: k,
            data: c.epochs.map((e) => ({ x: e.epoch, y: e[k] })),
            borderColor: token(`--${k}`),
            backgroundColor: token(`--${k}`),
            borderWidth: 2,
            borderCapStyle: "round",
            borderJoinStyle: "round",
            pointRadius: (ctx) => (ctx.dataIndex === last ? 4 : 0),
            pointHoverRadius: 5,
            pointBorderColor: ink.surface,
            pointBorderWidth: 2,
            pointHitRadius: 8,
          })),
        },
        options: {
          animation: false,
          maintainAspectRatio: false,
          layout: { padding: { right: 84 } },
          interaction: { mode: "index", intersect: false },
          scales: {
            x: {
              type: "linear",
              min: c.epochs[0].epoch,
              max: c.epochs[last].epoch,
              title: { display: true, text: "epoch" },
              ticks: { precision: 0 },
              grid: { color: ink.grid },
              border: { color: ink.grid },
            },
            y: {
              type: "logarithmic",
              min: bottom,
              max: top,
              title: { display: true, text: `loss (${UNITS[c.units]})` },
              ticks: { autoSkip: false, callback: (v) => (isLabelledTick(v) ? fmt(v) : "") },
              grid: { color: (ctx) => (ctx.tick && isLabelledTick(ctx.tick.value) ? ink.grid : "transparent") },
              border: { color: ink.grid },
            },
          },
          plugins: {
            legend: { position: "top", align: "start", labels: { boxWidth: 16, boxHeight: 2, color: ink.text2 } },
            tooltip: {
              mode: "index",
              intersect: false,
              callbacks: {
                title: (items) => `epoch ${items[0].parsed.x}${items[0].parsed.x === c.best_epoch ? " (best)" : ""}`,
                label: (item) => ` ${item.dataset.label}: ${fmt(item.parsed.y)}`,
              },
            },
          },
        },
        plugins: [overlay(c, ink)],
      });
    });
    document.body.dataset.rendered = String(charts.length);
  };
  build();
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", build);
})();
</script>
</body>
</html>
"""


if __name__ == "__main__":
    main()

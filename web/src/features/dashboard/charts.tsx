import type { ReactNode } from "react";
import { cx } from "@/components/ui";
import { DONUT_HOLE, DONUT_RADIUS, DONUT_UNIT, LINE_AXIS_LABELS, LINE_HEIGHT_PX } from "@/config";
import { t } from "@/i18n";

// The few shapes a dashboard needs, drawn by hand as Armature draws them: a chart library would be a dependency for
// a handful of rectangles and arcs. Every chart has its numbers in words too, and each value opens in the grid.

/** The six series colours, in the order a widget hands them out; literal so Tailwind sees them. */
export const CHART_TONES = ["text-chart-1", "text-chart-2", "text-chart-3", "text-chart-4", "text-chart-5", "text-chart-6"] as const;

/** A status category's colour, so a status reads as what it means. */
const categoryTone: Record<string, string> = {
  draft: "text-status-todo",
  review: "text-status-progress",
  approved: "text-accent",
  done: "text-status-done",
  closed: "text-ink-subtle",
};

export interface Slice {
  label: string;
  count: number;
  category?: string;
  /** Opens the slice's requirements, drawn around its name; none for what cannot be opened. */
  open?: (children: ReactNode) => ReactNode;
}

/** The tone of each slice: by its status category, else by its place. */
export function tonesOf(slices: Slice[]): string[] {
  return slices.map((s, i) => (s.category && categoryTone[s.category]) || CHART_TONES[i % CHART_TONES.length]!);
}

const opened = (s: Slice, children: ReactNode) => (s.open ? s.open(children) : children);

/** Rows of name, bar and count, each bar sized against the largest. */
export function Bars({ slices }: { slices: Slice[] }) {
  const max = Math.max(1, ...slices.map((s) => s.count));
  const tones = tonesOf(slices);
  return (
    <ul className="space-y-1.5" data-chart="bars">
      {slices.map((s, i) => (
        <li key={`${s.label}-${i}`} className="grid grid-cols-[minmax(0,9rem)_1fr_2.5rem] items-center gap-2 text-sm" data-bar={s.label}>
          <span className="truncate text-ink" title={s.label}>
            {opened(s, s.label)}
          </span>
          <span className="h-2 rounded-sm bg-surface-raised" aria-hidden>
            <span className={cx("block h-2 rounded-sm bg-current", tones[i])} style={{ width: `${(s.count / max) * 100}%` }} />
          </span>
          <span className="text-right text-ink-muted tabular-nums">{s.count}</span>
        </li>
      ))}
    </ul>
  );
}

/** A point on a circle at a fraction of the turn, clockwise from twelve o'clock. */
function point(radius: number, turn: number): string {
  const angle = (turn - 0.25) * 2 * Math.PI;
  const round = (v: number) => Math.round(v * 100) / 100;
  return `${round(DONUT_UNIT / 2 + radius * Math.cos(angle))} ${round(DONUT_UNIT / 2 + radius * Math.sin(angle))}`;
}

/** The path of a ring segment; a whole turn is two halves, since an arc ending where it starts is nothing. */
export function arcPath(start: number, end: number): string {
  if (end - start >= 1 - 1e-9) return `${arcPath(start, start + 0.5)} ${arcPath(start + 0.5, start + 1 - 1e-6)}`;
  const large = end - start > 0.5 ? 1 : 0;
  return [
    `M${point(DONUT_RADIUS, start)}`,
    `A${DONUT_RADIUS} ${DONUT_RADIUS} 0 ${large} 1 ${point(DONUT_RADIUS, end)}`,
    `L${point(DONUT_HOLE, end)}`,
    `A${DONUT_HOLE} ${DONUT_HOLE} 0 ${large} 0 ${point(DONUT_HOLE, start)}`,
    "Z",
  ].join(" ");
}

/** A ring of slices with the total in its hole, and a legend that names each and opens it. */
export function Donut({ slices, total }: { slices: Slice[]; total: number }) {
  const sum = slices.reduce((n, s) => n + s.count, 0);
  const tones = tonesOf(slices);
  let at = 0;
  const arcs = slices.map((s, i) => {
    const share = sum > 0 ? s.count / sum : 0;
    const arc = { slice: s, tone: tones[i]!, start: at, end: at + share, share };
    at += share;
    return arc;
  });
  return (
    <div data-chart="donut">
      <svg
        viewBox={`0 0 ${DONUT_UNIT} ${DONUT_UNIT}`}
        className="mx-auto block h-40 w-40"
        role="img"
        aria-label={arcs.map((a) => `${a.slice.label} ${Math.round(a.share * 100)}%`).join(", ")}
      >
        {arcs
          .filter((a) => a.share > 0)
          .map((a) => (
            <path key={a.slice.label} d={arcPath(a.start, a.end)} fill="currentColor" className={cx("stroke-surface", a.tone)} strokeWidth={1.5}>
              <title>{`${a.slice.label}: ${a.slice.count}`}</title>
            </path>
          ))}
        <text x={DONUT_UNIT / 2} y={DONUT_UNIT / 2} textAnchor="middle" dominantBaseline="middle" className="fill-ink text-xl font-semibold">
          {total}
        </text>
      </svg>
      <ul className="mt-3 grid gap-x-4 gap-y-1 text-xs text-ink-muted sm:grid-cols-2" data-chart-legend>
        {arcs.map((a) => (
          <li key={a.slice.label} className="flex min-w-0 items-center gap-2">
            <span className={cx("inline-block size-2.5 shrink-0 rounded-sm bg-current", a.tone)} aria-hidden />
            <span className="min-w-0 flex-1 truncate text-ink">{opened(a.slice, a.slice.label)}</span>
            <span className="tabular-nums">
              {a.slice.count}
              <span className="ml-1 text-ink-subtle">{Math.round(a.share * 100)}%</span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A large number with what it counts under it. */
export function Stat({ value, label }: { value: ReactNode; label: string }) {
  return (
    <div>
      <div className="text-3xl font-semibold tracking-tight text-ink tabular-nums">{value}</div>
      <div className="text-xs text-ink-muted">{label}</div>
    </div>
  );
}

export interface Series {
  name: string;
  points: { label: string; count: number }[];
}

/** Lines over time against the tallest point, drawn into a unit box that stretches to the widget. */
export function Lines({ series }: { series: Series[] }) {
  const unit = 100;
  const longest = Math.max(1, ...series.map((s) => s.points.length - 1));
  const max = Math.max(1, ...series.flatMap((s) => s.points.map((p) => p.count)));
  const labels = series[0]?.points.map((p) => p.label) ?? [];
  const every = Math.max(1, Math.ceil(labels.length / LINE_AXIS_LABELS));
  return (
    <div data-chart="lines">
      <svg
        viewBox={`0 0 ${unit} ${unit}`}
        preserveAspectRatio="none"
        className="block w-full overflow-visible"
        style={{ height: LINE_HEIGHT_PX }}
        role="img"
        aria-label={series.map((s) => s.name).join(", ")}
      >
        <line x1={0} y1={unit} x2={unit} y2={unit} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        {series.map((s, i) => (
          <polyline
            key={s.name}
            className={CHART_TONES[i % CHART_TONES.length]}
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
            points={s.points.map((p, x) => `${(x / longest) * unit},${unit - (p.count / max) * unit}`).join(" ")}
            data-series={s.name}
          />
        ))}
      </svg>
      <div className="mt-1 flex justify-between text-2xs text-ink-subtle" aria-hidden>
        {labels.map((label, i) => (
          <span key={`${label}-${i}`}>{i % every === 0 ? label : ""}</span>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-4 text-2xs text-ink-muted">
        {series.map((s, i) => (
          <span key={s.name} className="flex items-center gap-1">
            <span className={cx("inline-block h-0.5 w-3 bg-current", CHART_TONES[i % CHART_TONES.length])} aria-hidden />
            {s.name}
          </span>
        ))}
      </div>
    </div>
  );
}

/** The numbers of a chart as a table, behind a disclosure, for whoever reads tables better than pictures. */
export function AsTable({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <details className="mt-3 text-sm" data-as-table>
      <summary className="cursor-pointer text-xs text-ink-muted">{t.dashboard.asTable}</summary>
      <table className="mt-2 w-full text-left text-xs">
        <thead>
          <tr>
            {head.map((h) => (
              <th key={h} scope="col" className="py-1 pr-2 font-medium text-ink-muted">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="border-t border-border">
              {row.map((cell, j) => (
                <td key={j} className="py-1 pr-2 text-ink tabular-nums">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}

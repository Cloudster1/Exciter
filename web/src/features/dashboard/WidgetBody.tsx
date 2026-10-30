import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import type { WidgetConfig, WidgetData, WidgetKind } from "@/api/dashboards";
import { useViews } from "@/api/search";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { bucketAddress, bucketLabel } from "./address";
import { AsTable, Bars, Donut, Lines, Stat, type Slice } from "./charts";

// A bucket starts at midnight UTC, the day the server counted it in, so it keeps that day in every zone.
const axisDay = localDateFormat({ day: "numeric", month: "numeric", timeZone: "UTC" });
const tableDay = localDateFormat({ dateStyle: "short", timeZone: "UTC" });

/** What a widget shows, drawn from its data, every value opening its requirements in the grid. */
export function WidgetBody({ projectKey, kind, config, data }: { projectKey: string; kind: WidgetKind; config: WidgetConfig; data: WidgetData }) {
  const { data: views = [] } = useViews(projectKey, "grid");
  const view = views.find((v) => v.id === data.viewId);
  const opener =
    (cond: string, label: string, count: number) =>
    (children: ReactNode): ReactNode => (
      <Link
        to="/p/$projectKey/grid"
        params={{ projectKey }}
        search={bucketAddress(data, cond, view)}
        className="hover:text-accent hover:underline"
        aria-label={t.dashboard.open(label, count)}
        data-bucket={label}
      >
        {children}
      </Link>
    );
  const slicesOf = (index: number, words: Record<string, string> = {}): Slice[] =>
    (data.groups[index]?.buckets ?? []).map((b) => {
      const label = bucketLabel(b, words);
      return { label, count: b.count, category: b.category || undefined, open: b.query || b.label ? opener(b.query, label, b.count) : undefined };
    });
  const table = (slices: Slice[]) => (
    <AsTable head={[t.dashboard.value, t.dashboard.count]} rows={slices.map((s) => [s.open ? s.open(s.label) : s.label, s.count])} />
  );
  const total = opener("", t.dashboard.total(data.total), data.total);

  switch (kind) {
    case "count":
      return <Stat value={total(data.total)} label={t.dashboard.total(data.total)} />;
    case "breakdown": {
      const words = config.groupBy === "category" ? (t.workflows.categories as Record<string, string>) : {};
      const slices = slicesOf(0, words);
      return (
        <>
          {config.shape === "donut" ? <Donut slices={slices} total={data.total} /> : <Bars slices={slices} />}
          {table(slices)}
        </>
      );
    }
    case "review": {
      const slices = slicesOf(0, t.dashboard.approval);
      return (
        <>
          <Bars slices={slices} />
          {table(slices)}
        </>
      );
    }
    case "coverage":
      return (
        <div className="grid gap-4 sm:grid-cols-3">
          {data.groups.map((g, i) => (
            <section key={g.name} aria-label={t.dashboard.dimensions[g.name] ?? g.name}>
              <h3 className="mb-1 text-xs font-semibold text-ink-muted uppercase">{t.dashboard.dimensions[g.name] ?? g.name}</h3>
              <Bars slices={slicesOf(i, t.dashboard.levels)} />
            </section>
          ))}
        </div>
      );
    case "quality": {
      const slices = slicesOf(0);
      return (
        <>
          <Stat value={data.average === null ? "-" : Math.round(data.average)} label={data.average === null ? t.dashboard.noScore : t.dashboard.average} />
          <div className="mt-3">
            <Bars slices={slices} />
          </div>
          {table(slices)}
        </>
      );
    }
    case "suspects": {
      const suspect = data.groups[0]?.buckets[0];
      const n = suspect?.count ?? 0;
      return (
        <>
          <Stat value={suspect ? opener(suspect.query, t.dashboard.suspects(n), n)(n) : 0} label={t.dashboard.suspects(n)} />
          <RowList projectKey={projectKey} data={data} />
        </>
      );
    }
    case "list":
      return <RowList projectKey={projectKey} data={data} />;
    case "changes": {
      const series = data.lines.map((l) => ({
        name: t.dashboard.series[l.series] ?? l.series,
        points: l.points.map((p) => ({ label: axisDay.format(new Date(p.start)), count: p.count })),
      }));
      const dates = data.lines[0]?.points.map((p) => tableDay.format(new Date(p.start))) ?? [];
      return (
        <>
          <Lines series={series} />
          <AsTable
            head={["", ...series.map((s) => s.name)]}
            rows={dates.map((date, i) => [
              date,
              ...data.lines.map((l) => {
                const p = l.points[i]!;
                return p.query ? opener(p.query, `${date} ${t.dashboard.series[l.series] ?? l.series}`, p.count)(p.count) : p.count;
              }),
            ])}
          />
        </>
      );
    }
  }
  return null;
}

/** The requirements a list or the suspects widget names, each opening its page. */
function RowList({ projectKey, data }: { projectKey: string; data: WidgetData }) {
  if (data.rows.length === 0) return null;
  return (
    <ul className="mt-3 space-y-1 text-sm" data-widget-rows>
      {data.rows.map((row) => (
        <li key={row.id} className="flex min-w-0 items-baseline gap-2">
          <Link
            to="/p/$projectKey/r/$reqKey"
            params={{ projectKey, reqKey: row.key }}
            className="shrink-0 font-mono text-xs text-ink-muted hover:text-accent hover:underline"
          >
            {row.key}
          </Link>
          <span className="truncate text-ink">{row.title}</span>
          {row.status && <span className="ml-auto shrink-0 text-xs text-ink-subtle">{row.status.name}</span>}
        </li>
      ))}
    </ul>
  );
}

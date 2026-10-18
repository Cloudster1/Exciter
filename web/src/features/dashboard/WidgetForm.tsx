import { useState, type FormEvent } from "react";
import { useAttributes } from "@/api/attributes";
import { ApiError } from "@/api/client";
import { useWidgetPreview, type Widget, type WidgetConfig, type WidgetInput, type WidgetKind } from "@/api/dashboards";
import { useViews } from "@/api/search";
import { Button, Checkbox, ErrorBanner, Field, Segmented, Select } from "@/components/ui";
import { DASHBOARD_COLUMNS, DASHBOARD_DEFAULT_DAYS, DASHBOARD_DEFAULT_ROWS } from "@/config";
import { QueryInput } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { WidgetBody } from "./WidgetBody";

const KINDS: WidgetKind[] = ["breakdown", "count", "list", "coverage", "review", "suspects", "changes", "quality"];
const GROUPS = ["status", "category", "type", "owner", "priority", "label", "attribute"] as const;
const SERIES = ["created", "revised", "statusChanged"] as const;
const WIDTHS = Array.from({ length: DASHBOARD_COLUMNS }, (_, i) => i + 1);

/** Sets a widget up, or changes one: its kind once, its title, width, what it reads and how, previewed as it would read. */
export function WidgetForm({
  projectKey,
  widget,
  pending,
  error,
  onSave,
  onCancel,
}: {
  projectKey: string;
  widget?: Widget;
  pending: boolean;
  error: Error | null;
  onSave: (input: WidgetInput) => void;
  onCancel: () => void;
}) {
  const [kind, setKind] = useState<WidgetKind>(widget?.kind ?? "breakdown");
  const [title, setTitle] = useState<string>(widget?.title ?? t.dashboard.kinds.breakdown ?? "");
  const [width, setWidth] = useState(widget?.width ?? 1);
  const [config, setConfig] = useState<WidgetConfig>(widget?.config ?? { source: {} });
  const [from, setFrom] = useState<"all" | "query" | "view">(config.source.viewId ? "view" : config.source.query ? "query" : "all");
  const { data: attributes = [] } = useAttributes(projectKey);
  const { data: views = [] } = useViews(projectKey, "grid");
  const shared = views.filter((v) => v.shared);
  const preview = useWidgetPreview(projectKey, kind, config, true);
  const fields = error instanceof ApiError ? error.fields : {};
  const set = (patch: Partial<WidgetConfig>) => setConfig({ ...config, ...patch });

  function submit(event: FormEvent) {
    event.preventDefault();
    onSave({ kind, title, width, config });
  }

  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-widget-form>
      {error && Object.keys(fields).length === 0 && <ErrorBanner>{error.message}</ErrorBanner>}
      <div className="grid gap-3 sm:grid-cols-3">
        <Select
          label={t.dashboard.kind}
          value={kind}
          disabled={Boolean(widget)}
          onChange={(event) => {
            const next = event.target.value as WidgetKind;
            if (title === t.dashboard.kinds[kind]) setTitle(t.dashboard.kinds[next] ?? next);
            setKind(next);
          }}
        >
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {t.dashboard.kinds[k]}
            </option>
          ))}
        </Select>
        <Field label={t.dashboard.widgetTitle} value={title} onChange={(event) => setTitle(event.target.value)} error={fields.title} required />
        <Select label={t.dashboard.width} value={width} onChange={(event) => setWidth(Number(event.target.value))}>
          {WIDTHS.map((w) => (
            <option key={w} value={w}>
              {t.dashboard.widths(w)}
            </option>
          ))}
        </Select>
      </div>
      <div className="space-y-2">
        <Segmented
          label={t.dashboard.source}
          value={from}
          size="sm"
          options={[
            { value: "all", label: t.dashboard.everything },
            { value: "query", label: t.dashboard.sourceQuery },
            { value: "view", label: t.dashboard.sourceView },
          ]}
          onChange={(next) => {
            setFrom(next);
            set({ source: next === "view" ? { viewId: shared[0]?.id } : next === "query" ? { query: config.source.query ?? "" } : {} });
          }}
        />
        {from === "query" && (
          <QueryInput
            projectKey={projectKey}
            name="widget-query"
            label={t.dashboard.query}
            value={config.source.query ?? ""}
            onSubmit={(query) => set({ source: { query } })}
            error={preview.error}
          />
        )}
        {from === "view" && (
          <Select
            label={t.dashboard.view}
            value={config.source.viewId ?? ""}
            error={fields["config.source.viewId"]}
            onChange={(event) => set({ source: { viewId: event.target.value || undefined } })}
          >
            {shared.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name}
              </option>
            ))}
          </Select>
        )}
      </div>
      {kind === "breakdown" && (
        <div className="grid gap-3 sm:grid-cols-3">
          <Select
            label={t.dashboard.groupBy}
            value={config.groupBy ?? "status"}
            onChange={(event) => set({ groupBy: event.target.value as WidgetConfig["groupBy"] })}
          >
            {GROUPS.map((g) => (
              <option key={g} value={g}>
                {t.dashboard.groups[g]}
              </option>
            ))}
          </Select>
          {config.groupBy === "attribute" && (
            <Select
              label={t.dashboard.attribute}
              value={config.attributeId ?? ""}
              error={fields["config.attributeId"]}
              onChange={(event) => set({ attributeId: event.target.value || undefined })}
            >
              <option value="" />
              {attributes
                .filter((a) => a.kind !== "richText" && !a.retired)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
            </Select>
          )}
          <Select label={t.dashboard.shape} value={config.shape ?? "bars"} onChange={(event) => set({ shape: event.target.value as WidgetConfig["shape"] })}>
            {(["bars", "donut"] as const).map((s) => (
              <option key={s} value={s}>
                {t.dashboard.shapes[s]}
              </option>
            ))}
          </Select>
        </div>
      )}
      {kind === "list" && (
        <div className="w-32">
          <Field
            label={t.dashboard.rows}
            type="number"
            min={1}
            value={config.rows ?? DASHBOARD_DEFAULT_ROWS}
            onChange={(event) => set({ rows: Number(event.target.value) })}
            error={fields["config.rows"]}
          />
        </div>
      )}
      {kind === "changes" && (
        <div className="grid gap-3 sm:grid-cols-3">
          <Select
            label={t.dashboard.interval}
            value={config.interval ?? "week"}
            onChange={(event) => set({ interval: event.target.value as WidgetConfig["interval"] })}
          >
            {(["week", "month"] as const).map((i) => (
              <option key={i} value={i}>
                {t.dashboard.intervals[i]}
              </option>
            ))}
          </Select>
          <Field
            label={t.dashboard.days}
            type="number"
            min={1}
            value={config.days ?? DASHBOARD_DEFAULT_DAYS}
            onChange={(event) => set({ days: Number(event.target.value) })}
            error={fields["config.days"]}
          />
          <fieldset className="space-y-1">
            <legend className="sr-only">{t.dashboard.kinds.changes}</legend>
            {SERIES.map((s) => {
              const chosen = config.series ?? ["created", "revised"];
              return (
                <Checkbox
                  key={s}
                  label={t.dashboard.series[s]}
                  checked={chosen.includes(s)}
                  onChange={(event) => set({ series: event.target.checked ? [...chosen, s] : chosen.filter((each) => each !== s) })}
                />
              );
            })}
          </fieldset>
        </div>
      )}
      <section aria-label={t.dashboard.preview} className="rounded-control border border-dashed border-border p-3" data-widget-preview>
        <h3 className="mb-2 text-xs font-semibold text-ink-muted uppercase">{t.dashboard.preview}</h3>
        {preview.data && <WidgetBody projectKey={projectKey} kind={kind} config={config} data={preview.data} />}
        {preview.error && !(preview.error instanceof ApiError && preview.error.code === "bad_query") && (
          <p className="text-sm text-danger">{preview.error.message}</p>
        )}
      </section>
      <div className="flex gap-2">
        <Button type="submit" size="sm" loading={pending} data-action="save-widget">
          {widget ? t.dashboard.saveWidget : t.dashboard.addIt}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onCancel}>
          {t.dashboard.cancel}
        </Button>
      </div>
    </form>
  );
}

import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { NOW, useBaseline, useBaselineActions, useBaselines, useBaselineSpec, type Baseline } from "@/api/baselines";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import type { StatusCategory } from "@/api/workflows";
import { Button, Card, ErrorBanner, Field, PageHeader, Segmented, Select, Skeleton, Tag, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BASELINE_NAME_MAX_LENGTH, BASELINE_SPEC_PAGE_SIZE } from "@/config";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { ExportMenu } from "@/features/print/ExportMenu";
import { baselineDocumentHref } from "@/api/documents";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { BaselineMatrixView } from "./BaselineMatrix";
import { BaselineSigning } from "./BaselineSigning";
import { keepsBaselines } from "./Baselines";

const takenOn = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** One baseline read as a specification, each requirement at the revision it holds, with its administrators' actions. */
export function BaselinePage({ project, baselineId }: { project: Project; baselineId: string }) {
  const { data: baseline, error, refetch } = useBaseline(baselineId);
  const [reading, setReading] = useState<"spec" | "matrix">("spec");
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!baseline) return <Skeleton />;
  return (
    <div className="mx-auto max-w-3xl space-y-4" data-baseline-page={baseline.id}>
      <PageHeader
        crumb={
          <Link to="/p/$projectKey/baselines" params={{ projectKey: project.key }} className="hover:text-accent hover:underline">
            {t.baseline.back}
          </Link>
        }
        title={baseline.name}
        meta={[
          baseline.root ? t.baseline.ofSection(baseline.root.key, baseline.root.title) : t.baseline.wholeProject,
          t.baseline.items(baseline.items),
          t.baseline.takenBy(baseline.createdByName, takenOn.format(new Date(baseline.createdAt))),
        ].join(" · ")}
      />
      {baseline.description && <p className="text-sm text-ink-muted">{baseline.description}</p>}
      <div className="flex flex-wrap items-start gap-2">
        <ExportMenu title={baseline.name} href={(format) => baselineDocumentHref(baseline.id, format)} />
      </div>
      <Actions project={project} baseline={baseline} />
      <BaselineSigning baseline={baseline} />
      <ComparePicker project={project} baseline={baseline} />
      <Segmented
        label={t.baseline.pageTabs}
        value={reading}
        onChange={setReading}
        size="sm"
        options={[
          { value: "spec", label: t.baseline.readingTab },
          { value: "matrix", label: t.baseline.matrixTab, attrs: { "data-action": "baseline-matrix" } },
        ]}
      />
      {reading === "spec" ? <Reading project={project} baseline={baseline} /> : <BaselineMatrixView project={project} baseline={baseline} />}
    </div>
  );
}

function Actions({ project, baseline }: { project: Project; baseline: Baseline }) {
  const { update, remove } = useBaselineActions(project.key);
  const navigate = useNavigate();
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(baseline.name);
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  if (baseline.signing && baseline.signing.signatures > 0) {
    return (
      <p className="text-sm text-ink-muted" data-baseline-locked="signed">
        {t.control.signedHint}
      </p>
    );
  }
  if (baseline.locked) {
    return (
      <p className="flex items-center gap-2 text-sm text-ink-muted" data-baseline-locked="">
        <Tag>
          <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
          {t.baseline.locked}
        </Tag>
        {t.baseline.lockedHint}
      </p>
    );
  }
  if (!keepsBaselines(project)) return null;

  function rename(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: baseline.id, name: name.trim() }, { onSuccess: () => setRenaming(false) });
  }

  return (
    <div className="space-y-2">
      {renaming ? (
        <form onSubmit={rename} className="flex flex-wrap items-end gap-2" aria-label={t.baseline.rename}>
          <Field
            label={t.baseline.name}
            value={name}
            maxLength={BASELINE_NAME_MAX_LENGTH}
            error={fields.name}
            onChange={(event) => setName(event.target.value)}
          />
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            {t.baseline.save}
          </Button>
          <Button type="button" variant="ghost" onClick={() => setRenaming(false)}>
            {t.baseline.cancel}
          </Button>
        </form>
      ) : (
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" onClick={() => setRenaming(true)} data-action="rename-baseline">
            {t.baseline.rename}
          </Button>
          <Button
            variant="secondary"
            icon={<Icon.Lock />}
            loading={update.isPending}
            onClick={() => {
              if (window.confirm(t.baseline.confirmLock)) update.mutate({ id: baseline.id, locked: true });
            }}
            data-action="lock-baseline"
          >
            {t.baseline.lock}
          </Button>
          <Button
            variant="secondary"
            icon={<Icon.Trash />}
            loading={remove.isPending}
            onClick={() => {
              if (!window.confirm(t.baseline.confirmDelete)) return;
              remove.mutate(baseline.id, { onSuccess: () => void navigate({ to: "/p/$projectKey/baselines", params: { projectKey: project.key } }) });
            }}
            data-action="delete-baseline"
          >
            {t.baseline.delete}
          </Button>
        </div>
      )}
      {update.error && !fields.name && <ErrorBanner>{update.error.message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
    </div>
  );
}

function ComparePicker({ project, baseline }: { project: Project; baseline: Baseline }) {
  const { data } = useBaselines(project.key);
  const navigate = useNavigate();
  const [other, setOther] = useState(NOW);
  const others = (data ?? []).filter((each) => each.id !== baseline.id);
  return (
    <div className="flex flex-wrap items-end gap-2" data-compare-picker="">
      <Select label={t.baseline.compareWith} value={other} onChange={(event) => setOther(event.target.value)} className="w-56">
        <option value={NOW}>{t.baseline.now}</option>
        {others.map((each) => (
          <option key={each.id} value={each.id}>
            {each.name}
          </option>
        ))}
      </Select>
      <Button
        variant="secondary"
        onClick={() =>
          void navigate({
            to: "/p/$projectKey/baselines/$baselineId/compare",
            params: { projectKey: project.key, baselineId: baseline.id },
            search: { with: other },
          })
        }
        data-action="compare-baseline"
      >
        {t.baseline.compare}
      </Button>
    </div>
  );
}

function Reading({ project, baseline }: { project: Project; baseline: Baseline }) {
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useBaselineSpec(baseline.id, offset);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return (
    <div className="space-y-3">
      <ol className="space-y-3" aria-label={baseline.name} data-baseline-spec="">
        {data.entries.map((entry) => {
          const r = entry.requirement;
          const section = r.kind === "section";
          return (
            <li key={r.id} style={{ marginLeft: `${Math.min(entry.depth, 6) * 1}rem` }} data-baseline-entry={r.key}>
              <Card className="space-y-2 p-3">
                <div className="flex flex-wrap items-baseline gap-2">
                  <span className={cx("font-semibold text-ink", section ? "text-base" : "text-sm")}>
                    <span className="font-mono text-ink-muted">{entry.outline}</span> {r.title || t.baseline.purged}
                  </span>
                  <Link
                    to="/p/$projectKey/r/$reqKey"
                    params={{ projectKey: project.key, reqKey: r.key }}
                    className="font-mono text-xs text-ink-subtle hover:text-accent"
                  >
                    {r.key}
                  </Link>
                  <Tag>{t.baseline.revision(r.revision)}</Tag>
                  {entry.status && <StatusBadge name={entry.status.name} category={entry.status.category as StatusCategory} />}
                </div>
                {entry.statement ? <DocView doc={entry.statement as Doc} /> : <p className="text-sm text-ink-subtle">{t.baseline.purged}</p>}
              </Card>
            </li>
          );
        })}
      </ol>
      {data.total > BASELINE_SPEC_PAGE_SIZE && (
        <nav aria-label={baseline.name} className="flex items-center justify-between gap-2">
          <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - BASELINE_SPEC_PAGE_SIZE))}>
            {t.baseline.previous}
          </Button>
          <span className="text-sm text-ink-subtle tabular-nums">{t.baseline.range(offset + 1, offset + data.entries.length, data.total)}</span>
          <Button
            variant="secondary"
            size="sm"
            disabled={offset + BASELINE_SPEC_PAGE_SIZE >= data.total}
            onClick={() => setOffset(offset + BASELINE_SPEC_PAGE_SIZE)}
          >
            {t.baseline.next}
          </Button>
        </nav>
      )}
    </div>
  );
}

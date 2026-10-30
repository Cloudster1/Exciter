import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { executionFileHref, OUTCOMES, useCaseActions, useExecutionCase, useTestExecution, type ExecutionCase, type Outcome } from "@/api/tests";
import { Icon } from "@/components/icons";
import { Button, Card, ErrorBanner, IconButton, PageHeader, SectionTitle, Select, Skeleton, Tag, Textarea } from "@/components/ui";
import { TEST_NOTE_MAX_LENGTH } from "@/config";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { FileIssueDialog } from "./FileIssueDialog";

type Draft = Record<number, { outcome?: Outcome; note: string }>;

/** Runs one case iteration of an execution step by step: each step's outcome and actual result, files, and an
 * issue for a failure (#251). */
export function StepRunner({ project, executionId, caseKey, iteration }: { project: Project; executionId: string; caseKey: string; iteration: number }) {
  const { data: execution } = useTestExecution(executionId);
  const { data: c, error, refetch } = useExecutionCase(executionId, caseKey, iteration);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!c || !execution) return <Skeleton />;
  const runs = execution.cases ?? [];
  const at = runs.findIndex((r) => r.case.key === c.run.case.key && r.iteration === c.run.iteration);
  return (
    <Runner
      key={`${c.run.case.key}-${c.run.iteration}-${c.run.recordedAt ?? ""}`}
      project={project}
      executionId={executionId}
      executionTitle={execution.title}
      record={execution.can.record}
      open={execution.state === "open"}
      c={c}
      previous={at > 0 ? runs[at - 1] : undefined}
      next={at >= 0 && at < runs.length - 1 ? runs[at + 1] : undefined}
    />
  );
}

function Runner({
  project,
  executionId,
  executionTitle,
  record,
  open,
  c,
  previous,
  next,
}: {
  project: Project;
  executionId: string;
  executionTitle: string;
  record: boolean;
  open: boolean;
  c: ExecutionCase;
  previous?: { case: { key: string }; iteration: number };
  next?: { case: { key: string }; iteration: number };
}) {
  const navigate = useNavigate();
  const actions = useCaseActions(executionId, c.run.case.key);
  const [draft, setDraft] = useState<Draft>(() => Object.fromEntries(c.results.map((r) => [r.step, { outcome: r.outcome ?? undefined, note: r.note }])));
  const [note, setNote] = useState(c.run.note);
  const [override, setOverride] = useState<Outcome | "">("");
  const [filing, setFiling] = useState(false);
  const [saved, setSaved] = useState(false);
  const fields = actions.record.error instanceof ApiError ? actions.record.error.fields : {};
  const values = Object.entries(c.run.values ?? {})
    .map(([name, v]) => `${name} = ${v}`)
    .join(", ");
  const goTo = (to: { case: { key: string }; iteration: number }) =>
    void navigate({
      to: "/projects/$projectKey/tests/executions/$executionId/run/$reqKey",
      params: { projectKey: project.key, executionId, reqKey: to.case.key },
      search: { iteration: to.iteration },
    });
  const firstFailed = Object.entries(draft).find(([, d]) => d.outcome === "failed")?.[0];

  function save() {
    setSaved(false);
    actions.record.mutate(
      {
        iteration: c.run.iteration,
        steps: Object.entries(draft)
          .filter(([, d]) => d.outcome)
          .map(([step, d]) => ({ step: Number(step), outcome: d.outcome!, note: d.note || undefined })),
        note: note || undefined,
        ...(override ? { outcome: override } : {}),
      },
      { onSuccess: () => setSaved(true) },
    );
  }

  return (
    <div className="mx-auto max-w-4xl space-y-5" data-step-runner={`${c.run.case.key}#${c.run.iteration}`}>
      <p className="text-sm">
        <Link
          to="/projects/$projectKey/tests/executions/$executionId"
          params={{ projectKey: project.key, executionId }}
          className="text-ink-muted hover:text-accent hover:underline"
        >
          {executionTitle}
        </Link>
      </p>
      <PageHeader
        title={`${c.run.case.key} ${c.run.case.title}`}
        meta={[t.tests.iteration(c.run.iteration, c.iterations), values].filter(Boolean).join(" · ")}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Tag data-case-outcome={c.run.outcome ?? "none"}>{c.run.outcome ? t.tests.outcome[c.run.outcome] : t.tests.notRun}</Tag>
            {previous && (
              <Button variant="ghost" size="sm" onClick={() => goTo(previous)} data-action="previous-case">
                {t.tests.previous}
              </Button>
            )}
            {next && (
              <Button variant="ghost" size="sm" onClick={() => goTo(next)} data-action="next-case">
                {t.tests.next}
              </Button>
            )}
          </div>
        }
      />
      {!open && <p className="text-sm text-ink-muted">{t.tests.closedNote}</p>}
      {open && !record && <p className="text-sm text-ink-muted">{t.tests.readOnly}</p>}
      {Boolean(c.preconditions) && (
        <Card className="space-y-1 p-4">
          <SectionTitle>{t.tests.preconditions}</SectionTitle>
          <DocView doc={c.preconditions as Doc} />
        </Card>
      )}
      <ol className="space-y-3">
        {c.steps.map((s, i) => {
          const n = i + 1;
          const d = draft[n] ?? { note: "" };
          const files = c.files.filter((f) => f.step === n);
          return (
            <li key={n} data-run-step={n} data-step-outcome={d.outcome ?? "none"}>
              <Card className="space-y-2 p-4">
                <p className="text-sm font-semibold text-ink">{t.tests.step(n)}</p>
                <DocView doc={s.action as Doc} />
                {Boolean(s.expected) && (
                  <div>
                    <p className="text-xs font-semibold uppercase text-ink-muted">{t.tests.expected}</p>
                    <DocView doc={s.expected as Doc} />
                  </div>
                )}
                <div role="group" aria-label={t.tests.stepOutcome(n)} className="flex flex-wrap gap-2">
                  {OUTCOMES.map((o) => (
                    <Button
                      key={o}
                      size="sm"
                      variant={d.outcome === o ? (o === "failed" ? "danger" : "primary") : "secondary"}
                      aria-pressed={d.outcome === o}
                      disabled={!record}
                      onClick={() => setDraft({ ...draft, [n]: { ...d, outcome: o } })}
                      data-outcome-choice={o}
                    >
                      {t.tests.outcome[o]}
                    </Button>
                  ))}
                </div>
                <label className="block space-y-1 text-sm">
                  <span className="font-medium text-ink">{t.tests.actual}</span>
                  <Textarea
                    rows={2}
                    value={d.note}
                    maxLength={TEST_NOTE_MAX_LENGTH}
                    readOnly={!record}
                    onChange={(e) => setDraft({ ...draft, [n]: { ...d, note: e.target.value } })}
                    data-step-note={n}
                  />
                </label>
                <FileList files={files} record={record} onRemove={(id) => actions.removeFile.mutate(id)} />
                {record && (
                  <label className="block text-xs text-ink-muted">
                    {t.tests.attach(n)}
                    <input
                      type="file"
                      className="mt-1 block text-sm text-ink"
                      onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) actions.upload.mutate({ file, iteration: c.run.iteration, step: n });
                        e.target.value = "";
                      }}
                      data-step-file={n}
                    />
                  </label>
                )}
              </Card>
            </li>
          );
        })}
      </ol>
      {actions.upload.error && <ErrorBanner>{actions.upload.error.message}</ErrorBanner>}
      <Card className="space-y-3 p-4">
        <label className="block space-y-1 text-sm">
          <span className="font-medium text-ink">{t.tests.caseNote}</span>
          <Textarea rows={2} value={note} maxLength={TEST_NOTE_MAX_LENGTH} readOnly={!record} onChange={(e) => setNote(e.target.value)} />
        </label>
        <Select label={t.tests.caseOutcome} value={override} disabled={!record} onChange={(e) => setOverride(e.target.value as Outcome | "")} controlSize="sm">
          <option value="">{t.tests.fromSteps}</option>
          {OUTCOMES.map((o) => (
            <option key={o} value={o}>
              {t.tests.outcome[o]}
            </option>
          ))}
        </Select>
        {actions.record.error && <ErrorBanner>{Object.values(fields)[0] ?? actions.record.error.message}</ErrorBanner>}
        {saved && (
          <p role="status" className="text-sm text-ink-muted">
            {t.tests.recorded}
          </p>
        )}
        {record && (
          <div className="flex flex-wrap gap-2">
            <Button loading={actions.record.isPending} onClick={save} data-action="record-results">
              {t.tests.record}
            </Button>
            {c.run.outcome === "failed" && (
              <Button variant="secondary" onClick={() => setFiling(true)} data-action="file-issue">
                {t.tests.fileIssue}
              </Button>
            )}
          </div>
        )}
        {c.issues.length > 0 && (
          <p className="text-sm text-ink" data-case-issues>
            {t.tests.issues}: {c.issues.map((i) => i.key).join(", ")}
          </p>
        )}
      </Card>
      {filing && (
        <FileIssueDialog
          execution={executionId}
          caseKey={c.run.case.key}
          iteration={c.run.iteration}
          steps={c.steps.length}
          failedStep={firstFailed ? Number(firstFailed) : undefined}
          onClose={() => setFiling(false)}
        />
      )}
    </div>
  );
}

function FileList({ files, record, onRemove }: { files: ExecutionCase["files"]; record: boolean; onRemove: (id: string) => void }) {
  if (files.length === 0) return null;
  return (
    <ul className="space-y-1 text-sm" aria-label={t.tests.files}>
      {files.map((f) => (
        <li key={f.id} className="flex items-center gap-2" data-run-file={f.fileName}>
          <a href={executionFileHref(f.id)} className="text-accent hover:underline" download>
            {f.fileName}
          </a>
          {record && <IconButton size="sm" icon={<Icon.Trash />} label={t.tests.removeFile(f.fileName)} onClick={() => onRemove(f.id)} />}
        </li>
      ))}
    </ul>
  );
}

import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { executionReportHref, useExecutionActions, useTestExecution, type TestExecution } from "@/api/tests";
import { Button, Card, Checkbox, Dialog, ErrorBanner, Field, PageHeader, SectionTitle, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { TEST_CONFIGURATION_MAX_LENGTH, TEST_DESCRIPTION_MAX_LENGTH, TEST_TITLE_MAX_LENGTH, TEST_VERSION_MAX_LENGTH } from "@/config";
import { ExportMenu } from "@/features/print/ExportMenu";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { progressOf } from "./TestsPage";

const day = localDateFormat({ dateStyle: "medium" });

/** One test execution: its target, testers and progress, every case iteration with its outcome, closing it and its
 * report (#251). */
export function ExecutionPage({ project, executionId }: { project: Project; executionId: string }) {
  const { data: e, error, refetch } = useTestExecution(executionId);
  const [asking, setAsking] = useState<"edit" | "close" | null>(null);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!e) return <Skeleton />;
  const meta = [
    e.planTitle && `${t.tests.plan}: ${e.planTitle}`,
    e.baseline && `${t.tests.baseline}: ${e.baseline.name}`,
    e.version && `${t.tests.version}: ${e.version}`,
    e.configuration && `${t.tests.configuration}: ${e.configuration}`,
    t.tests.managedBy(e.manager.name),
    e.dueAt && t.tests.dueOn(day.format(new Date(e.dueAt))),
  ].filter(Boolean);
  return (
    <div className="mx-auto max-w-5xl space-y-5" data-execution-page={e.id} data-execution-state={e.state}>
      <p className="text-sm">
        <Link to="/p/$projectKey/tests" params={{ projectKey: project.key }} className="text-ink-muted hover:text-accent hover:underline">
          {t.tests.allTests}
        </Link>
      </p>
      <PageHeader
        title={e.title}
        meta={meta.join(" · ")}
        actions={
          <div className="flex flex-wrap gap-2">
            {e.can.manage && (
              <>
                <Button variant="secondary" onClick={() => setAsking("edit")} data-action="edit-execution">
                  {t.tests.edit}
                </Button>
                <Button variant="secondary" onClick={() => setAsking("close")} data-action="close-execution">
                  {t.tests.close}
                </Button>
              </>
            )}
            <ExportMenu
              title={e.title}
              formats={["pdf", "docx"]}
              projectKey={project.key}
              href={(format) => executionReportHref(e.id, format === "docx" ? "docx" : "pdf")}
            />
          </div>
        }
      />
      <p className="flex flex-wrap items-center gap-2 text-sm text-ink" data-execution-progress>
        <Tag>{t.tests.state[e.state]}</Tag>
        {progressOf(e)}
      </p>
      {e.state !== "open" && (
        <p role="status" className="text-sm text-ink-muted">
          {t.tests.closedNote} {e.closeComment}
        </p>
      )}
      {e.state === "open" && !e.can.record && <p className="text-sm text-ink-muted">{t.tests.readOnly}</p>}
      <Card className="space-y-2 p-4">
        <SectionTitle>{t.tests.testers}</SectionTitle>
        <p className="text-sm text-ink">{e.testers.map((s) => s.name).join(", ")}</p>
        {e.excluded.length > 0 && (
          <>
            <p className="text-xs font-semibold uppercase text-ink-muted">{t.tests.excluded}</p>
            <ul className="list-disc pl-5 text-sm text-ink-muted" data-excluded>
              {e.excluded.map((x) => (
                <li key={x.key}>
                  {x.key}: {x.reason}
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>
      <CaseTable project={project} execution={e} />
      {asking === "edit" && <EditDialog project={project} execution={e} onClose={() => setAsking(null)} />}
      {asking === "close" && <CloseDialog project={project} execution={e} onClose={() => setAsking(null)} />}
    </div>
  );
}

function CaseTable({ project, execution: e }: { project: Project; execution: TestExecution }) {
  const runs = e.cases ?? [];
  return (
    <Table aria-label={t.tests.cases}>
      <thead>
        <tr>
          <Th>{t.tests.caseColumn}</Th>
          <Th>{t.tests.iterationColumn}</Th>
          <Th>{t.tests.outcomeColumn}</Th>
          <Th>{t.tests.recordedColumn}</Th>
          <Th>
            <span className="sr-only">{t.tests.run}</span>
          </Th>
        </tr>
      </thead>
      <tbody>
        {runs.map((run) => {
          const values = Object.entries(run.values ?? {})
            .map(([name, v]) => `${name} = ${v}`)
            .join(", ");
          return (
            <tr key={`${run.case.id}-${run.iteration}`} data-case-run={`${run.case.key}#${run.iteration}`} data-outcome={run.outcome ?? "none"}>
              <Td>
                <span className="mr-2 font-mono text-xs text-ink-muted">{run.case.key}</span>
                {run.case.title}
              </Td>
              <Td>{values || run.iteration}</Td>
              <Td>{run.outcome ? t.tests.outcome[run.outcome] : t.tests.notRun}</Td>
              <Td>{run.ci ? t.tests.fromCI : (run.recordedBy?.name ?? "")}</Td>
              <Td className="text-right">
                <Link
                  to="/p/$projectKey/tests/executions/$executionId/run/$reqKey"
                  params={{ projectKey: project.key, executionId: e.id, reqKey: run.case.key }}
                  search={{ iteration: run.iteration }}
                  className="text-sm text-accent hover:underline"
                  aria-label={`${e.can.record ? t.tests.run : t.tests.open} ${run.case.key} ${values}`.trim()}
                >
                  {e.can.record ? t.tests.run : t.tests.open}
                </Link>
              </Td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}

function EditDialog({ project, execution: e, onClose }: { project: Project; execution: TestExecution; onClose: () => void }) {
  const { update } = useExecutionActions(project.key);
  const [title, setTitle] = useState(e.title);
  const [version, setVersion] = useState(e.version);
  const [configuration, setConfiguration] = useState(e.configuration);
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: e.id, title, version, configuration }, { onSuccess: onClose });
  }
  return (
    <Dialog open title={t.tests.editTitle} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        {update.error && Object.keys(fields).length === 0 && <ErrorBanner>{update.error.message}</ErrorBanner>}
        <Field
          label={t.tests.executionTitle}
          value={title}
          maxLength={TEST_TITLE_MAX_LENGTH}
          error={fields.title}
          onChange={(ev) => setTitle(ev.target.value)}
        />
        <Field
          label={t.tests.version}
          value={version}
          maxLength={TEST_VERSION_MAX_LENGTH}
          error={fields.version}
          onChange={(ev) => setVersion(ev.target.value)}
        />
        <Field
          label={t.tests.configuration}
          value={configuration}
          maxLength={TEST_CONFIGURATION_MAX_LENGTH}
          error={fields.configuration}
          onChange={(ev) => setConfiguration(ev.target.value)}
        />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.tests.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} data-action="save-execution">
            {t.tests.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function CloseDialog({ project, execution: e, onClose }: { project: Project; execution: TestExecution; onClose: () => void }) {
  const { close } = useExecutionActions(project.key);
  const [comment, setComment] = useState("");
  const [abort, setAbort] = useState(false);
  function submit(event: FormEvent) {
    event.preventDefault();
    close.mutate({ id: e.id, comment, abort }, { onSuccess: onClose });
  }
  return (
    <Dialog open title={t.tests.closeTitle} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-sm text-ink">{t.tests.closeBody}</p>
        {close.error && <ErrorBanner>{close.error.message}</ErrorBanner>}
        <Field label={t.tests.closeComment} rows={2} value={comment} maxLength={TEST_DESCRIPTION_MAX_LENGTH} onChange={(ev) => setComment(ev.target.value)} />
        <Checkbox label={t.tests.abort} checked={abort} onChange={(ev) => setAbort(ev.target.checked)} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.tests.cancel}
          </Button>
          <Button type="submit" variant={abort ? "danger" : "primary"} loading={close.isPending} data-action="close-execution-send">
            {t.tests.closeSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

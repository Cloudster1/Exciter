import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useTestExecutions, useTestPlanActions, useTestPlans, type TestExecution } from "@/api/tests";
import { Icon } from "@/components/icons";
import { Button, Dialog, EmptyState, ErrorBanner, Field, PageHeader, Radio, Skeleton, Table, TabPanel, Tabs, Tag, Td, Th } from "@/components/ui";
import { TEST_TITLE_MAX_LENGTH, TEST_DESCRIPTION_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const day = localDateFormat({ dateStyle: "medium" });

type Tab = "plans" | "executions";

/** A project's tests: its plans, and its executions with how far each has got (#251). */
export function TestsPage({ project }: { project: Project }) {
  const [tab, setTab] = useState<Tab>("executions");
  return (
    <div className="mx-auto max-w-5xl space-y-5" data-tests-page={project.key}>
      <PageHeader
        title={t.tests.title}
        meta={t.tests.intro}
        tabs={
          <Tabs<Tab>
            label={t.tests.title}
            value={tab}
            onChange={setTab}
            panelId="tests-panel"
            tabs={[
              { value: "executions", label: t.tests.executions, attrs: { "data-tab": "executions" } },
              { value: "plans", label: t.tests.plans, attrs: { "data-tab": "plans" } },
            ]}
          />
        }
      />
      <TabPanel id="tests-panel">{tab === "plans" ? <PlanList project={project} /> : <ExecutionList project={project} />}</TabPanel>
    </div>
  );
}

function PlanList({ project }: { project: Project }) {
  const { data: plans, error, refetch } = useTestPlans(project.key);
  const [making, setMaking] = useState(false);
  const mayEdit = project.can.edit && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{t.tests.failed}</ErrorBanner>;
  if (!plans) return <Skeleton />;
  return (
    <div className="space-y-3">
      {mayEdit && (
        <Button icon={<Icon.Plus />} onClick={() => setMaking(true)} data-action="new-plan">
          {t.tests.newPlan}
        </Button>
      )}
      {plans.length === 0 ? (
        <EmptyState title={t.tests.noPlans} />
      ) : (
        <ul className="divide-y divide-border rounded-overlay border border-border bg-surface">
          {plans.map((plan) => (
            <li key={plan.id} className="flex flex-wrap items-baseline gap-2 px-4 py-3" data-plan={plan.title}>
              <Link
                to="/p/$projectKey/tests/plans/$planId"
                params={{ projectKey: project.key, planId: plan.id }}
                className="font-medium text-ink hover:underline"
              >
                {plan.title}
              </Link>
              <Tag>{plan.scope === "query" ? t.tests.byQuery : t.tests.picked}</Tag>
              {plan.description && <span className="w-full text-sm text-ink-muted">{plan.description}</span>}
            </li>
          ))}
        </ul>
      )}
      {making && <NewPlanDialog project={project} onClose={() => setMaking(false)} />}
    </div>
  );
}

function NewPlanDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const { create } = useTestPlanActions(project.key);
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [scope, setScope] = useState<"cases" | "query">("cases");
  const [query, setQuery] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { title: title.trim(), description: description.trim() || undefined, scope, ...(scope === "query" ? { query } : {}) },
      { onSuccess: (plan) => void navigate({ to: "/p/$projectKey/tests/plans/$planId", params: { projectKey: project.key, planId: plan.id } }) },
    );
  }

  return (
    <Dialog title={t.tests.newPlanTitle} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3" noValidate data-plan-form>
        {create.error && Object.keys(fields).length === 0 && <ErrorBanner>{create.error.message}</ErrorBanner>}
        <Field
          label={t.tests.planTitle}
          value={title}
          maxLength={TEST_TITLE_MAX_LENGTH}
          error={fields.title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
        <Field
          label={t.tests.description}
          rows={2}
          value={description}
          maxLength={TEST_DESCRIPTION_MAX_LENGTH}
          error={fields.description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium text-ink">{t.tests.scope}</legend>
          <Radio name="plan-scope" label={t.tests.scopeCases} checked={scope === "cases"} onChange={() => setScope("cases")} />
          <Radio name="plan-scope" label={t.tests.scopeQuery} checked={scope === "query"} onChange={() => setScope("query")} />
        </fieldset>
        {scope === "query" && (
          <Field label={t.tests.query} hint={t.tests.queryHint} value={query} error={fields.query} onChange={(e) => setQuery(e.target.value)} />
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.tests.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!title.trim()} data-action="make-plan">
            {t.tests.makePlan}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** How far an execution has got, in words. */
export function progressOf(e: TestExecution): string {
  const p = e.progress;
  return t.tests.progress(p.total - p.notRun, p.total, p.failed);
}

function ExecutionList({ project }: { project: Project }) {
  const { data: executions, error, refetch } = useTestExecutions(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{t.tests.failed}</ErrorBanner>;
  if (!executions) return <Skeleton />;
  if (executions.length === 0) return <EmptyState title={t.tests.noExecutions} />;
  return <ExecutionTable executions={executions} label={t.tests.executions} />;
}

/** Executions as a table: each with its state, plan, target and progress, leading to its page. */
export function ExecutionTable({ executions, label }: { executions: TestExecution[]; label: string }) {
  return (
    <Table aria-label={label}>
      <thead>
        <tr>
          <Th>{t.tests.executionTitle}</Th>
          <Th>{t.tests.plan}</Th>
          <Th>{t.tests.version}</Th>
          <Th>{t.tests.outcomeColumn}</Th>
          <Th>{t.tests.due}</Th>
        </tr>
      </thead>
      <tbody>
        {executions.map((e) => (
          <tr key={e.id} data-execution={e.title}>
            <Td>
              <Link
                to="/p/$projectKey/tests/executions/$executionId"
                params={{ projectKey: e.projectKey, executionId: e.id }}
                className="font-medium text-ink hover:underline"
              >
                {e.title}
              </Link>{" "}
              <Tag>{t.tests.state[e.state]}</Tag>
            </Td>
            <Td>{e.planTitle}</Td>
            <Td>{[e.version, e.configuration].filter(Boolean).join(", ")}</Td>
            <Td>{progressOf(e)}</Td>
            <Td>{e.dueAt ? day.format(new Date(e.dueAt)) : ""}</Td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

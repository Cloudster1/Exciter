import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useTypes } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { useGrid } from "@/api/search";
import { useTestPlan, useTestPlanActions, type TestPlan } from "@/api/tests";
import { Icon } from "@/components/icons";
import { Button, Card, Dialog, ErrorBanner, Field, IconButton, PageHeader, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { TEST_CASE_PICKER_LIMIT } from "@/config";
import { t } from "@/i18n";
import { StartExecutionDialog } from "./StartExecutionDialog";

/** A test plan: its cases in order, picked or found by its query, and the executions started from it (#251). */
export function PlanPage({ project, planId }: { project: Project; planId: string }) {
  const { data: plan, error, refetch } = useTestPlan(planId);
  const [starting, setStarting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const mayEdit = project.can.edit && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!plan) return <Skeleton />;
  return (
    <div className="mx-auto max-w-4xl space-y-5" data-plan-page={plan.id}>
      <p className="text-sm">
        <Link to="/projects/$projectKey/tests" params={{ projectKey: project.key }} className="text-ink-muted hover:text-accent hover:underline">
          {t.tests.allTests}
        </Link>
      </p>
      <PageHeader
        title={plan.title}
        meta={plan.scope === "query" ? t.tests.byQuery : t.tests.picked}
        actions={
          mayEdit && (
            <div className="flex flex-wrap gap-2">
              <Button icon={<Icon.Plus />} onClick={() => setStarting(true)} disabled={plan.cases.length === 0} data-action="start-execution">
                {t.tests.start}
              </Button>
              <Button variant="ghost" icon={<Icon.Trash />} onClick={() => setDeleting(true)} data-action="delete-plan">
                {t.tests.deletePlan}
              </Button>
            </div>
          )
        }
      />
      {plan.description && <p className="text-sm text-ink-muted">{plan.description}</p>}
      {plan.scope === "query" ? (
        <QueryCases project={project} plan={plan} mayEdit={mayEdit} />
      ) : (
        <PickedCases project={project} plan={plan} mayEdit={mayEdit} />
      )}
      {starting && <StartExecutionDialog project={project} plan={plan} onClose={() => setStarting(false)} />}
      {deleting && <DeletePlanDialog project={project} plan={plan} onClose={() => setDeleting(false)} />}
    </div>
  );
}

function CaseLink({ project, caseKey, title }: { project: Project; caseKey: string; title: string }) {
  return (
    <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: project.key, reqKey: caseKey }} className="text-ink hover:underline">
      <span className="mr-2 font-mono text-xs text-ink-muted">{caseKey}</span>
      {title}
    </Link>
  );
}

function PickedCases({ project, plan, mayEdit }: { project: Project; plan: TestPlan; mayEdit: boolean }) {
  const { setCases } = useTestPlanActions(project.key);
  const { data: types } = useTypes(project.key);
  const testTypes = (types ?? []).filter((type) => type.role === "testCase").map((type) => type.id);
  const { data: grid } = useGrid(project.key, { type: testTypes }, { limit: TEST_CASE_PICKER_LIMIT, offset: 0 });
  const [pick, setPick] = useState("");
  const keys = plan.cases.map((c) => c.key);
  const offered = testTypes.length === 0 ? [] : (grid?.rows ?? []).filter((row) => !keys.includes(row.key));
  const save = (next: string[]) => setCases.mutate({ id: plan.id, cases: next });
  const move = (from: number, to: number) => {
    const next = [...keys];
    const [key] = next.splice(from, 1);
    if (key) next.splice(to, 0, key);
    save(next);
  };
  const fields = setCases.error instanceof ApiError ? setCases.error.fields : {};

  return (
    <Card className="space-y-3 p-4">
      <SectionTitle>{t.tests.cases}</SectionTitle>
      {(fields.cases || (setCases.error && !fields.cases)) && <ErrorBanner>{fields.cases ?? setCases.error?.message}</ErrorBanner>}
      {plan.cases.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.tests.noCases}</p>
      ) : (
        <ol className="space-y-1" data-plan-cases>
          {plan.cases.map((c, i) => (
            <li key={c.id} className="flex items-center gap-1 text-sm" data-plan-case={c.key}>
              <span className="w-6 tabular-nums text-ink-muted">{i + 1}</span>
              <span className="flex-1">
                <CaseLink project={project} caseKey={c.key} title={c.title} />
                {c.trashed && <Tag className="ml-2">{t.tests.trashed}</Tag>}
              </span>
              {mayEdit && (
                <>
                  <IconButton size="sm" icon={<Icon.ChevronUp />} label={t.tests.moveUp(c.key)} disabled={i === 0} onClick={() => move(i, i - 1)} />
                  <IconButton
                    size="sm"
                    icon={<Icon.ChevronDown />}
                    label={t.tests.moveDown(c.key)}
                    disabled={i === plan.cases.length - 1}
                    onClick={() => move(i, i + 1)}
                  />
                  <IconButton size="sm" icon={<Icon.X />} label={t.tests.remove(c.key)} onClick={() => save(keys.filter((k) => k !== c.key))} />
                </>
              )}
            </li>
          ))}
        </ol>
      )}
      {mayEdit && (
        <div className="flex items-end gap-2">
          <Select label={t.tests.addCase} id="plan-add-case" value={pick} onChange={(e) => setPick(e.target.value)} controlSize="sm">
            <option value="">{t.tests.pickCase}</option>
            {offered.map((row) => (
              <option key={row.key} value={row.key}>{`${row.key} ${row.title}`}</option>
            ))}
          </Select>
          <Button
            size="sm"
            variant="secondary"
            icon={<Icon.Plus />}
            disabled={!pick}
            loading={setCases.isPending}
            onClick={() => {
              save([...keys, pick]);
              setPick("");
            }}
            data-action="add-plan-case"
          >
            {t.tests.add}
          </Button>
        </div>
      )}
    </Card>
  );
}

function QueryCases({ project, plan, mayEdit }: { project: Project; plan: TestPlan; mayEdit: boolean }) {
  const { update } = useTestPlanActions(project.key);
  const [query, setQuery] = useState(plan.query);
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: plan.id, query });
  }
  return (
    <Card className="space-y-3 p-4">
      <SectionTitle>{t.tests.cases}</SectionTitle>
      {mayEdit && (
        <form onSubmit={submit} className="flex items-end gap-2">
          <Field
            label={t.tests.query}
            hint={t.tests.queryHint}
            value={query}
            error={fields.query ?? update.error?.message}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" size="sm" loading={update.isPending} data-action="save-plan-query">
            {t.tests.save}
          </Button>
        </form>
      )}
      {plan.cases.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.tests.noCases}</p>
      ) : (
        <ol className="list-decimal space-y-1 pl-6 text-sm" data-plan-cases>
          {plan.cases.map((c) => (
            <li key={c.id} data-plan-case={c.key}>
              <CaseLink project={project} caseKey={c.key} title={c.title} />
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}

function DeletePlanDialog({ project, plan, onClose }: { project: Project; plan: TestPlan; onClose: () => void }) {
  const { remove } = useTestPlanActions(project.key);
  const navigate = useNavigate();
  return (
    <Dialog open title={t.tests.deletePlanTitle(plan.title)} onClose={onClose}>
      <p className="text-sm text-ink">{t.tests.deletePlanBody}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <div className="flex justify-end gap-2 pt-3">
        <Button variant="secondary" onClick={onClose}>
          {t.tests.cancel}
        </Button>
        <Button
          variant="danger"
          loading={remove.isPending}
          onClick={() => remove.mutate(plan.id, { onSuccess: () => void navigate({ to: "/projects/$projectKey/tests", params: { projectKey: project.key } }) })}
          data-action="confirm-delete-plan"
        >
          {t.tests.delete}
        </Button>
      </div>
    </Dialog>
  );
}

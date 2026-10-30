import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useTypes, type RequirementType } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { useStatuses, useWorkflowActions, useWorkflows, type Status, type Workflow } from "@/api/workflows";
import { Button, Card, Dialog, ErrorBanner, SectionTitle, Select, SelectInput, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NEW_WORKFLOW } from "@/config";
import { t } from "@/i18n";
import { StatusList } from "./StatusList";
import { WorkflowGraph } from "./WorkflowGraph";

/** Who follows which workflow, about to change: the project when there is no type, else that type. */
type Assigning = { workflow: Workflow; typeId?: string; from: Workflow };

/** The Workflow tab of a project's settings: the workflow it follows drawn out, its statuses, its workflows,
 * and which of them each type follows. */
export function WorkflowSettings({ project }: { project: Project }) {
  const { data: workflows, error, refetch } = useWorkflows(project.key);
  const { data: statuses } = useStatuses(project.key);
  const { data: types } = useTypes(project.key);
  const { remove } = useWorkflowActions(project.key);
  const [assigning, setAssigning] = useState<Assigning | null>(null);
  const navigate = useNavigate();
  const design = (workflowId: string) => navigate({ to: "/p/$projectKey/settings/workflows/$workflowId", params: { projectKey: project.key, workflowId } });
  const mayChange = project.can.administer && !project.archivedAt;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!workflows || !statuses) return <Skeleton />;
  const followed = workflows.find((workflow) => workflow.projectDefault) ?? workflows[0];
  const byId = new Map(workflows.map((workflow) => [workflow.id, workflow]));
  const typeFlow = (type: RequirementType) => (type.workflowId ? byId.get(type.workflowId) : undefined) ?? followed;
  const typeNames = (workflow: Workflow) => (types ?? []).filter((type) => workflow.typeIds.includes(type.id)).map((type) => type.name);

  return (
    <div className="space-y-6" data-workflow-settings>
      {followed && <WorkflowGraph workflow={followed} statuses={statuses} caption={t.workflows.followedCaption} />}

      <section className="space-y-3" aria-labelledby="workflows-title">
        <SectionTitle id="workflows-title">{t.workflows.workflowsTitle}</SectionTitle>
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
        {workflows.map((workflow) => {
          const usedBy = typeNames(workflow);
          return (
            <Card key={workflow.id} className="flex flex-wrap items-start justify-between gap-3 p-4" data-workflow-card={workflow.name}>
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-2 text-sm font-medium text-ink">
                  {workflow.name}
                  {workflow.projectDefault && <Tag>{t.workflows.projectDefault}</Tag>}
                </p>
                <p className="mt-0.5 text-xs text-ink-muted">
                  {usedBy.length ? t.workflows.typesFollow(usedBy.join(", ")) : t.workflows.counts(workflow.steps.length, workflow.transitions.length)}
                </p>
                {workflow.description && <p className="mt-1 text-sm text-ink-muted">{workflow.description}</p>}
              </div>
              {mayChange && (
                <div className="flex gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    icon={<Icon.Edit />}
                    onClick={() => void design(workflow.id)}
                    data-action="design-workflow"
                    aria-label={t.workflows.designOne(workflow.name)}
                  >
                    {t.workflows.design}
                  </Button>
                  {!workflow.projectDefault && usedBy.length === 0 && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => remove.mutate(workflow.id)}
                      data-action="delete-workflow"
                      aria-label={t.workflows.deleteOne(workflow.name)}
                    >
                      {t.workflows.delete}
                    </Button>
                  )}
                </div>
              )}
            </Card>
          );
        })}
        {mayChange && (
          <Button icon={<Icon.Plus />} variant="secondary" onClick={() => void design(NEW_WORKFLOW)} data-action="new-workflow">
            {t.workflows.newWorkflow}
          </Button>
        )}
      </section>

      <section className="space-y-3" aria-labelledby="assignment-title">
        <SectionTitle id="assignment-title">{t.workflows.assignmentTitle}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.workflows.assignmentIntro}</p>
        <Card className="space-y-3 p-4" data-workflow-assignment>
          <Select
            label={t.workflows.projectFollows}
            value={followed?.id ?? ""}
            disabled={!mayChange}
            onChange={(event) => {
              const workflow = byId.get(event.target.value);
              if (workflow && followed) setAssigning({ workflow, from: followed });
            }}
            data-assign="project"
          >
            {workflows.map((workflow) => (
              <option key={workflow.id} value={workflow.id}>
                {workflow.name}
              </option>
            ))}
          </Select>
          {(types ?? [])
            .filter((type) => !type.retired)
            .map((type) => (
              <div key={type.id} className="flex flex-wrap items-center justify-between gap-2 text-sm" data-assign-type={type.name}>
                <span className="text-ink">{type.name}</span>
                <SelectInput
                  aria-label={t.workflows.typeFollows(type.name)}
                  value={type.workflowId ?? ""}
                  disabled={!mayChange}
                  onChange={(event) => {
                    const workflow = byId.get(event.target.value);
                    if (workflow) setAssigning({ workflow, typeId: type.id, from: typeFlow(type)! });
                  }}
                >
                  {!type.workflowId && <option value="">{t.workflows.followsProject(followed?.name ?? "")}</option>}
                  {workflows.map((workflow) => (
                    <option key={workflow.id} value={workflow.id}>
                      {workflow.name}
                    </option>
                  ))}
                </SelectInput>
              </div>
            ))}
        </Card>
      </section>

      <section className="space-y-3" aria-labelledby="statuses-title">
        <SectionTitle id="statuses-title">{t.workflows.statusesTitle}</SectionTitle>
        <StatusList project={project} mayChange={mayChange} />
      </section>

      {assigning && <AssignDialog project={project} assigning={assigning} statuses={statuses} onClose={() => setAssigning(null)} />}
    </div>
  );
}

/** Asks where requirements go whose status the chosen workflow lacks, then assigns it; with none, it assigns
 * as soon as it is confirmed. */
function AssignDialog({ project, assigning, statuses, onClose }: { project: Project; assigning: Assigning; statuses: Status[]; onClose: () => void }) {
  const { assign } = useWorkflowActions(project.key);
  const { workflow, typeId, from } = assigning;
  const has = new Set(workflow.steps.map((step) => step.status.id));
  const initial = workflow.steps.find((step) => step.isInitial)?.status.id ?? "";
  const lacking = from.steps
    .map((step) => step.status)
    .filter((status) => !has.has(status.id) && (statuses.find((each) => each.id === status.id)?.requirements ?? 0) > 0);
  const [mapping, setMapping] = useState<Record<string, string>>(() => Object.fromEntries(lacking.map((status) => [status.id, initial])));
  const fields = assign.error instanceof ApiError ? assign.error.fields : {};

  return (
    <Dialog open title={t.workflows.assignTitle(workflow.name)} onClose={onClose} attrs={{ "data-assign-dialog": "" }}>
      <div className="space-y-3">
        <p className="text-sm text-ink">{lacking.length ? t.workflows.mapIntro : t.workflows.nothingToMap}</p>
        {lacking.map((status) => (
          <Select
            key={status.id}
            label={t.workflows.mapFrom(status.name)}
            value={mapping[status.id] ?? ""}
            onChange={(event) => setMapping((current) => ({ ...current, [status.id]: event.target.value }))}
            data-map-from={status.name}
          >
            {workflow.steps.map((step) => (
              <option key={step.status.id} value={step.status.id}>
                {step.status.name}
              </option>
            ))}
          </Select>
        ))}
        {assign.error && <ErrorBanner>{fields.statusMapping ?? assign.error.message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button
            loading={assign.isPending}
            onClick={() => assign.mutate({ workflowId: workflow.id, typeId, statusMapping: mapping }, { onSuccess: onClose })}
            data-action="confirm-assign"
          >
            {t.workflows.assign}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

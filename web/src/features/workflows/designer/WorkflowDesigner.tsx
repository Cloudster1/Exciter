import { useMemo, useState } from "react";
import { ApiError } from "@/api/client";
import { useRuleTypes, useStatuses, useWorkflow, useWorkflowActions, type Status, type Workflow } from "@/api/workflows";
import { Button, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { t } from "@/i18n";
import type { Selection } from "./flow";
import { connect, emptyDesign, fromWorkflow, moveNode, problems, toInput, type Design } from "./graph";
import { Inspector } from "./Inspector";
import { WorkflowCanvas } from "./WorkflowCanvas";

/** Draws a project's workflow as the graph it is: its statuses arranged on the canvas, and arrows saying how
 * a requirement may move between them. */
export function WorkflowDesigner({
  projectKey,
  workflowId,
  onDone,
  onCancel,
}: {
  projectKey: string;
  workflowId?: string;
  onDone: (saved: Workflow) => void;
  onCancel: () => void;
}) {
  const { data: statusList } = useStatuses(projectKey);
  const { data: existing } = useWorkflow(projectKey, workflowId ?? "");
  const { data: catalogue } = useRuleTypes();

  // A status made in the inspector is known here at once, so its card draws before the list is read again.
  const [coined, setCoined] = useState<Status[]>([]);
  const statuses = useMemo(() => new Map([...(statusList ?? []), ...coined].map((status) => [status.id, status])), [statusList, coined]);

  const [design, setDesign] = useState<{ loadedFrom?: string } & Design>(() => emptyDesign());
  const [selection, setSelection] = useState<Selection>({ kind: "none" });

  // The canvas is seeded once the workflow arrives, and not again, so a refetch never undoes a drag.
  if (existing && design.loadedFrom !== existing.id) {
    setDesign({ loadedFrom: existing.id, ...fromWorkflow(existing) });
  }

  const { create, save } = useWorkflowActions(projectKey);
  const pending = create.isPending || save.isPending;
  const error = create.error ?? save.error;
  const fields = error instanceof ApiError ? error.fields : {};
  const hints = problems(design, statuses);

  function submit() {
    const input = toInput(design);
    const done = { onSuccess: (saved: Workflow) => onDone(saved) };
    if (workflowId) save.mutate({ id: workflowId, ...input }, done);
    else create.mutate(input, done);
  }

  function connectNodes(from: string, to: string) {
    const result = connect(design, from, to, statuses.get(to)?.name ?? "");
    if (!result) return;
    setDesign((current) => ({ ...current, ...result.design }));
    setSelection({ kind: "edge", key: result.key });
  }

  const loading = workflowId !== undefined && !existing;

  return (
    <div className="space-y-3" data-workflow-designer>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-64 flex-1">
          <Field
            label={t.workflows.name}
            id="field-workflow-name"
            value={design.name}
            onChange={(event) => setDesign((current) => ({ ...current, name: event.target.value }))}
            error={fields.name}
          />
        </div>
        <div className="flex gap-2">
          <Button loading={pending} disabled={loading} onClick={submit} data-action="save-workflow">
            {workflowId ? t.workflows.save : t.workflows.create}
          </Button>
          <Button variant="secondary" onClick={onCancel}>
            {t.common.cancel}
          </Button>
        </div>
      </div>
      {hints.length > 0 && (
        <p className="text-sm text-ink-muted" data-design-hints>
          {t.workflows.beforeSave(hints.join(" "))}
        </p>
      )}
      {error && (
        <ErrorBanner>
          {[
            error.message,
            ...Object.entries(fields)
              .filter(([field]) => field !== "name")
              .map(([, message]) => message),
          ].join(" ")}
        </ErrorBanner>
      )}
      <div className="flex flex-col items-start gap-3 lg:flex-row">
        <div className="max-h-[70vh] w-full min-w-0 flex-1 overflow-auto rounded-overlay border border-border">
          {loading ? (
            <Skeleton />
          ) : (
            <WorkflowCanvas
              design={design}
              statuses={statuses}
              selection={selection}
              onSelect={setSelection}
              onMove={(statusId, x, y) => setDesign((current) => ({ ...current, ...moveNode(current, statusId, x, y) }))}
              onConnect={connectNodes}
            />
          )}
        </div>
        <aside className="w-full shrink-0 rounded-overlay border border-border bg-surface p-4 lg:w-80" aria-label={t.workflows.inspector}>
          <Inspector
            projectKey={projectKey}
            design={design}
            statuses={statuses}
            catalogue={catalogue ?? []}
            selection={selection}
            onSelect={setSelection}
            onChange={(next) => setDesign((current) => ({ ...current, ...next }))}
            onCoined={(status) => setCoined((current) => [...current, status])}
          />
        </aside>
      </div>
    </div>
  );
}

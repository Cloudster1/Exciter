import { useMemo } from "react";
import type { Status, Workflow } from "@/api/workflows";
import { Card } from "@/components/ui";
import { t } from "@/i18n";
import { fromWorkflow } from "./designer/graph";
import { WorkflowCanvas } from "./designer/WorkflowCanvas";

/** One workflow, drawn as the designer left it and touchable by nobody, so a choice between workflows is a
 * choice between pictures. */
export function WorkflowGraph({ workflow, statuses, caption }: { workflow: Workflow; statuses: Status[]; caption?: string }) {
  const design = useMemo(() => fromWorkflow(workflow), [workflow]);
  const byId = useMemo(() => new Map(statuses.map((status) => [status.id, status])), [statuses]);
  return (
    <Card className="overflow-hidden" data-workflow-graph={workflow.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-border px-4 py-2.5">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-medium text-ink">{workflow.name}</h2>
          {caption && <p className="text-xs text-ink-muted">{caption}</p>}
        </div>
        <p className="text-xs text-ink-subtle tabular-nums">{t.workflows.counts(workflow.steps.length, workflow.transitions.length)}</p>
      </div>
      <div className="overflow-x-auto">
        <WorkflowCanvas design={design} statuses={byId} readOnly label={t.workflows.graphLabel(workflow.name)} />
      </div>
    </Card>
  );
}

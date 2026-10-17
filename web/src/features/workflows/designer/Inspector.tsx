import { useState } from "react";
import type { RuleType, Status } from "@/api/workflows";
import { Button, Field, Select } from "@/components/ui";
import { t } from "@/i18n";
import { useProjectLinkTypes } from "@/api/links";
import { StatusBadge } from "../StatusBadge";
import type { Selection } from "./flow";
import { ANYWHERE, addStatus, connect, removeEdge, removeStatus, setInitial, setOnRevision, updateEdge, type Design, type Edge } from "./graph";
import { NewStatusForm } from "./NewStatusForm";
import { RuleEditor } from "./RuleEditor";

/** The panel beside the canvas: whatever is selected, and the keyboard's way of doing what the pointer does
 * there, adding a status, drawing a transition, choosing where requirements start. */
export function Inspector({
  projectKey,
  design,
  statuses,
  catalogue,
  selection,
  onSelect,
  onChange,
  onCoined,
}: {
  projectKey: string;
  design: Design;
  statuses: Map<string, Status>;
  catalogue: RuleType[];
  selection: Selection;
  onSelect: (selection: Selection) => void;
  onChange: (design: Design) => void;
  onCoined: (status: Status) => void;
}) {
  if (selection.kind === "edge") {
    const edge = design.edges.find((each) => each.key === selection.key);
    if (edge) {
      return (
        <EdgePanel
          projectKey={projectKey}
          edge={edge}
          statuses={statuses}
          catalogue={catalogue}
          onChange={(patch) => onChange(updateEdge(design, edge.key, patch))}
          onRemove={() => {
            onChange(removeEdge(design, edge.key));
            onSelect({ kind: "none" });
          }}
        />
      );
    }
  }
  if (selection.kind === "node") {
    const status = statuses.get(selection.statusId);
    if (status && design.nodes.some((node) => node.statusId === status.id)) {
      return <NodePanel status={status} design={design} statuses={statuses} onChange={onChange} onSelect={onSelect} />;
    }
  }
  return <DesignPanel projectKey={projectKey} design={design} statuses={statuses} onChange={onChange} onSelect={onSelect} onCoined={onCoined} />;
}

function DesignPanel({
  projectKey,
  design,
  statuses,
  onChange,
  onSelect,
  onCoined,
}: {
  projectKey: string;
  design: Design;
  statuses: Map<string, Status>;
  onChange: (design: Design) => void;
  onSelect: (selection: Selection) => void;
  onCoined: (status: Status) => void;
}) {
  const [adding, setAdding] = useState("");
  const available = [...statuses.values()].filter((status) => !design.nodes.some((node) => node.statusId === status.id));

  // Adding leaves this panel open, because the next thing is usually another.
  function add() {
    const status = statuses.get(adding);
    if (!status) return;
    onChange(addStatus(design, status));
    setAdding("");
  }

  return (
    <div className="space-y-4">
      <Field
        label={t.workflows.description}
        id="field-workflow-description"
        value={design.description}
        rows={2}
        onChange={(event) => onChange({ ...design, description: event.target.value })}
      />
      <div className="space-y-2">
        <Select label={t.workflows.addStatus} id="field-add-status" value={adding} onChange={(event) => setAdding(event.target.value)}>
          <option value="">{available.length ? t.workflows.chooseStatus : t.workflows.allPlaced}</option>
          {available.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!adding} onClick={add} data-action="add-status">
          {t.workflows.addStatusButton}
        </Button>
      </div>
      <NewStatusForm
        projectKey={projectKey}
        onCreated={(status) => {
          onCoined(status);
          onChange(addStatus(design, status));
          onSelect({ kind: "node", statusId: status.id });
        }}
      />
      <div className="space-y-1 text-xs text-ink-subtle">
        {t.workflows.designerHelp.map((line) => (
          <p key={line}>{line}</p>
        ))}
      </div>
    </div>
  );
}

function NodePanel({
  status,
  design,
  statuses,
  onChange,
  onSelect,
}: {
  status: Status;
  design: Design;
  statuses: Map<string, Status>;
  onChange: (design: Design) => void;
  onSelect: (selection: Selection) => void;
}) {
  const [target, setTarget] = useState("");
  const others = design.nodes.map((node) => statuses.get(node.statusId)).filter((each): each is Status => each !== undefined && each.id !== status.id);
  const leaving = design.edges.filter((edge) => edge.from === status.id);
  const isInitial = design.initialId === status.id;
  const onRevision = design.nodes.find((node) => node.statusId === status.id)?.onRevisionId ?? "";

  function addTransition() {
    const to = statuses.get(target);
    if (!to) return;
    const result = connect(design, status.id, to.id, to.name);
    if (!result) return;
    onChange(result.design);
    onSelect({ kind: "edge", key: result.key });
    setTarget("");
  }

  return (
    <div className="space-y-4" data-node-panel={status.name}>
      <div>
        <StatusBadge name={status.name} category={status.category} className="text-sm" />
        {status.description && <p className="mt-1 text-xs text-ink-muted">{status.description}</p>}
      </div>
      {isInitial ? (
        <p className="rounded-control bg-accent-subtle px-2 py-1.5 text-sm text-accent">{t.workflows.startsHere}</p>
      ) : (
        <Button size="sm" variant="secondary" onClick={() => onChange(setInitial(design, status.id))} data-action="make-initial">
          {t.workflows.makeInitial}
        </Button>
      )}
      <Select
        label={t.workflows.onRevision}
        hint={t.workflows.onRevisionHint}
        id="field-on-revision"
        value={onRevision}
        onChange={(event) => onChange(setOnRevision(design, status.id, event.target.value))}
      >
        <option value="">{t.workflows.stays}</option>
        {others.map((each) => (
          <option key={each.id} value={each.id}>
            {each.name}
          </option>
        ))}
      </Select>
      <div className="space-y-2">
        <Select label={t.workflows.addTransitionTo} id="field-to-status" value={target} onChange={(event) => setTarget(event.target.value)}>
          <option value="">{others.length ? t.workflows.chooseStatus : t.workflows.addAnotherFirst}</option>
          {others.map((each) => (
            <option key={each.id} value={each.id}>
              {each.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!target} onClick={addTransition} data-action="add-transition">
          {t.workflows.addTransitionButton}
        </Button>
      </div>
      {leaving.length > 0 && (
        <div>
          <p className="text-sm font-medium text-ink-muted">{t.workflows.leavingHere}</p>
          <ul className="mt-1 space-y-0.5">
            {leaving.map((edge) => (
              <li key={edge.key}>
                <Button variant="link" onClick={() => onSelect({ kind: "edge", key: edge.key })}>
                  {t.workflows.edgeTo(edge.name || t.workflows.unnamed, statuses.get(edge.to)?.name ?? "")}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          onChange(removeStatus(design, status.id));
          onSelect({ kind: "none" });
        }}
        data-action="remove-status"
      >
        {t.workflows.removeFromWorkflow}
      </Button>
    </div>
  );
}

function EdgePanel({
  projectKey,
  edge,
  statuses,
  catalogue,
  onChange,
  onRemove,
}: {
  projectKey: string;
  edge: Edge;
  statuses: Map<string, Status>;
  catalogue: RuleType[];
  onChange: (patch: Partial<Edge>) => void;
  onRemove: () => void;
}) {
  const { data: linkTypes } = useProjectLinkTypes(projectKey);
  const from = edge.from === ANYWHERE ? t.workflows.anyStatusLower : (statuses.get(edge.from)?.name ?? "");
  const to = statuses.get(edge.to)?.name ?? "";
  return (
    <div className="space-y-4" data-edge-panel>
      <Field label={t.workflows.transitionName} id="field-transition-name" value={edge.name} onChange={(event) => onChange({ name: event.target.value })} />
      <p className="text-sm text-ink-muted">{t.workflows.fromTo(from, to)}</p>
      <Field
        label={t.workflows.description}
        id="field-transition-description"
        value={edge.description}
        onChange={(event) => onChange({ description: event.target.value })}
      />
      <RuleEditor
        rules={edge.rules}
        catalogue={catalogue}
        linkTypes={(linkTypes ?? []).map((each) => ({ id: each.id, name: each.name }))}
        onChange={(rules) => onChange({ rules })}
      />
      <Button size="sm" variant="ghost" onClick={onRemove} data-action="remove-transition">
        {t.workflows.removeTransition}
      </Button>
    </div>
  );
}

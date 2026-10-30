import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useProjects } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { useReuseRequirement } from "@/api/reuse";
import { useCopyRequirement, useMoveRequirement, useTree } from "@/api/tree";
import { useStatuses } from "@/api/workflows";
import { Button, Checkbox, Dialog, ErrorBanner, Select } from "@/components/ui";
import { t } from "@/i18n";
import { subtreeOf } from "./outline";

// Depth is drawn with spaces a select keeps, since an option holds only text.
const INDENT = "   ";
/** The choice of the top of a project, which no requirement's id can be. */
const TOP = "";

const titles = { move: t.tree.moveTitle, copy: t.tree.copyTitle, reuse: t.reuse.title };
const submits = { move: () => t.tree.moveSubmit, copy: () => t.tree.copySubmit, reuse: () => t.reuse.submit };

function mappingRefused(error: unknown): boolean {
  return error instanceof ApiError && error.fields.statusMapping !== undefined;
}

/** What a requirement being placed is known by. */
export interface Placed {
  id: string;
  key: string;
  title: string;
  projectKey: string;
}

/**
 * Moves, copies or reuses a requirement by choosing a project and a parent from lists, which is how the keyboard
 * does what dragging in the tree does; it goes last below the parent chosen. A reuse goes to another project only.
 */
export function PlaceDialog({
  placed,
  mode,
  into,
  onClose,
  onDone,
}: {
  placed: Placed;
  mode: "move" | "copy" | "reuse";
  /** The project chosen first, when the reader came from one of them. */
  into?: string;
  onClose: () => void;
  onDone: (r: Requirement) => void;
}) {
  const moving = mode === "move";
  const reusing = mode === "reuse";
  const { data: projects } = useProjects();
  const targets = (projects ?? []).filter((p) =>
    reusing ? p.key !== placed.projectKey && p.can.edit && !p.archivedAt : p.key === placed.projectKey || (p.can.edit && !p.archivedAt),
  );
  const [chosen, setProjectKey] = useState(into ?? (reusing ? "" : placed.projectKey));
  const projectKey = chosen || (reusing ? (targets[0]?.key ?? "") : placed.projectKey);
  const { data: nodes, isLoading } = useTree(projectKey);
  const { data: home } = useTree(placed.projectKey);
  const across = moving && projectKey !== placed.projectKey;
  const { data: statuses } = useStatuses(across ? projectKey : "");
  const [chosenStatus, setChosenStatus] = useState<Record<string, string>>({});
  const [askAll, setAskAll] = useState(false);
  const [parentId, setParentId] = useState(TOP);
  const [children, setChildren] = useState(true);
  const move = useMoveRequirement();
  const copy = useCopyRequirement();
  const reuse = useReuseRequirement();
  const error = move.error ?? copy.error ?? reuse.error;
  // Below itself or what is below it would cut it off, so a move within its project does not offer them.
  const gone = new Set(moving && projectKey === placed.projectKey ? subtreeOf(nodes ?? [], placed.id).map((n) => n.id) : []);
  const places = (nodes ?? []).filter((n) => !gone.has(n.id));
  // The server keeps a status of the same name and asks for the rest; once it has refused, the workflow of a type
  // there lacks one the project has, so every status that moves is asked for.
  const named = (name: string) => (statuses ?? []).find((s) => s.name.toLowerCase() === name.toLowerCase());
  const moved = across ? subtreeOf(home ?? [], placed.id).flatMap((n) => (n.status ? [n.status] : [])) : [];
  const asked = moved.filter((s, i) => moved.findIndex((each) => each.id === s.id) === i && (askAll || !named(s.name)));
  const mapping = Object.fromEntries(asked.map((s) => [s.id, chosenStatus[s.id] ?? named(s.name)?.id ?? statuses?.[0]?.id ?? ""]));

  function submit(event: FormEvent) {
    event.preventDefault();
    const parent = parentId === TOP ? null : parentId;
    const siblings = places.filter((n) => (n.parentId ?? null) === parent && n.id !== placed.id);
    const place = { parentId: parent, afterId: siblings[siblings.length - 1]?.id ?? null };
    const target = projectKey === placed.projectKey ? undefined : projectKey;
    const statusMapping = asked.length > 0 ? mapping : undefined;
    if (moving)
      move.mutate(
        { key: placed.key, projectKey: target, place, statusMapping },
        { onSuccess: onDone, onError: (refusal) => setAskAll((was) => was || mappingRefused(refusal)) },
      );
    else if (reusing) reuse.mutate({ key: placed.key, projectKey, place, children }, { onSuccess: (made) => onDone(made.requirement) });
    else copy.mutate({ key: placed.key, projectKey: target, place, children, links: false }, { onSuccess: (made) => onDone(made.requirement) });
  }

  return (
    <Dialog open title={titles[mode](placed.key)} onClose={onClose} attrs={{ "data-place-dialog": mode }}>
      <form onSubmit={submit} className="space-y-3">
        {error && <ErrorBanner>{(error instanceof ApiError && error.fields.statusMapping) || error.message}</ErrorBanner>}
        {reusing && <p className="text-sm text-ink-muted">{targets.length > 0 ? t.reuse.intro : t.reuse.noTarget}</p>}
        <Select
          label={t.tree.project}
          value={projectKey}
          onChange={(event) => {
            setProjectKey(event.target.value);
            setParentId(TOP);
            setChosenStatus({});
            setAskAll(false);
          }}
        >
          {targets.map((p) => (
            <option key={p.key} value={p.key}>
              {p.name} ({p.key})
            </option>
          ))}
        </Select>
        <Select
          label={t.tree.parent}
          value={parentId}
          onChange={(event) => setParentId(event.target.value)}
          disabled={isLoading}
          hint={isLoading ? t.tree.loadingPlaces : t.tree.parentHint}
        >
          <option value={TOP}>{t.tree.top}</option>
          {places.map((n) => (
            <option key={n.id} value={n.id}>
              {INDENT.repeat(n.depth) + `${n.outline} ${n.title}`}
            </option>
          ))}
        </Select>
        {asked.length > 0 && (
          <fieldset className="space-y-3" data-status-mapping="">
            <legend className="text-sm text-ink">{t.tree.mapIntro}</legend>
            {asked.map((status) => (
              <Select
                key={status.id}
                label={t.tree.mapFrom(status.name)}
                value={mapping[status.id]}
                onChange={(event) => setChosenStatus((current) => ({ ...current, [status.id]: event.target.value }))}
                data-map-from={status.name}
              >
                {(statuses ?? []).map((each) => (
                  <option key={each.id} value={each.id}>
                    {each.name}
                  </option>
                ))}
              </Select>
            ))}
          </fieldset>
        )}
        {moving ? (
          <p className="text-sm text-ink-muted">{t.tree.moveHint}</p>
        ) : (
          <Checkbox label={reusing ? t.reuse.children : t.tree.copyChildren} checked={children} onChange={(event) => setChildren(event.target.checked)} />
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={move.isPending || copy.isPending || reuse.isPending} disabled={reusing && !projectKey} data-action={`confirm-${mode}`}>
            {submits[mode]()}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

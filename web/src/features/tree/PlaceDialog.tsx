import { useState, type FormEvent } from "react";
import { useProjects } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { useReuseRequirement } from "@/api/reuse";
import { useCopyRequirement, useMoveRequirement, useTree } from "@/api/tree";
import { Button, Checkbox, Dialog, ErrorBanner, Select } from "@/components/ui";
import { t } from "@/i18n";
import { subtreeOf } from "./outline";

// Depth is drawn with spaces a select keeps, since an option holds only text.
const INDENT = "   ";
/** The choice of the top of a project, which no requirement's id can be. */
const TOP = "";

const titles = { move: t.tree.moveTitle, copy: t.tree.copyTitle, reuse: t.reuse.title };
const submits = { move: () => t.tree.moveSubmit, copy: () => t.tree.copySubmit, reuse: () => t.reuse.submit };

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
  const [parentId, setParentId] = useState(TOP);
  const [children, setChildren] = useState(true);
  const move = useMoveRequirement();
  const copy = useCopyRequirement();
  const reuse = useReuseRequirement();
  const error = move.error ?? copy.error ?? reuse.error;
  // Below itself or what is below it would cut it off, so a move within its project does not offer them.
  const gone = new Set(moving && projectKey === placed.projectKey ? subtreeOf(nodes ?? [], placed.id).map((n) => n.id) : []);
  const places = (nodes ?? []).filter((n) => !gone.has(n.id));

  function submit(event: FormEvent) {
    event.preventDefault();
    const parent = parentId === TOP ? null : parentId;
    const siblings = places.filter((n) => (n.parentId ?? null) === parent && n.id !== placed.id);
    const place = { parentId: parent, afterId: siblings[siblings.length - 1]?.id ?? null };
    const target = projectKey === placed.projectKey ? undefined : projectKey;
    if (moving) move.mutate({ key: placed.key, projectKey: target, place }, { onSuccess: onDone });
    else if (reusing) reuse.mutate({ key: placed.key, projectKey, place, children }, { onSuccess: (made) => onDone(made.requirement) });
    else copy.mutate({ key: placed.key, projectKey: target, place, children, links: false }, { onSuccess: (made) => onDone(made.requirement) });
  }

  return (
    <Dialog title={titles[mode](placed.key)} onClose={onClose} data-place-dialog={mode}>
      <form onSubmit={submit} className="space-y-3">
        {error && <ErrorBanner>{error.message}</ErrorBanner>}
        {reusing && <p className="text-sm text-ink-muted">{targets.length > 0 ? t.reuse.intro : t.reuse.noTarget}</p>}
        <Select
          label={t.tree.project}
          value={projectKey}
          onChange={(event) => {
            setProjectKey(event.target.value);
            setParentId(TOP);
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
        {moving ? (
          <p className="text-sm text-ink-muted">{t.tree.moveHint}</p>
        ) : (
          <Checkbox label={reusing ? t.reuse.children : t.tree.copyChildren} checked={children} onChange={(event) => setChildren(event.target.checked)} />
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" loading={move.isPending || copy.isPending || reuse.isPending} disabled={reusing && !projectKey} data-action={`confirm-${mode}`}>
            {submits[mode]()}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

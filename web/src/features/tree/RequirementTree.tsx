import { useId, useRef, useState, type DragEvent, type KeyboardEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useMoveRequirement, type Place, type TreeNode } from "@/api/tree";
import { cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TREE_DROP_EDGE, TREE_INDENT_PX } from "@/config";
import { QualityBadge } from "@/features/quality/QualityBadge";
import { t } from "@/i18n";
import { afterMove, outlines } from "./outline";

/** Where a dragged requirement lands against the row under the pointer. */
export type DropZone = "before" | "inside" | "after";

/** The zone of a row a pointer at offsetY of height is over: its edges place beside, its middle inside. */
export function dropZone(offsetY: number, height: number): DropZone {
  if (offsetY < height * TREE_DROP_EDGE) return "before";
  if (offsetY > height * (1 - TREE_DROP_EDGE)) return "after";
  return "inside";
}

/** The place a drop in a zone of a row asks for: beside it among its siblings, or last below it. */
export function dropPlace(nodes: TreeNode[], node: TreeNode, zone: DropZone): Place {
  const siblings = (parentId: string | null) => nodes.filter((n) => (n.parentId ?? null) === parentId);
  if (zone === "inside") {
    const children = siblings(node.id);
    return { parentId: node.id, afterId: children[children.length - 1]?.id ?? null };
  }
  if (zone === "after") return { parentId: node.parentId ?? null, afterId: node.id };
  const row = siblings(node.parentId ?? null);
  const index = row.findIndex((n) => n.id === node.id);
  return { parentId: node.parentId ?? null, afterId: index > 0 ? row[index - 1]!.id : null };
}

/** A project's requirements as a tree, numbered as they read: arrows walk it, Enter opens and M moves one, and
 * one dragged onto the edge of a row goes beside it, onto its middle below it. */
export function RequirementTree({
  project,
  nodes,
  currentId,
  onMove,
}: {
  project: Project;
  nodes: TreeNode[];
  currentId?: string;
  onMove: (node: TreeNode) => void;
}) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [focused, setFocused] = useState<string>();
  const [over, setOver] = useState<{ id: string; zone: DropZone }>();
  const [status, setStatus] = useState("");
  const dragging = useRef<TreeNode | null>(null);
  const hintId = useId();
  const navigate = useNavigate();
  const move = useMoveRequirement();
  const movable = project.can.edit && !project.archivedAt;

  // A node shows while nothing above it is folded.
  const hidden = new Set<string>();
  for (const node of nodes) {
    if (node.parentId && (collapsed.has(node.parentId) || hidden.has(node.parentId))) hidden.add(node.id);
  }
  const shown = nodes.filter((node) => !hidden.has(node.id));
  const roving = focused ?? currentId ?? shown[0]?.id;
  // Whether id sits anywhere below of, which a drop of of onto it would turn into a loop.
  const below = (id: string, of: string) => {
    let parent = nodes.find((x) => x.id === id)?.parentId;
    while (parent) {
      if (parent === of) return true;
      parent = nodes.find((x) => x.id === parent)?.parentId;
    }
    return false;
  };

  function toggle(id: string, open?: boolean) {
    setCollapsed((before) => {
      const next = new Set(before);
      if (open ?? next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function focus(el: Element | null | undefined) {
    if (!(el instanceof HTMLElement) || !el.dataset.treeItem) return;
    setFocused(el.dataset.treeItem);
    el.focus();
  }

  function drop(node: TreeNode, place: Place) {
    setStatus("");
    move.mutate(
      { key: node.key, place },
      { onSuccess: (moved) => setStatus(t.tree.moved(moved.key, moved.outline)), onError: (error) => setStatus(error.message) },
    );
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>, node: TreeNode) {
    if (event.target !== event.currentTarget || event.altKey || event.ctrlKey || event.metaKey) return;
    const items = Array.from(event.currentTarget.closest('[role="tree"]')?.querySelectorAll('[role="treeitem"]') ?? []);
    const index = items.indexOf(event.currentTarget);
    const open = node.children > 0 && !collapsed.has(node.id);
    const handled = () => {
      event.preventDefault();
      event.stopPropagation();
    };
    switch (event.key) {
      case "ArrowDown":
        handled();
        focus(items[index + 1]);
        break;
      case "ArrowUp":
        handled();
        focus(items[index - 1]);
        break;
      case "Home":
        handled();
        focus(items[0]);
        break;
      case "End":
        handled();
        focus(items[items.length - 1]);
        break;
      case "ArrowRight":
        handled();
        if (node.children > 0 && !open) toggle(node.id, true);
        else if (open) focus(items[index + 1]);
        break;
      case "ArrowLeft":
        handled();
        if (open) toggle(node.id, false);
        else focus(items.find((el) => (el as HTMLElement).dataset.treeItem === node.parentId));
        break;
      case "Enter":
        handled();
        void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey: project.key, reqKey: node.key } });
        break;
      case "m":
      case "M":
        if (!movable) break;
        handled();
        onMove(node);
        break;
    }
  }

  function onDragOver(event: DragEvent<HTMLDivElement>, node: TreeNode) {
    const from = dragging.current;
    // Onto itself or anything below it would cut it off from the top.
    if (!from || from.id === node.id || below(node.id, from.id)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    const rect = event.currentTarget.getBoundingClientRect();
    const zone = dropZone(event.clientY - rect.top, rect.height);
    if (over?.id !== node.id || over.zone !== zone) {
      setOver({ id: node.id, zone });
      const preview = outlines(afterMove(nodes, from.id, dropPlace(nodes, node, zone))).get(from.id);
      setStatus(preview ? t.tree.preview(from.key, preview) : "");
    }
  }

  function onDrop(event: DragEvent<HTMLDivElement>, node: TreeNode) {
    event.preventDefault();
    const from = dragging.current;
    setOver(undefined);
    if (!from || from.id === node.id || below(node.id, from.id)) return;
    const rect = event.currentTarget.getBoundingClientRect();
    drop(from, dropPlace(nodes, node, dropZone(event.clientY - rect.top, rect.height)));
  }

  if (nodes.length === 0) return <p className="px-2 py-1 text-sm text-ink-subtle">{t.tree.empty}</p>;
  return (
    <div>
      <div role="tree" aria-label={t.tree.label(project.name)} aria-describedby={hintId} data-requirement-tree={project.key}>
        {shown.map((node) => {
          const open = node.children > 0 && !collapsed.has(node.id);
          const zone = over?.id === node.id ? over.zone : undefined;
          return (
            <div
              key={node.id}
              role="treeitem"
              aria-label={`${node.outline} ${node.title}`}
              aria-level={node.depth + 1}
              aria-expanded={node.children > 0 ? open : undefined}
              aria-selected={node.id === currentId}
              aria-keyshortcuts={movable ? "M" : undefined}
              tabIndex={roving === node.id ? 0 : -1}
              draggable={movable}
              onDragStart={(event) => {
                event.dataTransfer.setData("text/plain", node.key);
                event.dataTransfer.effectAllowed = "move";
                dragging.current = node;
              }}
              onDragEnd={() => {
                dragging.current = null;
                setOver(undefined);
              }}
              onKeyDown={(event) => onKeyDown(event, node)}
              onFocus={(event) => event.target === event.currentTarget && setFocused(node.id)}
              data-tree-item={node.id}
              data-tree-key={node.key}
              className="outline-none [&:focus-visible>[data-tree-row]]:ring-2 [&:focus-visible>[data-tree-row]]:ring-focus"
            >
              {/* biome-ignore lint/a11y/noStaticElementInteractions: a dragged requirement lands on the row; the keyboard moves with M */}
              <div
                className={cx(
                  "relative flex h-8 items-center gap-1 rounded-control pr-2 text-sm text-ink-muted hover:bg-surface-raised hover:text-ink",
                  node.id === currentId && "bg-accent-subtle font-medium text-accent",
                  zone === "inside" && "ring-2 ring-accent",
                  zone === "before" && "before:absolute before:inset-x-0 before:top-0 before:h-0.5 before:bg-accent",
                  zone === "after" && "after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-accent",
                )}
                style={{ paddingLeft: node.depth * TREE_INDENT_PX }}
                onDragOver={(event) => onDragOver(event, node)}
                onDragLeave={() => over?.id === node.id && setOver(undefined)}
                onDrop={(event) => onDrop(event, node)}
                data-tree-row
                data-drop={zone}
              >
                {/* For the pointer only: the keyboard folds and unfolds with the arrows. */}
                <span
                  aria-hidden="true"
                  className="flex size-5 shrink-0 cursor-pointer items-center justify-center"
                  onClick={() => node.children > 0 && toggle(node.id)}
                >
                  {node.children > 0 && <Icon.ChevronDown className={cx("transition-transform", !open && "-rotate-90")} />}
                </span>
                <span className="shrink-0 font-mono text-xs text-ink-subtle" data-tree-outline>
                  {node.outline}
                </span>
                <Link
                  to="/p/$projectKey/r/$reqKey"
                  params={{ projectKey: project.key, reqKey: node.key }}
                  tabIndex={-1}
                  className={cx("min-w-0 flex-1 truncate", node.kind === "section" && "font-semibold text-ink")}
                >
                  {node.title}
                </Link>
                {node.kind === "requirement" && node.quality !== null && <QualityBadge score={node.quality} className="shrink-0" />}
              </div>
            </div>
          );
        })}
      </div>
      <p id={hintId} className="sr-only">
        {t.tree.keys}
      </p>
      <p role="status" className="px-2 text-xs text-ink-muted [&:not(:empty)]:py-1" data-tree-status>
        {status}
      </p>
    </div>
  );
}

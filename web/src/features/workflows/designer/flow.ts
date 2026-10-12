import { Position, type Edge, type Node, type NodeHandle } from "@xyflow/react";
import type { HTMLAttributes, SVGAttributes } from "react";
import type { Status } from "@/api/workflows";
import { WORKFLOW_EDGE_BEND, WORKFLOW_HANDLE_SIZE, WORKFLOW_NODE_HEIGHT, WORKFLOW_NODE_WIDTH } from "@/config";
import { t } from "@/i18n";
import type { EdgeShape } from "@/lib/graph";
import { ANYWHERE, anywhereRect, bendClearOf, bends, edgeShape, rectOf, type Design } from "./graph";

// What React Flow is handed: the design translated into its nodes and edges, with the geometry still ours so
// the picture and what is saved agree.

export type Selection = { kind: "none" } | { kind: "node"; statusId: string } | { kind: "edge"; key: string };

/** React Flow refuses an empty id, so the "any status" box needs a name of its own. */
export const ANYWHERE_NODE_ID = "anywhere";

export function toNodeId(statusId: string): string {
  return statusId === ANYWHERE ? ANYWHERE_NODE_ID : statusId;
}

export function fromNodeId(nodeId: string): string {
  return nodeId === ANYWHERE_NODE_ID ? ANYWHERE : nodeId;
}

export type StatusNodeData = { status: Status; initial: boolean; readOnly: boolean };
export type AnywhereNodeData = { readOnly: boolean };
export type TransitionEdgeData = { name: string; shape: EdgeShape; readOnly: boolean };

export type StatusNode = Node<StatusNodeData, "status">;
export type AnywhereNode = Node<AnywhereNodeData, "anywhere">;
export type FlowNode = StatusNode | AnywhereNode;
export type TransitionEdge = Edge<TransitionEdgeData, "transition">;

/** Where a box's handles are before anything is measured, so an edge draws on the first paint and under a test
 * runner that measures nothing; the id is null, not absent, since React Flow finds the handle by that word. */
function handlesOf(cover: boolean): NodeHandle[] {
  const ring: NodeHandle = {
    id: null,
    type: "source",
    position: Position.Right,
    x: WORKFLOW_NODE_WIDTH - WORKFLOW_HANDLE_SIZE / 2,
    y: (WORKFLOW_NODE_HEIGHT - WORKFLOW_HANDLE_SIZE) / 2,
    width: WORKFLOW_HANDLE_SIZE,
    height: WORKFLOW_HANDLE_SIZE,
  };
  if (!cover) return [ring];
  return [{ id: null, type: "target", position: Position.Left, x: 0, y: 0, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT }, ring];
}

/** Data attributes go on the wrapper element, where a test or a suite measures the box. */
function dom(attributes: Record<string, string | number | boolean | undefined>): HTMLAttributes<HTMLDivElement> {
  return attributes as HTMLAttributes<HTMLDivElement>;
}

/** The same attributes for an edge, whose element is an SVG group. */
function svgDom(attributes: Record<string, string | number | boolean | undefined>): SVGAttributes<SVGGElement> {
  return attributes as SVGAttributes<SVGGElement>;
}

/** The nodes and edges for one drawing; read-only, nothing can be grabbed, focused or selected, and the "any
 * status" box only earns its place when a transition leaves it. */
export function toFlow(design: Design, statuses: Map<string, Status>, selection: Selection, readOnly: boolean): { nodes: FlowNode[]; edges: TransitionEdge[] } {
  const nodes: FlowNode[] = [];
  for (const node of design.nodes) {
    const status = statuses.get(node.statusId);
    if (!status) continue;
    const selected = selection.kind === "node" && selection.statusId === node.statusId;
    const initial = design.initialId === node.statusId;
    nodes.push({
      id: toNodeId(node.statusId),
      type: "status",
      position: { x: node.x, y: node.y },
      initialWidth: WORKFLOW_NODE_WIDTH,
      initialHeight: WORKFLOW_NODE_HEIGHT,
      draggable: !readOnly,
      selectable: !readOnly,
      focusable: !readOnly,
      connectable: !readOnly,
      handles: handlesOf(true),
      selected,
      ariaRole: readOnly ? undefined : "button",
      ariaLabel: initial ? t.workflows.startsHereLabel(status.name) : status.name,
      domAttributes: dom({ "data-workflow-node": status.name, "data-x": node.x, "data-y": node.y, "aria-pressed": readOnly ? undefined : selected }),
      data: { status, initial, readOnly },
    });
  }
  if (!readOnly || design.edges.some((edge) => edge.from === ANYWHERE)) {
    const rect = anywhereRect();
    nodes.push({
      id: ANYWHERE_NODE_ID,
      type: "anywhere",
      position: { x: rect.x, y: rect.y },
      initialWidth: WORKFLOW_NODE_WIDTH,
      initialHeight: WORKFLOW_NODE_HEIGHT,
      draggable: false,
      selectable: false,
      focusable: false,
      connectable: !readOnly,
      handles: handlesOf(false),
      ariaLabel: t.workflows.anyStatus,
      domAttributes: dom({ "data-workflow-anywhere": "" }),
      data: { readOnly },
    });
  }

  const bent = bends(design.edges);
  const drawn = nodes.map((node) => ({
    id: fromNodeId(node.id),
    rect: { x: node.position.x, y: node.position.y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT },
  }));
  const edges: TransitionEdge[] = [];
  for (const edge of design.edges) {
    const from = rectOf(design, edge.from);
    const to = rectOf(design, edge.to);
    if (!from || !to) continue;
    // The other cards are in the way; the arrow bows round them.
    const obstacles = drawn.filter((each) => each.id !== edge.from && each.id !== edge.to).map((each) => each.rect);
    const bend = bendClearOf(from, to, obstacles, bent.get(edge.key) ?? 0, WORKFLOW_EDGE_BEND);
    const selected = selection.kind === "edge" && selection.key === edge.key;
    edges.push({
      id: edge.key,
      type: "transition",
      source: toNodeId(edge.from),
      target: toNodeId(edge.to),
      selectable: !readOnly,
      focusable: !readOnly,
      selected,
      ariaRole: readOnly ? undefined : "button",
      ariaLabel: t.workflows.transitionLabel(edge.name || t.workflows.unnamed),
      domAttributes: svgDom({ "data-workflow-edge": edge.name, "aria-pressed": readOnly ? undefined : selected }),
      data: { name: edge.name, shape: edgeShape(from, to, bend), readOnly },
    });
  }
  return { nodes, edges };
}

import { STATUS_CATEGORIES, type RuleKind, type RuleType, type Status, type StatusCategory, type Workflow, type WorkflowInput } from "@/api/workflows";
import {
  WORKFLOW_ANYWHERE_X,
  WORKFLOW_ANYWHERE_Y,
  WORKFLOW_CANVAS_MARGIN,
  WORKFLOW_CANVAS_MIN_HEIGHT,
  WORKFLOW_CANVAS_MIN_WIDTH,
  WORKFLOW_COLUMN_GAP,
  WORKFLOW_EDGE_BEND,
  WORKFLOW_GRID,
  WORKFLOW_LAYOUT_ORIGIN_X,
  WORKFLOW_LAYOUT_ORIGIN_Y,
  WORKFLOW_NODE_HEIGHT,
  WORKFLOW_NODE_WIDTH,
  WORKFLOW_ROW_GAP,
} from "@/config";
import { t } from "@/i18n";
import { along, bends as bendsBy, edgeShape, type Connection, type Point, type Rect } from "@/lib/graph";

export { clipToRect, edgeShape } from "@/lib/graph";
export type { EdgeShape, Point, Rect } from "@/lib/graph";

// The workflow as the designer holds it: statuses placed on a canvas and the transitions drawn between them,
// plain data and pure functions so what a valid picture is can be tested without a browser.

export interface Design {
  name: string;
  description: string;
  nodes: Node[];
  /** The status new requirements start in; empty until one is chosen. */
  initialId: string;
  edges: Edge[];
}

export interface Node {
  statusId: string;
  x: number;
  y: number;
  /** Where a new revision sends a requirement in this status, empty when it stays. */
  onRevisionId: string;
}

/** The source of a transition available from every status. */
export const ANYWHERE = "";

export interface Edge {
  /** A handle that survives renames and saves; the server id once it has one. */
  key: string;
  id?: string;
  name: string;
  description: string;
  /** ANYWHERE, or the status the transition leaves. */
  from: string;
  to: string;
  rules: DraftRule[];
}

export interface DraftRule {
  kind: RuleKind;
  type: string;
  config: Record<string, unknown>;
}

export function emptyDesign(): Design {
  return { name: "", description: "", nodes: [], initialId: "", edges: [] };
}

/** Reads a saved workflow onto the canvas, laying out whatever was never placed. */
export function fromWorkflow(workflow: Workflow): Design {
  let design: Design = {
    name: workflow.name,
    description: workflow.description,
    nodes: workflow.steps
      .filter((step) => step.layout)
      .map((step) => ({ statusId: step.status.id, x: step.layout!.x, y: step.layout!.y, onRevisionId: step.onRevisionStatusId ?? "" })),
    initialId: workflow.steps.find((step) => step.isInitial)?.status.id ?? "",
    edges: workflow.transitions.map((transition) => ({
      key: transition.id,
      id: transition.id,
      name: transition.name,
      description: transition.description,
      from: transition.fromStatusId ?? ANYWHERE,
      to: transition.toStatusId,
      rules: transition.rules.map((rule) => ({ kind: rule.kind, type: rule.type, config: (rule.config as Record<string, unknown> | null) ?? {} })),
    })),
  };
  for (const step of workflow.steps) {
    if (!step.layout) {
      const node = { statusId: step.status.id, onRevisionId: step.onRevisionStatusId ?? "", ...slotFor(design.nodes, step.status.category) };
      design = { ...design, nodes: [...design.nodes, node] };
    }
  }
  return design;
}

export function toInput(design: Design): WorkflowInput {
  return {
    name: design.name.trim(),
    description: design.description.trim(),
    steps: design.nodes.map((node) => ({
      statusId: node.statusId,
      isInitial: node.statusId === design.initialId,
      onRevisionStatusId: node.onRevisionId || undefined,
      layout: { x: node.x, y: node.y },
    })),
    transitions: design.edges.map((edge) => ({
      id: edge.id,
      name: edge.name.trim(),
      description: edge.description.trim(),
      fromStatusId: edge.from === ANYWHERE ? undefined : edge.from,
      toStatusId: edge.to,
      rules: edge.rules.map((rule) => ({ kind: rule.kind, type: rule.type, config: rule.config })),
    })),
  };
}

/** The first free place in a category's column, the columns following the lifecycle left to right; the
 * any-status box has a place nothing is laid out over. */
export function slotFor(nodes: Node[], category: StatusCategory): Point {
  const x = WORKFLOW_LAYOUT_ORIGIN_X + STATUS_CATEGORIES.indexOf(category) * WORKFLOW_COLUMN_GAP;
  const occupied = [anywhereRect(), ...nodes];
  for (let row = 0; ; row++) {
    const y = WORKFLOW_LAYOUT_ORIGIN_Y + row * WORKFLOW_ROW_GAP;
    const taken = occupied.some((each) => Math.abs(each.x - x) < WORKFLOW_NODE_WIDTH && Math.abs(each.y - y) < WORKFLOW_NODE_HEIGHT);
    if (!taken) return { x, y };
  }
}

/** Puts a status on the canvas; the first one placed is where requirements start. */
export function addStatus(design: Design, status: Status): Design {
  if (design.nodes.some((node) => node.statusId === status.id)) return design;
  return {
    ...design,
    nodes: [...design.nodes, { statusId: status.id, onRevisionId: "", ...slotFor(design.nodes, status.category) }],
    initialId: design.initialId || status.id,
  };
}

/** Takes a status off the canvas with every transition touching it, which the server would refuse anyway. */
export function removeStatus(design: Design, statusId: string): Design {
  return {
    ...design,
    nodes: design.nodes.filter((node) => node.statusId !== statusId).map((node) => (node.onRevisionId === statusId ? { ...node, onRevisionId: "" } : node)),
    initialId: design.initialId === statusId ? "" : design.initialId,
    edges: design.edges.filter((edge) => edge.from !== statusId && edge.to !== statusId),
  };
}

export function setInitial(design: Design, statusId: string): Design {
  return { ...design, initialId: statusId };
}

export function setOnRevision(design: Design, statusId: string, onRevisionId: string): Design {
  return { ...design, nodes: design.nodes.map((node) => (node.statusId === statusId ? { ...node, onRevisionId } : node)) };
}

/** Drops a status where it was dragged, snapped to the grid and kept on the canvas. */
export function moveNode(design: Design, statusId: string, x: number, y: number): Design {
  const snap = (value: number) => Math.max(0, Math.round(value / WORKFLOW_GRID) * WORKFLOW_GRID);
  return { ...design, nodes: design.nodes.map((node) => (node.statusId === statusId ? { ...node, x: snap(x), y: snap(y) } : node)) };
}

let nextKey = 0;

/** Draws a transition; a status cannot move to itself, so that hands back nothing. */
export function connect(design: Design, from: string, to: string, name: string): { design: Design; key: string } | null {
  if (from === to || to === ANYWHERE) return null;
  const known = (id: string) => id === ANYWHERE || design.nodes.some((node) => node.statusId === id);
  if (!known(from) || !known(to)) return null;
  nextKey += 1;
  const key = `new-${nextKey}`;
  const edge: Edge = { key, name, description: "", from, to, rules: [] };
  return { design: { ...design, edges: [...design.edges, edge] }, key };
}

export function updateEdge(design: Design, key: string, patch: Partial<Omit<Edge, "key" | "id">>): Design {
  return { ...design, edges: design.edges.map((edge) => (edge.key === key ? { ...edge, ...patch } : edge)) };
}

export function removeEdge(design: Design, key: string): Design {
  return { ...design, edges: design.edges.filter((edge) => edge.key !== key) };
}

export function nodeRect(node: Point): Rect {
  return { x: node.x, y: node.y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT };
}

export function anywhereRect(): Rect {
  return { x: WORKFLOW_ANYWHERE_X, y: WORKFLOW_ANYWHERE_Y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT };
}

/** The rect of an edge's end, or nothing if it points at a status not drawn. */
export function rectOf(design: Design, id: string): Rect | undefined {
  if (id === ANYWHERE) return anywhereRect();
  const node = design.nodes.find((each) => each.statusId === id);
  return node ? nodeRect(node) : undefined;
}

/** How far each transition bows out, so two running opposite ways stay apart. */
export function bends(edges: Edge[]): Map<string, number> {
  return bendsBy(edges as Connection[], WORKFLOW_EDGE_BEND);
}

/** How many points along a curve are checked against the cards it must miss, and how close to a card's edge
 * it may pass without counting as through it. */
const CURVE_SAMPLES = 9;
const CLEARANCE = 6;

/** The bend an arrow takes so it misses every card that is neither end: the pair's bend first, then further
 * out to either side; an arrow that cannot be cleared keeps the pair's bend. */
export function bendClearOf(from: Rect, to: Rect, obstacles: Rect[], base: number, step: number): number {
  const candidates = [base, base + step, base - step, base + 2 * step, base - 2 * step];
  for (const bend of candidates) {
    if (!crossesAny(edgeShape(from, to, bend).path, obstacles)) return bend;
  }
  return base;
}

/** Whether a quadratic path, sampled along its length, enters any of the rects. */
function crossesAny(path: string, obstacles: Rect[]): boolean {
  const numbers = path.match(/-?\d+(\.\d+)?/g)?.map(Number) ?? [];
  const [x0 = 0, y0 = 0, cx = 0, cy = 0, x1 = 0, y1 = 0] = numbers;
  for (let i = 1; i < CURVE_SAMPLES; i++) {
    const x = along(x0, cx, x1, i / CURVE_SAMPLES);
    const y = along(y0, cy, y1, i / CURVE_SAMPLES);
    const inside = (r: Rect) => x > r.x + CLEARANCE && x < r.x + r.width - CLEARANCE && y > r.y + CLEARANCE && y < r.y + r.height - CLEARANCE;
    if (obstacles.some(inside)) return true;
  }
  return false;
}

/** How big the canvas has to be to show everything, with room to drag into. */
export function canvasSize(design: Design): { width: number; height: number } {
  const rects = [anywhereRect(), ...design.nodes.map(nodeRect)];
  const right = Math.max(...rects.map((rect) => rect.x + rect.width));
  const bottom = Math.max(...rects.map((rect) => rect.y + rect.height));
  return {
    width: Math.max(WORKFLOW_CANVAS_MIN_WIDTH, right + WORKFLOW_CANVAS_MARGIN),
    height: Math.max(WORKFLOW_CANVAS_MIN_HEIGHT, bottom + WORKFLOW_CANVAS_MARGIN),
  };
}

/** What would stop this design from saving, in the words the server would use, said before the round trip. */
export function problems(design: Design, statuses: Map<string, Status>): string[] {
  const words = t.workflows.problems;
  const out: string[] = [];
  if (design.name.trim() === "") out.push(words.name);
  if (design.nodes.length === 0) out.push(words.noStatus);
  else if (!design.initialId) out.push(words.noInitial);
  if (design.edges.some((edge) => edge.name.trim() === "")) out.push(words.unnamed);
  const seen = new Set<string>();
  for (const edge of design.edges) {
    const key = `${edge.from}|${edge.name.trim().toLowerCase()}`;
    if (edge.name.trim() && seen.has(key)) {
      out.push(words.twice(edge.name.trim(), edge.from === ANYWHERE ? t.workflows.anyStatusLower : (statuses.get(edge.from)?.name ?? "")));
    }
    seen.add(key);
  }
  return out;
}

/** A rule type's label in the reader's language, the server's own where the catalogue has no word for it. */
export function ruleLabel(type: string, catalogue: RuleType[]): string {
  const words = t.workflows.ruleLabels as Record<string, string | undefined>;
  return words[type] ?? catalogue.find((each) => each.type === type)?.label ?? type;
}

/** A rule as one line for the inspector, with its configuration spelled out. */
export function describeRule(rule: DraftRule, catalogue: RuleType[], linkTypes: { id: string; name: string }[] = []): string {
  const type = catalogue.find((each) => each.type === rule.type);
  if (!type) return rule.type;
  const parts = type.options
    .map((option) => {
      const value = rule.config[option.name];
      const label = optionLabel(option.name, option.label);
      if (Array.isArray(value)) return value.length ? `${label}: ${value.map((each) => roleWord(String(each))).join(", ")}` : "";
      if (!value) return "";
      const named = option.kind === "linkType" ? linkTypes.find((each) => each.id === value)?.name : undefined;
      const word = named ?? (t.workflows.optionChoices as Record<string, string | undefined>)[String(value)] ?? String(value);
      return `${label}: ${word}`;
    })
    .filter(Boolean);
  const label = ruleLabel(rule.type, catalogue);
  return parts.length ? `${label} (${parts.join("; ")})` : label;
}

/** A project role's name in the reader's language. */
export function roleWord(role: string): string {
  return (t.permissions.roles as Record<string, string | undefined>)[role] ?? role;
}

/** A rule option's label in the reader's language, the server's own where the catalogue has no word for it. */
export function optionLabel(name: string, fallback: string): string {
  return (t.workflows.optionLabels as Record<string, string | undefined>)[name] ?? fallback;
}

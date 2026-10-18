import type { TraceGraph, TraceGraphEdge, TraceGraphNode } from "@/api/trace";
import { GRAPH_BEND_STEP, GRAPH_COLUMN_GAP, GRAPH_MARGIN, GRAPH_NODE_HEIGHT, GRAPH_NODE_WIDTH, GRAPH_ROW_GAP } from "@/config";
import { bends, edgeShape, type EdgeShape, type Rect } from "@/lib/graph";

// Where a trace graph's nodes and links are drawn: a row per level, up above the centre and down below it, each row
// centred under the widest. Plain data, so the picture is tested without a browser.

export interface GraphLayout {
  places: Map<string, Rect>;
  shapes: Map<string, EdgeShape>;
  width: number;
  height: number;
}

/** A key of its own for each link: its id, or for an issue, page, test or tag its two ends and relation. */
export function edgeKey(edge: TraceGraphEdge): string {
  return edge.linkId ?? `${edge.from}>${edge.to}>${edge.relation ?? ""}`;
}

/** The nodes by level, levels from the top down. */
export function rows(graph: TraceGraph): [number, TraceGraphNode[]][] {
  const byLevel = new Map<number, TraceGraphNode[]>();
  for (const node of graph.nodes) byLevel.set(node.level, [...(byLevel.get(node.level) ?? []), node]);
  return [...byLevel.entries()].sort(([a], [b]) => a - b);
}

export function layoutGraph(graph: TraceGraph): GraphLayout {
  const levels = rows(graph);
  const span = (count: number) => count * GRAPH_NODE_WIDTH + Math.max(0, count - 1) * GRAPH_COLUMN_GAP;
  const widest = Math.max(1, ...levels.map(([, nodes]) => nodes.length));
  const places = new Map<string, Rect>();
  levels.forEach(([, nodes], row) => {
    const start = GRAPH_MARGIN + (span(widest) - span(nodes.length)) / 2;
    nodes.forEach((node, i) => {
      places.set(node.id, {
        x: start + i * (GRAPH_NODE_WIDTH + GRAPH_COLUMN_GAP),
        y: GRAPH_MARGIN + row * (GRAPH_NODE_HEIGHT + GRAPH_ROW_GAP),
        width: GRAPH_NODE_WIDTH,
        height: GRAPH_NODE_HEIGHT,
      });
    });
  });
  const drawn = graph.edges.filter((edge) => places.has(edge.from) && places.has(edge.to));
  const bent = bends(
    drawn.map((edge) => ({ key: edgeKey(edge), from: edge.from, to: edge.to })),
    GRAPH_BEND_STEP,
  );
  const shapes = new Map<string, EdgeShape>();
  for (const edge of drawn) shapes.set(edgeKey(edge), edgeShape(places.get(edge.from)!, places.get(edge.to)!, bent.get(edgeKey(edge)) ?? 0));
  return {
    places,
    shapes,
    width: 2 * GRAPH_MARGIN + span(widest),
    height: 2 * GRAPH_MARGIN + levels.length * GRAPH_NODE_HEIGHT + Math.max(0, levels.length - 1) * GRAPH_ROW_GAP,
  };
}

/** The node each node was reached from, one level nearer the centre, so the graph reads as a tree; the centre has none. */
export function reachedFrom(graph: TraceGraph): Map<string, { from: TraceGraphNode; edge: TraceGraphEdge }> {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const out = new Map<string, { from: TraceGraphNode; edge: TraceGraphEdge }>();
  for (const node of graph.nodes) {
    if (node.level === 0) continue;
    for (const edge of graph.edges) {
      const other = edge.from === node.id ? edge.to : edge.to === node.id ? edge.from : undefined;
      const from = other === undefined ? undefined : byId.get(other);
      if (from && Math.abs(from.level) === Math.abs(node.level) - 1 && (from.level === 0 || Math.sign(from.level) === Math.sign(node.level))) {
        out.set(node.id, { from, edge });
        break;
      }
    }
  }
  return out;
}

/** What a node is called: a requirement's or issue's key, where a page, test or tag is. */
export function nodeName(node: TraceGraphNode): string {
  if (node.requirement) return node.requirement.key;
  if (node.issueKey) return node.issueKey;
  if (node.page) return `${node.page.spaceKey}/${node.page.title || node.page.id}`;
  if (node.test) return node.test.name;
  if (node.code) return `${node.code.path}:${node.code.line}`;
  return node.id;
}

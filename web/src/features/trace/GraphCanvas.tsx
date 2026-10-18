import { useCallback, useMemo } from "react";
import { BaseEdge, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type EdgeProps, type Node, type NodeChange, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import type { TraceGraph, TraceGraphEdge, TraceGraphNode } from "@/api/trace";
import { Icon } from "@/components/icons";
import { cx, IconButton } from "@/components/ui";
import { GRAPH_CANVAS_HEIGHT, GRAPH_MAX_ZOOM, GRAPH_MIN_ZOOM } from "@/config";
import { t } from "@/i18n";
import { edgeKey, layoutGraph } from "./layout";
import { NodeCard } from "./NodeCard";
import type { EdgeShape } from "@/lib/graph";

type GraphNodeData = { node: TraceGraphNode; title?: string; chosen: boolean; label: string };
type GraphFlowNode = Node<GraphNodeData, "trace">;
type GraphEdgeData = { edge: TraceGraphEdge; shape: EdgeShape };
type GraphFlowEdge = Edge<GraphEdgeData, "trace">;

const nodeTypes = { trace: TraceNode };
const edgeTypes = { trace: TraceEdge };

/** The picture of a trace graph: our own rows and curves, which React Flow pans, zooms and walks by keyboard. */
export function GraphCanvas(props: {
  graph: TraceGraph;
  reqKey: string;
  titles: Map<string, string>;
  labelOf: (node: TraceGraphNode) => string;
  chosen?: string;
  onChoose: (id: string) => void;
  onCentre: (node: TraceGraphNode) => void;
}) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}

function Canvas({
  graph,
  reqKey,
  titles,
  labelOf,
  chosen,
  onChoose,
  onCentre,
}: {
  graph: TraceGraph;
  reqKey: string;
  titles: Map<string, string>;
  labelOf: (node: TraceGraphNode) => string;
  chosen?: string;
  onChoose: (id: string) => void;
  onCentre: (node: TraceGraphNode) => void;
}) {
  const layout = useMemo(() => layoutGraph(graph), [graph]);
  const flow = useReactFlow();
  const nodes = useMemo<GraphFlowNode[]>(
    () =>
      graph.nodes.map((node) => {
        const place = layout.places.get(node.id)!;
        return {
          id: node.id,
          type: "trace",
          position: { x: place.x, y: place.y },
          width: place.width,
          height: place.height,
          draggable: false,
          connectable: false,
          selected: node.id === chosen,
          ariaLabel: labelOf(node),
          data: { node, title: titles.get(node.id), chosen: node.id === chosen, label: labelOf(node) },
        };
      }),
    [graph, layout, chosen, titles, labelOf],
  );
  const edges = useMemo<GraphFlowEdge[]>(
    () =>
      graph.edges
        .filter((edge) => layout.shapes.has(edgeKey(edge)))
        .map((edge) => ({
          id: edgeKey(edge),
          type: "trace",
          source: edge.from,
          target: edge.to,
          focusable: false,
          selectable: false,
          data: { edge, shape: layout.shapes.get(edgeKey(edge))! },
        })),
    [graph, layout],
  );
  const onNodesChange = useCallback(
    (changes: NodeChange<GraphFlowNode>[]) => {
      // Keyboard selection never reaches onNodeClick, so the choice is read here.
      for (const change of changes) if (change.type === "select" && change.selected) onChoose(change.id);
    },
    [onChoose],
  );
  return (
    <div className="space-y-2">
      <div className="flex justify-end gap-1" data-graph-zoom>
        <IconButton size="sm" icon={<Icon.Plus />} label={t.graph.zoomIn} onClick={() => void flow.zoomIn()} />
        <IconButton size="sm" icon={<Icon.Collapse />} label={t.graph.zoomOut} onClick={() => void flow.zoomOut()} />
        <IconButton size="sm" icon={<Icon.Expand />} label={t.graph.fit} onClick={() => void flow.fitView()} />
      </div>
      <div
        role="group"
        aria-label={t.graph.canvas(reqKey)}
        data-graph-canvas
        className="relative overflow-hidden rounded-control border border-border bg-surface"
        style={{ height: GRAPH_CANVAS_HEIGHT }}
      >
        <svg width="0" height="0" className="absolute" aria-hidden>
          <defs>
            <marker id="graph-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M 0 0 L 10 5 L 0 10 z" className="fill-ink-muted" />
            </marker>
            <marker id="graph-arrow-suspect" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M 0 0 L 10 5 L 0 10 z" className="fill-warning" />
            </marker>
          </defs>
        </svg>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onNodeClick={(_, node) => onChoose(node.id)}
          onNodeDoubleClick={(_, node) => node.data.node.requirement && onCentre(node.data.node)}
          nodesDraggable={false}
          nodesConnectable={false}
          nodesFocusable
          edgesFocusable={false}
          elementsSelectable
          fitView
          minZoom={GRAPH_MIN_ZOOM}
          maxZoom={GRAPH_MAX_ZOOM}
          zoomOnScroll={false}
          zoomOnDoubleClick={false}
          preventScrolling={false}
          deleteKeyCode={null}
          selectionKeyCode={null}
          multiSelectionKeyCode={null}
          proOptions={{ hideAttribution: true }}
          style={{ width: "100%", height: "100%", background: "transparent" }}
        />
      </div>
    </div>
  );
}

function TraceNode({ data }: NodeProps<GraphFlowNode>) {
  return (
    <div className={cx("h-full rounded-xl", data.chosen && "ring-2 ring-accent")} data-graph-node={data.label}>
      <NodeCard node={data.node} title={data.title} compact />
    </div>
  );
}

/** Our curve between two nodes, drawn in the warning colour when the link is suspect, dashed when broken. */
function TraceEdge({ data }: EdgeProps<GraphFlowEdge>) {
  if (!data) return null;
  const { edge, shape } = data;
  return (
    <BaseEdge
      path={shape.path}
      markerEnd={edge.suspect ? "url(#graph-arrow-suspect)" : "url(#graph-arrow)"}
      style={{
        stroke: edge.suspect ? "var(--color-warning)" : "var(--color-ink-muted)",
        strokeWidth: edge.suspect ? 2 : 1.5,
        strokeDasharray: edge.broken ? "6 4" : undefined,
      }}
    />
  );
}

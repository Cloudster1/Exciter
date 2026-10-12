import { useCallback, useMemo } from "react";
import {
  BaseEdge,
  ConnectionMode,
  EdgeLabelRenderer,
  Handle,
  Position,
  ReactFlow,
  useStoreApi,
  type Connection,
  type EdgeChange,
  type EdgeProps,
  type IsValidConnection,
  type NodeChange,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import type { Status } from "@/api/workflows";
import { Icon } from "@/components/icons";
import { cx } from "@/components/ui";
import {
  WORKFLOW_BADGE_SIZE,
  WORKFLOW_CONNECT_RADIUS,
  WORKFLOW_DRAG_THRESHOLD_PX,
  WORKFLOW_EDGE_HIT_WIDTH,
  WORKFLOW_GRID,
  WORKFLOW_HANDLE_SIZE,
  WORKFLOW_NAME_LENGTH,
  WORKFLOW_NODE_HEIGHT,
  WORKFLOW_NODE_WIDTH,
} from "@/config";
import { t } from "@/i18n";
import { categoryLook } from "../StatusBadge";
import {
  ANYWHERE_NODE_ID,
  fromNodeId,
  toFlow,
  type AnywhereNode as AnywhereNodeType,
  type FlowNode,
  type Selection,
  type StatusNode as StatusNodeType,
  type TransitionEdge as TransitionEdgeType,
} from "./flow";
import { canvasSize, type Design } from "./graph";

const NOTHING: Selection = { kind: "none" };

const nodeTypes = { status: StatusNode, anywhere: AnywhereNode };
const edgeTypes = { transition: TransitionEdge };

/** Only a status takes a transition, and never from itself. */
const isValidConnection: IsValidConnection = (connection) => connection.source !== connection.target && connection.target !== ANYWHERE_NODE_ID;

/** The picture of a workflow: React Flow drags, connects and takes the keyboard, while the places and curves
 * stay ours so what is drawn is what is saved; zoom is locked and the canvas scrolls like a page. */
export function WorkflowCanvas({
  design,
  statuses,
  selection = NOTHING,
  onSelect = () => {},
  onMove = () => {},
  onConnect = () => {},
  readOnly = false,
  label = t.workflows.canvas,
}: {
  design: Design;
  statuses: Map<string, Status>;
  selection?: Selection;
  onSelect?: (selection: Selection) => void;
  onMove?: (statusId: string, x: number, y: number) => void;
  onConnect?: (from: string, to: string) => void;
  readOnly?: boolean;
  label?: string;
}) {
  const size = canvasSize(design);
  const { nodes, edges } = useMemo(() => toFlow(design, statuses, selection, readOnly), [design, statuses, selection, readOnly]);

  const onNodesChange = useCallback(
    (changes: NodeChange<FlowNode>[]) => {
      for (const change of changes) {
        if (change.type === "position" && change.position) onMove(fromNodeId(change.id), change.position.x, change.position.y);
        // Keyboard selection never reaches onNodeClick, so selection is read here.
        if (change.type === "select" && change.selected) onSelect({ kind: "node", statusId: fromNodeId(change.id) });
      }
    },
    [onMove, onSelect],
  );
  const onEdgesChange = useCallback(
    (changes: EdgeChange<TransitionEdgeType>[]) => {
      for (const change of changes) {
        if (change.type === "select" && change.selected) onSelect({ kind: "edge", key: change.id });
      }
    },
    [onSelect],
  );
  const connect = useCallback((connection: Connection) => onConnect(fromNodeId(connection.source), fromNodeId(connection.target)), [onConnect]);

  return (
    <div
      role={readOnly ? "figure" : "group"}
      aria-label={label}
      data-workflow-canvas
      data-workflow-readonly={readOnly ? "" : undefined}
      className="relative bg-surface select-none"
      style={{ width: size.width, height: size.height }}
    >
      <svg width={size.width} height={size.height} className="absolute inset-0" aria-hidden>
        <defs>
          <marker id="workflow-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" className="fill-ink-muted" />
          </marker>
          <marker id="workflow-arrow-selected" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" className="fill-accent" />
          </marker>
          <pattern id="workflow-grid" width="24" height="24" patternUnits="userSpaceOnUse">
            <circle cx="1" cy="1" r="1" className="fill-border" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#workflow-grid)" />
      </svg>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={connect}
        onPaneClick={() => onSelect(NOTHING)}
        isValidConnection={isValidConnection}
        connectionMode={ConnectionMode.Loose}
        connectionRadius={WORKFLOW_CONNECT_RADIUS}
        nodesDraggable={!readOnly}
        nodesConnectable={!readOnly}
        nodesFocusable={!readOnly}
        edgesFocusable={!readOnly}
        elementsSelectable={!readOnly}
        elevateNodesOnSelect={false}
        snapToGrid
        snapGrid={[WORKFLOW_GRID, WORKFLOW_GRID]}
        nodeDragThreshold={WORKFLOW_DRAG_THRESHOLD_PX}
        defaultViewport={{ x: 0, y: 0, zoom: 1 }}
        minZoom={1}
        maxZoom={1}
        translateExtent={[
          [0, 0],
          [size.width, size.height],
        ]}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        panOnDrag={false}
        panOnScroll={false}
        preventScrolling={false}
        autoPanOnNodeDrag={false}
        autoPanOnConnect={false}
        deleteKeyCode={null}
        selectionKeyCode={null}
        multiSelectionKeyCode={null}
        style={{ width: "100%", height: "100%", background: "transparent" }}
      />
    </div>
  );
}

/** A handle that covers the box, so a transition dropped anywhere on it lands; it never starts one. */
function CoverHandle({ connectable }: { connectable: boolean }) {
  return (
    <Handle
      type="target"
      position={Position.Left}
      isConnectable={connectable}
      isConnectableStart={false}
      style={{
        position: "absolute",
        inset: 0,
        width: "100%",
        height: "100%",
        transform: "none",
        border: 0,
        background: "transparent",
        opacity: 0,
        pointerEvents: "none",
      }}
    />
  );
}

/** The ring on a card's right edge that a new transition is dragged out of, in the card's colour. */
function ConnectHandle({ label, connectable, ring }: { label: string; connectable: boolean; ring: string }) {
  return (
    <Handle
      type="source"
      position={Position.Right}
      isConnectable={connectable}
      data-connect-handle={connectable ? label : undefined}
      title={connectable ? t.workflows.dragFrom(label) : undefined}
      className={cx("rounded-full border-2 bg-surface hover:bg-accent-subtle", ring, connectable ? "cursor-crosshair" : "opacity-0")}
      style={{ width: WORKFLOW_HANDLE_SIZE, height: WORKFLOW_HANDLE_SIZE }}
    />
  );
}

function StatusNode({ data, selected }: NodeProps<StatusNodeType>) {
  const { status, initial, readOnly } = data;
  const look = categoryLook[status.category];
  const name = status.name.length > WORKFLOW_NAME_LENGTH ? `${status.name.slice(0, WORKFLOW_NAME_LENGTH - 3)}...` : status.name;
  return (
    <div
      className={cx("relative flex items-center gap-3 rounded-xl border px-4", look.card, selected && "border-accent ring-2 ring-accent/30")}
      style={{ width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT }}
    >
      <Badge className={look.badge} glyph={look.glyph} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-ink">{name}</p>
        <p className="truncate text-xs text-ink-muted" data-workflow-subtitle>
          {status.description || t.workflows.categories[status.category]}
        </p>
      </div>
      {initial && (
        <span
          data-workflow-initial
          className="absolute -top-2.5 left-4 rounded-full bg-accent px-2 py-0.5 text-2xs font-semibold tracking-wide text-on-accent uppercase"
        >
          {t.workflows.start}
        </span>
      )}
      <CoverHandle connectable={!readOnly} />
      <ConnectHandle label={status.name} connectable={!readOnly} ring={look.ring} />
    </div>
  );
}

/** The round badge that carries a card's category, so the colour is read twice. */
function Badge({ className, glyph: Glyph }: { className: string; glyph: typeof Icon.Check }) {
  return (
    <span
      className={cx("grid shrink-0 place-items-center rounded-full text-surface", className)}
      style={{ width: WORKFLOW_BADGE_SIZE, height: WORKFLOW_BADGE_SIZE }}
    >
      <Glyph />
    </span>
  );
}

function AnywhereNode({ data }: NodeProps<AnywhereNodeType>) {
  return (
    <div
      className="relative flex items-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface-raised px-4"
      style={{ width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT }}
    >
      <Badge className="bg-ink-subtle" glyph={Icon.More} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-ink">{t.workflows.anyStatus}</p>
        <p className="truncate text-xs text-ink-muted">{t.workflows.globalTransitions}</p>
      </div>
      <ConnectHandle label={t.workflows.anyStatusLower} connectable={!data.readOnly} ring="border-ink-subtle" />
    </div>
  );
}

/** Our quadratic curve between the cards' borders, with the name in a pill on its middle. */
function TransitionEdge({ id, data, selected }: EdgeProps<TransitionEdgeType>) {
  const store = useStoreApi();
  if (!data) return null;
  const { x, y } = data.shape.label;
  return (
    <>
      <BaseEdge
        path={data.shape.path}
        interactionWidth={WORKFLOW_EDGE_HIT_WIDTH}
        markerEnd={selected ? "url(#workflow-arrow-selected)" : "url(#workflow-arrow)"}
        style={{ stroke: selected ? "var(--color-accent)" : "var(--color-ink-muted)", strokeWidth: 2 }}
      />
      <EdgeLabelRenderer>
        <div
          aria-hidden
          data-workflow-edge-label={data.name}
          className={cx(
            "nopan nodrag absolute rounded-full border px-2.5 py-0.5 text-xs whitespace-nowrap shadow-1",
            selected ? "border-accent bg-accent-subtle font-medium text-accent" : "border-border bg-surface-raised text-ink",
            data.name || "text-ink-muted italic",
          )}
          style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)`, pointerEvents: data.readOnly ? "none" : "all" }}
          onClick={() => store.getState().addSelectedEdges([id])}
        >
          {data.name || t.workflows.unnamed}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

import type { TraceGraphNode } from "@/api/trace";
import type { StatusCategory } from "@/api/workflows";
import { Icon } from "@/components/icons";
import { cx, Tag } from "@/components/ui";
import { TypeIcon } from "@/features/types/TypeIcon";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { t } from "@/i18n";
import { nodeName } from "./layout";

const kindGlyph = { requirement: Icon.File, issue: Icon.Link, page: Icon.Book, test: Icon.Check, code: Icon.Code } as const;

/** What the graph marks on a node, in words, the link that reached it suspect when said. */
export function nodeMarks(node: TraceGraphNode, suspect = false): { label: string; tone: "danger" | "warning" | "muted" }[] {
  const out: { label: string; tone: "danger" | "warning" | "muted" }[] = [];
  if (suspect) out.push({ label: t.graph.suspect, tone: "warning" });
  if (node.broken) out.push({ label: t.graph.broken, tone: "danger" });
  if (node.test && (node.test.outcome === "failed" || node.test.outcome === "errored")) out.push({ label: t.graph.failing, tone: "danger" });
  if (node.test?.outdated || node.code?.outdated) out.push({ label: t.graph.outdated, tone: "warning" });
  if (node.gaps.length) {
    const names = node.gaps.map((dim) => t.coverage.dimensions[dim as keyof typeof t.coverage.dimensions] ?? dim);
    out.push({ label: t.graph.gaps(names.join(", ")), tone: "warning" });
  }
  if (node.hidden > 0) out.push({ label: t.graph.hiddenShort(node.hidden), tone: "muted" });
  return out;
}

const toneClass = { danger: "bg-danger-subtle text-danger", warning: "border-warning bg-warning-subtle text-ink", muted: "bg-surface-sunken text-ink-muted" };

/** A node as a card: what kind it is, its name and title, its status or outcome, and its marks. */
export function NodeCard({ node, title, suspect = false, compact = false }: { node: TraceGraphNode; title?: string; suspect?: boolean; compact?: boolean }) {
  const Glyph = kindGlyph[node.kind];
  const shownTitle = node.requirement?.title ?? title ?? "";
  const status = node.requirement?.status;
  const marks = nodeMarks(node, suspect);
  return (
    <div
      className={cx(
        "flex h-full min-w-0 flex-col justify-center gap-0.5 rounded-xl border px-3 py-1.5 text-left",
        node.level === 0 ? "border-accent bg-accent-subtle" : "border-border bg-surface-raised",
        node.broken && "border-dashed border-danger",
      )}
      data-node-kind={node.kind}
    >
      <div className="flex min-w-0 items-center gap-1.5">
        {node.type ? <TypeIcon icon={node.type.icon} colour={node.type.colour} /> : <Glyph className="size-3.5 shrink-0 text-ink-muted" />}
        <span className="truncate font-mono text-xs text-ink" data-node-name>
          {nodeName(node)}
        </span>
        <span className="sr-only">{`, ${t.graph.kinds[node.kind]}`}</span>
        {compact && marks.length > 0 && <Icon.Warning className="ml-auto size-3.5 shrink-0 text-warning" label={marks.map((m) => m.label).join(", ")} />}
      </div>
      {shownTitle && <p className={cx("text-sm text-ink", compact ? "truncate" : "")}>{shownTitle}</p>}
      {!compact && (
        <div className="flex flex-wrap items-center gap-1.5 pt-1">
          {status && <StatusBadge name={status.name} category={status.category as StatusCategory} />}
          {node.type && <span className="text-xs text-ink-muted">{node.type.name}</span>}
          {node.test && <Tag>{t.evidence.results.outcome[node.test.outcome] ?? node.test.outcome}</Tag>}
          {marks.map((mark) => (
            <Tag key={mark.label} className={toneClass[mark.tone]} data-node-mark={mark.label}>
              {mark.label}
            </Tag>
          ))}
        </div>
      )}
    </div>
  );
}

import type { ImportPreviewNode } from "@/api/word";
import { Icon } from "@/components/icons";
import { Card, Tag } from "@/components/ui";
import { WORD_PREVIEW_MAX_NODES } from "@/config";
import { t } from "@/i18n";

interface Branch {
  node: ImportPreviewNode;
  below: Branch[];
}

// The server sends the parts in order with a depth; nesting them makes the hierarchy what a screen reader hears.
function nest(nodes: ImportPreviewNode[]): Branch[] {
  const top: Branch[] = [];
  const open: { depth: number; below: Branch[] }[] = [{ depth: Number.NEGATIVE_INFINITY, below: top }];
  for (const node of nodes) {
    while (open.length > 1 && open[open.length - 1]!.depth >= node.depth) open.pop();
    const branch = { node, below: [] };
    open[open.length - 1]!.below.push(branch);
    open.push({ depth: node.depth, below: branch.below });
  }
  return top;
}

const kindIcon = {
  section: Icon.Folder,
  requirement: Icon.File,
  informative: Icon.Info,
  skipped: Icon.X,
} as const;

/** Each part of the document in order, and what it becomes: a section, a requirement, text that explains, or nothing. */
export function PreviewTree({ nodes }: { nodes: ImportPreviewNode[] }) {
  return (
    <Card className="space-y-2 p-4" data-word-tree={nodes.length}>
      <h3 id="word-tree-title" className="text-sm font-medium text-ink">
        {t.word.treeTitle}
      </h3>
      {nodes.length === 0 ? <p className="text-sm text-ink-muted">{t.word.treeEmpty}</p> : <Branches branches={nest(nodes)} labelledBy="word-tree-title" />}
      {nodes.length >= WORD_PREVIEW_MAX_NODES && <p className="text-sm text-ink-muted">{t.word.treeCapped(WORD_PREVIEW_MAX_NODES)}</p>}
    </Card>
  );
}

function Branches({ branches, labelledBy }: { branches: Branch[]; labelledBy?: string }) {
  return (
    <ul aria-labelledby={labelledBy} className={labelledBy ? "space-y-1" : "mt-1 space-y-1 border-l border-border pl-4"}>
      {branches.map((branch, i) => (
        <li key={i}>
          <Part node={branch.node} />
          {branch.below.length > 0 && <Branches branches={branch.below} />}
        </li>
      ))}
    </ul>
  );
}

function Part({ node }: { node: ImportPreviewNode }) {
  const KindIcon = kindIcon[node.kind];
  return (
    <div
      className={
        node.kind === "skipped" ? "flex flex-wrap items-baseline gap-x-2 text-sm text-ink-muted" : "flex flex-wrap items-baseline gap-x-2 text-sm text-ink"
      }
      data-word-node={node.kind}
    >
      <Tag className="inline-flex items-center gap-1">
        <KindIcon className="size-3.5" />
        {t.word.kinds[node.kind]}
      </Tag>
      <span className={node.kind === "section" ? "font-medium" : undefined}>{node.title || t.word.untitled}</span>
      {node.source && <span className="text-xs text-ink-muted">{node.source}</span>}
      {node.existingKey && (
        <span className="text-xs text-ink-muted" data-word-updates={node.existingKey}>
          {t.word.updates(node.existingKey, t.word.matchedBy[node.matchedBy ?? ""] ?? "")}
        </span>
      )}
      {node.losses > 0 && (
        <span className="text-xs text-ink-muted" data-word-losses={node.losses}>
          {t.word.losses(node.losses)}
        </span>
      )}
    </div>
  );
}

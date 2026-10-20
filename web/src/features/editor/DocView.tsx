import { Fragment, type ReactNode } from "react";
import { cx } from "@/components/ui";
import { attachmentUrl } from "@/api/attachments";
import { TermWords } from "@/features/glossary/TermButton";
import { t } from "@/i18n";
import { DIAGRAM_NODE, DiagramFigure, diagramLanguage, diagramSource } from "./DiagramViews";
import { imageAlt, imageWidth } from "./files";
import type { FindingMark } from "./quality";
import { safeHref, type DocMark, type DocNode } from "./schema";

/** A document read without its editor, as Stator's DocView draws a page; what the allowlist does not name is left out.
 * Findings of the writing rules, when given, are marked in its text (#207). */
export function DocView({
  doc,
  className,
  findings,
  ...rest
}: {
  doc: DocNode | null;
  className?: string;
  findings?: FindingMark[];
  [attr: `data-${string}`]: string | undefined;
}) {
  const shown = findings?.length && doc ? markFindings(doc, findings) : doc;
  return (
    <div className={cx("doc-content", className)} {...rest}>
      {(shown?.content ?? []).map((node, i) => block(node, i))}
    </div>
  );
}

// The editor's text blocks, the ones a finding's block counts.
const TEXTBLOCKS = new Set(["paragraph", "heading", "codeBlock"]);

/** The document with each finding's words carrying a finding mark, the way a comparison carries its diff marks. */
export function markFindings(doc: DocNode, findings: FindingMark[]): DocNode {
  let index = 0;
  const walk = (node: DocNode): DocNode => {
    if (TEXTBLOCKS.has(node.type)) {
      const here = findings.filter((f) => f.block === index && f.to > f.from);
      index++;
      return here.length ? { ...node, content: splitText(node.content ?? [], here) } : node;
    }
    return node.content ? { ...node, content: node.content.map(walk) } : node;
  };
  return walk(doc);
}

// Text is counted in UTF-16 code units and every other inline node as one, as the editor counts them.
function splitText(content: DocNode[], findings: FindingMark[]): DocNode[] {
  const out: DocNode[] = [];
  let at = 0;
  for (const child of content) {
    if (child.type !== "text") {
      out.push(child);
      at++;
      continue;
    }
    const text = child.text ?? "";
    const end = at + text.length;
    const cuts = [at, end, ...findings.flatMap((f) => [f.from, f.to]).filter((cut) => cut > at && cut < end)].sort((a, b) => a - b);
    for (let i = 0; i < cuts.length - 1; i++) {
      const [from, to] = [cuts[i]!, cuts[i + 1]!];
      if (to <= from) continue;
      const covering = findings.filter((f) => f.from <= from && f.to >= to);
      const marks = [
        ...(child.marks ?? []),
        ...covering.map((f) => ({ type: "finding", attrs: { key: f.key, severity: f.accepted ? "accepted" : f.severity, title: f.title ?? "" } })),
      ];
      out.push({ ...child, text: text.slice(from - at, to - at), marks });
    }
    at = end;
  }
  return out;
}

/** One block of a comparison and what happened to it. */
export interface DiffBlockView {
  change: "equal" | "inserted" | "deleted" | "modified";
  node: DocNode;
}

/** Two revisions of a document compared, block by block, each changed block labelled so colour is never the only sign. */
/** A title compared as one paragraph, inline where the title stands: its words, the changed ones marked. */
export function DiffTitle({ blocks }: { blocks: DiffBlockView[] }) {
  return (
    <span data-diff-title>
      {blocks.map((b, i) => {
        const words = inline(b.node);
        if (b.change === "deleted") return <del key={i}>{words}</del>;
        if (b.change === "inserted") return <ins key={i}>{words}</ins>;
        return <Fragment key={i}>{words}</Fragment>;
      })}
    </span>
  );
}

export function DocDiffView({ blocks, ...rest }: { blocks: DiffBlockView[]; [attr: `data-${string}`]: string | undefined }) {
  const labels = { inserted: t.history.added, deleted: t.history.removed, modified: t.history.changed };
  return (
    <div className="doc-content text-ink" data-diff-view {...rest}>
      {blocks.map((b, i) => (
        <div key={i} className="doc-diff-block" data-diff-block={b.change}>
          {b.change !== "equal" && <span className="doc-diff-label">{labels[b.change]}</span>}
          {block(b.node, 0)}
        </div>
      ))}
    </div>
  );
}

function children(node: DocNode): ReactNode[] {
  return (node.content ?? []).map((child, i) => block(child, i));
}

function block(node: DocNode, key: number): ReactNode {
  switch (node.type) {
    case "paragraph":
      return <p key={key}>{inline(node)}</p>;
    case "heading": {
      const level = Number(node.attrs?.level ?? 1);
      if (level === 1) return <h1 key={key}>{inline(node)}</h1>;
      if (level === 2) return <h2 key={key}>{inline(node)}</h2>;
      return <h3 key={key}>{inline(node)}</h3>;
    }
    case "bulletList":
      return <ul key={key}>{children(node)}</ul>;
    case "orderedList":
      return (
        <ol key={key} start={typeof node.attrs?.start === "number" ? node.attrs.start : undefined}>
          {children(node)}
        </ol>
      );
    case "listItem":
      return <li key={key}>{children(node)}</li>;
    case "blockquote":
      return <blockquote key={key}>{children(node)}</blockquote>;
    case "codeBlock":
      return (
        <pre key={key}>
          <code>{inline(node)}</code>
        </pre>
      );
    case "horizontalRule":
      return <hr key={key} />;
    case "image": {
      const id = String(node.attrs?.attachmentId ?? "");
      const width = imageWidth(node.attrs?.width);
      return (
        <figure key={key} data-image="" data-attachment-id={id}>
          <img src={attachmentUrl(id, true)} alt={imageAlt(node.attrs?.alt) ?? ""} style={width ? { width: `${width}px` } : undefined} />
        </figure>
      );
    }
    case DIAGRAM_NODE: {
      const source = diagramSource(node.attrs?.source);
      return source ? <DiagramFigure key={key} language={diagramLanguage(node.attrs?.language)} source={source} /> : null;
    }
    case "table":
      return (
        <div key={key} className="doc-table-wrap">
          <table>
            <tbody>{children(node)}</tbody>
          </table>
        </div>
      );
    case "tableRow":
      return <tr key={key}>{children(node)}</tr>;
    case "tableCell":
    case "tableHeader": {
      const Cell = node.type === "tableHeader" ? "th" : "td";
      const span = (name: string) => (typeof node.attrs?.[name] === "number" && (node.attrs[name] as number) > 1 ? (node.attrs[name] as number) : undefined);
      return (
        <Cell key={key} colSpan={span("colspan")} rowSpan={span("rowspan")}>
          {children(node)}
        </Cell>
      );
    }
    default:
      return null;
  }
}

function inline(node: DocNode): ReactNode[] {
  return (node.content ?? []).map((child, i) => {
    if (child.type === "hardBreak") return <br key={i} />;
    if (child.type === "mention") {
      return (
        <span key={i} className="mention" data-mention={String(child.attrs?.id ?? "")}>
          @{String(child.attrs?.label ?? "")}
        </span>
      );
    }
    if (child.type === "attachment") {
      const id = String(child.attrs?.attachmentId ?? "");
      const name = String(child.attrs?.fileName ?? "");
      return (
        <a key={i} href={attachmentUrl(id)} download={name} data-attachment-id={id} aria-label={t.attachments.download(name)}>
          {name}
        </a>
      );
    }
    if (child.type !== "text") return null;
    return (child.marks ?? []).reduceRight<ReactNode>((inner, mark) => marked(mark, inner, i), child.text ?? "");
  });
}

function marked(mark: DocMark, inner: ReactNode, key: number): ReactNode {
  switch (mark.type) {
    case "bold":
      return <strong key={key}>{inner}</strong>;
    case "italic":
      return <em key={key}>{inner}</em>;
    case "strike":
      return <s key={key}>{inner}</s>;
    case "code":
      return <code key={key}>{inner}</code>;
    // Only a comparison carries these two; a screen reader is told where each starts and ends, since it
    // announces neither element by itself.
    case "diffInsert":
      return (
        <ins key={key} data-diff="insert">
          <span className="sr-only">{t.history.insertedStart}</span>
          {inner}
          <span className="sr-only">{t.history.insertedEnd}</span>
        </ins>
      );
    case "diffDelete":
      return (
        <del key={key} data-diff="delete">
          <span className="sr-only">{t.history.deletedStart}</span>
          {inner}
          <span className="sr-only">{t.history.deletedEnd}</span>
        </del>
      );
    // Only a requirement read with its findings shown carries this one (#207).
    case "finding":
      return (
        <span
          key={key}
          className={`finding finding-${String(mark.attrs?.severity ?? "hint")}`}
          data-finding={String(mark.attrs?.key ?? "")}
          title={String(mark.attrs?.title ?? "") || undefined}
        >
          {inner}
        </span>
      );
    // Words linked to a glossary term on purpose (#210).
    case "term": {
      const termId = typeof mark.attrs?.termId === "string" ? mark.attrs.termId : "";
      return termId ? (
        <TermWords key={key} termId={termId}>
          {inner}
        </TermWords>
      ) : (
        inner
      );
    }
    case "link": {
      const href = safeHref(mark.attrs?.href);
      return href ? (
        <a key={key} href={href} target="_blank" rel="noopener noreferrer nofollow">
          {inner}
        </a>
      ) : (
        inner
      );
    }
    default:
      return inner;
  }
}

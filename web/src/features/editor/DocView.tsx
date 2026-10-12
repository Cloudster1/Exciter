import type { ReactNode } from "react";
import { cx } from "@/components/ui";
import { safeHref, type DocMark, type DocNode } from "./schema";

/** A document read without its editor, as Stator's DocView draws a page; what the allowlist does not name is left out. */
export function DocView({ doc, className, ...rest }: { doc: DocNode | null; className?: string; [attr: `data-${string}`]: string | undefined }) {
  return (
    <div className={cx("doc-content", className)} {...rest}>
      {(doc?.content ?? []).map((node, i) => block(node, i))}
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
          <code>{(node.content ?? []).map((c) => c.text ?? "").join("")}</code>
        </pre>
      );
    case "horizontalRule":
      return <hr key={key} />;
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

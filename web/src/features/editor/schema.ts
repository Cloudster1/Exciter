/** The document the editor, the reader and the server agree on. The server's allowlist is the authority;
 * allowlist.test.ts keeps this side within it. */
export interface DocMark {
  type: string;
  attrs?: Record<string, unknown>;
}

export interface DocNode {
  type: string;
  text?: string;
  attrs?: Record<string, unknown>;
  marks?: DocMark[];
  content?: DocNode[];
}

export interface Doc extends DocNode {
  type: "doc";
}

export const emptyDoc: Doc = { type: "doc", content: [{ type: "paragraph" }] };

/** A document that says nothing: no blocks, or only paragraphs with nothing in them. */
export function isEmptyDoc(doc: DocNode | null | undefined): boolean {
  if (!doc) return true;
  return (doc.content ?? []).every((block) => block.type === "paragraph" && !(block.content && block.content.length > 0));
}

const SCHEMES = ["http", "https", "mailto"];
const MAX_HREF_LENGTH = 2048;

/** A web or mail address or a link within the site, and nothing a browser would run; the API's SafeHref. */
export function safeHref(href: unknown): string | null {
  if (typeof href !== "string" || href === "" || [...href].length > MAX_HREF_LENGTH) return null;
  // Browsers drop tabs and newlines inside a URL, so "java\tscript:" would run; a backslash reads as a slash.
  for (const ch of href) {
    const code = ch.charCodeAt(0);
    if (code <= 0x20 || code === 0x7f || ch === "\\") return null;
  }
  if (href.startsWith("//")) return null;
  const colon = href.indexOf(":");
  const end = href.search(/[/?#]/);
  if (colon < 0 || (end >= 0 && end < colon)) return href;
  const scheme = href.slice(0, colon).toLowerCase();
  if (!SCHEMES.includes(scheme)) return null;
  if (scheme === "mailto") return href.length > colon + 1 ? href : null;
  try {
    return new URL(href).host ? href : null;
  } catch {
    return null;
  }
}

/** The text of a node and everything under it, for previews. */
export function textOf(node: DocNode): string {
  if (node.type === "text") return node.text ?? "";
  if (node.type === "mention") return `@${String(node.attrs?.label ?? "")}`;
  if (node.type === "hardBreak") return "\n";
  return (node.content ?? []).map(textOf).join(node.type === "doc" ? "\n" : "");
}

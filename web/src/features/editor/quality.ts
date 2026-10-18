import { Extension } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";

// The writing rules' findings (#207) as marks in the text: decorations only, so the document and the server's
// allowlist never see them.

/** A finding where it stands: the text block counted in document order, and offsets within its inline content. */
export interface FindingMark {
  key: string;
  severity: "error" | "warning" | "hint";
  block: number;
  from: number;
  to: number;
  accepted?: boolean;
  /** What a pointer resting on the mark reads: the rule and how to fix it. */
  title?: string;
}

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    qualityMarks: {
      /** Marks these findings, in place of the ones marked before. */
      setFindings: (findings: FindingMark[]) => ReturnType;
      /** Selects a finding's words and brings them into view. */
      showFinding: (finding: Pick<FindingMark, "block" | "from" | "to">) => ReturnType;
    };
  }
}

/** Where each text block's inline content starts and how long it is, in the order the server counts them. */
export function textblocks(doc: PMNode): { start: number; size: number }[] {
  const found: { start: number; size: number }[] = [];
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return true;
    found.push({ start: pos + 1, size: node.content.size });
    return false;
  });
  return found;
}

/** A finding's range in the document, held inside its block; null when the block is gone or the range is empty. */
export function findingRange(
  blocks: { start: number; size: number }[],
  finding: Pick<FindingMark, "block" | "from" | "to">,
): { from: number; to: number } | null {
  const block = blocks[finding.block];
  if (!block) return null;
  const from = Math.max(0, Math.min(finding.from, block.size));
  const to = Math.max(from, Math.min(finding.to, block.size));
  return to > from ? { from: block.start + from, to: block.start + to } : null;
}

function decorate(doc: PMNode, findings: FindingMark[]): DecorationSet {
  const blocks = textblocks(doc);
  const marks = findings.flatMap((finding) => {
    const range = findingRange(blocks, finding);
    if (!range) return [];
    const attrs: Record<string, string> = {
      class: `finding finding-${finding.accepted ? "accepted" : finding.severity}`,
      "data-finding": finding.key,
    };
    if (finding.title) attrs.title = finding.title;
    return [Decoration.inline(range.from, range.to, attrs, { finding: finding.key })];
  });
  return DecorationSet.create(doc, marks);
}

export const qualityKey = new PluginKey<DecorationSet>("qualityMarks");

/** Holds the marks of the latest answer and moves them with the text typed since, until the next answer comes. */
export const QualityMarks = Extension.create({
  name: "qualityMarks",
  addCommands() {
    return {
      setFindings:
        (findings) =>
        ({ tr, dispatch }) => {
          if (dispatch) tr.setMeta(qualityKey, findings).setMeta("addToHistory", false);
          return true;
        },
      showFinding:
        (finding) =>
        ({ tr, dispatch }) => {
          const range = findingRange(textblocks(tr.doc), finding);
          if (!range) return false;
          if (dispatch) tr.setSelection(TextSelection.create(tr.doc, range.from, range.to)).scrollIntoView();
          return true;
        },
    };
  },
  addProseMirrorPlugins() {
    return [
      new Plugin<DecorationSet>({
        key: qualityKey,
        state: {
          init: () => DecorationSet.empty,
          apply: (tr, set) => {
            const findings = tr.getMeta(qualityKey) as FindingMark[] | undefined;
            return findings ? decorate(tr.doc, findings) : set.map(tr.mapping, tr.doc);
          },
        },
        props: { decorations: (state) => qualityKey.getState(state) },
      }),
    ];
  },
});

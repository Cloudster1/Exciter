import { Extension, Mark, mergeAttributes } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { findingRange, textblocks } from "./quality";

// The glossary in the text (#210): the `term` mark an author puts on words on purpose, which the document keeps and
// the server's allowlist names, and the uses the check found, which are decorations only and never saved.

/** A use of a term where it stands, counted as findings are: the text block in document order, offsets within it. */
export interface TermMark {
  termId: string;
  name: string;
  /** The start of the definition, what a pointer or the caret resting on the words shows. */
  preview: string;
  explicit: boolean;
  block: number;
  from: number;
  to: number;
}

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    term: {
      /** Links the selected words to a term. */
      setTerm: (termId: string) => ReturnType;
      /** Takes the link to a term off the words the caret is in. */
      unsetTerm: () => ReturnType;
    };
    termMarks: {
      /** Marks these uses, in place of the ones marked before. */
      setTermUses: (uses: TermMark[]) => ReturnType;
    };
  }
}

/** Words linked to a term on purpose: a span carrying the term's id, which the reader draws as a button. */
export const TermLink = Mark.create({
  name: "term",
  inclusive: false,
  addAttributes() {
    return {
      termId: {
        default: null,
        parseHTML: (element) => element.getAttribute("data-term-id"),
        renderHTML: (attributes) => ({ "data-term-id": attributes.termId as string }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "span[data-term-id]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["span", mergeAttributes(HTMLAttributes, { class: "term-link" }), 0];
  },
  addCommands() {
    return {
      setTerm:
        (termId) =>
        ({ commands }) =>
          commands.setMark(this.name, { termId }),
      unsetTerm:
        () =>
        ({ chain }) =>
          chain().extendMarkRange(this.name).unsetMark(this.name).run(),
    };
  },
});

/** The key a use's words carry in the editor, so the popover finds them where the caret is. */
export const termUseAt = (use: Pick<TermMark, "block" | "from">) => `${use.block}:${use.from}`;

function decorate(doc: PMNode, uses: TermMark[]): DecorationSet {
  const blocks = textblocks(doc);
  const marks = uses.flatMap((use) => {
    const range = findingRange(blocks, use);
    if (!range) return [];
    const attrs = { class: use.explicit ? "term-use term-use-explicit" : "term-use", "data-term-use": use.termId, "data-term-use-at": termUseAt(use) };
    return [Decoration.inline(range.from, range.to, attrs, { term: use })];
  });
  return DecorationSet.create(doc, marks);
}

export const termMarksKey = new PluginKey<DecorationSet>("termMarks");

/** The use the caret is in, or the first one the selection touches; null when it is in none. */
export function termUseAtSelection(state: EditorState): TermMark | null {
  const set = termMarksKey.getState(state);
  if (!set) return null;
  const { from, to } = state.selection;
  const found = set.find(from, to, (spec) => Boolean(spec.term));
  return (found[0]?.spec.term as TermMark | undefined) ?? null;
}

/** Holds the uses of the latest answer and moves them with the text typed since, as the findings' marks do. */
export const TermMarks = Extension.create({
  name: "termMarks",
  addCommands() {
    return {
      setTermUses:
        (uses) =>
        ({ tr, dispatch }) => {
          if (dispatch) tr.setMeta(termMarksKey, uses).setMeta("addToHistory", false);
          return true;
        },
    };
  },
  addProseMirrorPlugins() {
    return [
      new Plugin<DecorationSet>({
        key: termMarksKey,
        state: {
          init: () => DecorationSet.empty,
          apply: (tr, set) => {
            const uses = tr.getMeta(termMarksKey) as TermMark[] | undefined;
            return uses ? decorate(tr.doc, uses) : set.map(tr.mapping, tr.doc);
          },
        },
        props: { decorations: (state) => termMarksKey.getState(state) },
      }),
    ];
  },
});

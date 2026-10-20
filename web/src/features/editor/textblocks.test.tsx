import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { stubApi } from "@/test/app";
import { Editor } from "@tiptap/core";
import { Node } from "@tiptap/pm/model";
import { DocView } from "./DocView";
import { editorExtensions } from "./extensions";
import { textblocks, type FindingMark } from "./quality";
import type { DocNode } from "./schema";

// The text blocks as the server counts them (#207), written by document.TextBlocks: each block's plain text and, for
// every byte of it, the offset in the block that the editor counts.
interface Case {
  name: string;
  doc: DocNode;
  blocks: { text: string; offsets: number[] }[];
}
const cases = JSON.parse(readFileSync(resolve(process.cwd(), "../api/document-textblocks.json"), "utf8")) as Case[];

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

function make(doc: DocNode) {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions(), content: doc });
  return editor;
}

const bytes = (text: string) => new TextEncoder().encode(text).length;

/** The last word of a block's text and where it stands, in the server's offsets. */
function lastWord(block: Case["blocks"][number]): { word: string; from: number; to: number } | null {
  const words = [...block.text.matchAll(/[\p{L}\p{N}]+/gu)];
  const found = words.at(-1);
  if (!found || found.index === undefined) return null;
  const start = bytes(block.text.slice(0, found.index));
  return { word: found[0], from: block.offsets[start]!, to: block.offsets[start + bytes(found[0])]! };
}

function marks(c: Case): { mark: FindingMark; word: string }[] {
  return c.blocks.flatMap((block, i) => {
    const found = lastWord(block);
    return found ? [{ word: found.word, mark: { key: `${c.name} ${i}`, severity: "warning" as const, block: i, from: found.from, to: found.to } }] : [];
  });
}

describe("the server's text blocks", () => {
  it.each(cases.map((c) => [c.name, c] as const))("are the editor's, %s", (_name, c) => {
    const doc = Node.fromJSON(make(c.doc).schema, c.doc);
    const blocks = textblocks(doc);
    expect(blocks).toHaveLength(c.blocks.length);
    blocks.forEach((block, i) => {
      const offsets = c.blocks[i]!.offsets;
      expect(offsets[bytes(c.blocks[i]!.text)], `block ${i}`).toBe(block.size);
    });
  });

  it.each(cases.map((c) => [c.name, c] as const))("mark a finding's words in the editor, %s", (_name, c) => {
    const e = make(c.doc);
    const expected = marks(c);
    e.commands.setFindings(expected.map((each) => each.mark));
    for (const { mark, word } of expected) {
      const found = e.view.dom.querySelector(`[data-finding="${mark.key}"]`);
      expect(found?.textContent, mark.key).toBe(word);
      expect(found?.className).toContain("finding-warning");
    }
  });

  it.each(cases.map((c) => [c.name, c] as const))("mark a finding's words in a requirement read, %s", (_name, c) => {
    const expected = marks(c);
    // A diagram among the blocks asks for its drawing, which this test leaves unanswered.
    stubApi({});
    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <DocView doc={c.doc} findings={expected.map((each) => each.mark)} />
      </QueryClientProvider>,
    );
    for (const { mark, word } of expected) {
      expect(container.querySelector(`[data-finding="${mark.key}"]`)?.textContent, mark.key).toBe(word);
    }
  });
});

describe("the marks in the editor", () => {
  it("move with the text typed before them, and go with the next answer", () => {
    const e = make({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "The system shall respond quickly." }] }] });
    e.commands.setFindings([{ key: "vague:statement:quickly:0", severity: "warning", block: 0, from: 25, to: 32 }]);
    expect(e.view.dom.querySelector(".finding")?.textContent).toBe("quickly");
    e.commands.insertContentAt(1, "Then ");
    expect(e.view.dom.querySelector(".finding")?.textContent).toBe("quickly");
    e.commands.setFindings([]);
    expect(e.view.dom.querySelector(".finding")).toBeNull();
  });

  it("select a finding's words when shown", () => {
    const e = make({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "It shall stop." }] }] });
    e.commands.showFinding({ block: 0, from: 0, to: 2 });
    const { from, to } = e.state.selection;
    expect(e.state.doc.textBetween(from, to)).toBe("It");
  });
});

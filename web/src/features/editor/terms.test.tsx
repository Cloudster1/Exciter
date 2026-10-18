import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Editor as TiptapEditor } from "@tiptap/core";
import { problems } from "@/test/allowlist";
import { axeViolations } from "@/test/axe";
import { Editor } from "./Editor";
import { editorExtensions } from "./extensions";
import type { Doc, DocNode } from "./schema";
import { termUseAt, termUseAtSelection, type TermMark } from "./terms";

// Glossary terms in the editor (#210): the `term` mark an author links words with, which the server's allowlist
// names, and the uses the check finds, marked where the server's positions say and explained beside the words.

interface Case {
  name: string;
  doc: DocNode;
  blocks: { text: string; offsets: number[] }[];
}
const cases = JSON.parse(readFileSync(resolve(process.cwd(), "../api/document-textblocks.json"), "utf8")) as Case[];

const TERM_ID = "0195f000-0000-7000-8000-0000000007a1";
const bytes = (text: string) => new TextEncoder().encode(text).length;

let editor: TiptapEditor | undefined;
afterEach(() => editor?.destroy());

function make(doc: DocNode) {
  editor = new TiptapEditor({ element: document.createElement("div"), extensions: editorExtensions(), content: doc });
  return editor;
}

/** The first word of each block, as a use of a term where the server's offsets put it. */
function uses(c: Case): { use: TermMark; word: string }[] {
  return c.blocks.flatMap((block, i) => {
    const found = [...block.text.matchAll(/[\p{L}\p{N}]+/gu)][0];
    if (!found || found.index === undefined) return [];
    const start = bytes(block.text.slice(0, found.index));
    const use: TermMark = {
      termId: TERM_ID,
      name: "Term",
      preview: "",
      explicit: i % 2 === 1,
      block: i,
      from: block.offsets[start]!,
      to: block.offsets[start + bytes(found[0])]!,
    };
    return [{ use, word: found[0] }];
  });
}

const sentence: Doc = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "The brake pedals shall return." }] }] };
const pedals: TermMark = {
  termId: TERM_ID,
  name: "Brake pedal",
  preview: "The lever the driver presses to slow the car.",
  explicit: false,
  block: 0,
  from: 4,
  to: 16,
};

describe("the term mark", () => {
  it("links the selected words to a term, in a document the server accepts, and comes off again", () => {
    const e = make(sentence);
    e.chain().setTextSelection({ from: 5, to: 17 }).setTerm(TERM_ID).run();
    const doc = e.getJSON() as DocNode;
    expect(problems(doc)).toEqual([]);
    expect(doc.content?.[0]?.content?.[1]).toEqual({ type: "text", text: "brake pedals", marks: [{ type: "term", attrs: { termId: TERM_ID } }] });
    expect(e.getHTML()).toContain(`<span data-term-id="${TERM_ID}" class="term-link">brake pedals</span>`);
    e.chain().setTextSelection(8).unsetTerm().run();
    expect(JSON.stringify(e.getJSON())).not.toContain('"term"');
  });

  it("is read back from the editor's own markup", () => {
    const e = make({ type: "doc", content: [{ type: "paragraph" }] });
    e.commands.setContent(`<p>The <span data-term-id="${TERM_ID}">pedal</span> returns.</p>`);
    expect(JSON.stringify(e.getJSON())).toContain(`{"type":"term","attrs":{"termId":"${TERM_ID}"}}`);
  });
});

describe("the uses of terms in the editor", () => {
  it.each(cases.map((c) => [c.name, c] as const))("mark a term's words where the server's positions say, %s", (_name, c) => {
    const e = make(c.doc);
    const expected = uses(c);
    e.commands.setTermUses(expected.map((each) => each.use));
    for (const { use, word } of expected) {
      const found = e.view.dom.querySelector(`[data-term-use-at="${termUseAt(use)}"]`);
      expect(found?.textContent, termUseAt(use)).toBe(word);
      expect(found?.classList.contains("term-use")).toBe(true);
      expect(found?.classList.contains("term-use-explicit")).toBe(use.explicit);
    }
  });

  it("move with the text typed before them, say which the caret is in, and go with the next answer", () => {
    const e = make(sentence);
    e.commands.setTermUses([pedals]);
    expect(e.view.dom.querySelector(".term-use")?.textContent).toBe("brake pedals");
    e.commands.insertContentAt(1, "Then ");
    expect(e.view.dom.querySelector(".term-use")?.textContent).toBe("brake pedals");
    e.commands.setTextSelection(12);
    expect(termUseAtSelection(e.state)?.name).toBe("Brake pedal");
    e.commands.setTextSelection(1);
    expect(termUseAtSelection(e.state)).toBeNull();
    e.commands.setTermUses([]);
    expect(e.view.dom.querySelector(".term-use")).toBeNull();
  });
});

describe("the definition beside a term's words", () => {
  it("shows while a pointer rests on them", async () => {
    render(
      <main>
        <h1>A requirement</h1>
        <Editor id="statement" value={sentence} onChange={() => {}} terms={[pedals]} aria-label="Statement" />
      </main>,
    );
    const words = await waitFor(() => {
      const found = document.querySelector<HTMLElement>("#statement .term-use");
      expect(found).not.toBeNull();
      return found!;
    });
    expect(words.textContent).toBe("brake pedals");
    fireEvent.mouseOver(words);
    const tip = await screen.findByRole("tooltip");
    expect(tip).toHaveTextContent("Brake pedal");
    expect(tip).toHaveTextContent("The lever the driver presses to slow the car.");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    fireEvent.mouseLeave(document.querySelector("[data-editor-frame]")!);
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
  });
});

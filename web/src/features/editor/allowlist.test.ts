import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { allowlist, commentAllowlist, problems } from "@/test/allowlist";
import { commentExtensions, editorExtensions } from "./extensions";
import type { DocNode } from "./schema";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

function make() {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions() });
  return editor;
}

describe("the web editor against the server's allowlist", () => {
  it("has exactly the server's nodes and marks, each with the server's attributes", () => {
    const e = make();
    for (const [name, type] of Object.entries(e.schema.nodes)) {
      expect(allowlist.nodes, `node ${name}`).toHaveProperty([name]);
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(allowlist.nodes[name]?.attrs ?? {}).sort());
    }
    for (const [name, type] of Object.entries(e.schema.marks)) {
      expect(allowlist.marks, `mark ${name}`).toHaveProperty([name]);
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(allowlist.marks[name]?.attrs ?? {}).sort());
    }
    expect(Object.keys(allowlist.nodes).sort()).toEqual(Object.keys(e.schema.nodes).sort());
    expect(Object.keys(allowlist.marks).sort()).toEqual(Object.keys(e.schema.marks).sort());
  });

  it("writes documents the server accepts, from every block and style it offers", () => {
    const e = make();
    e.chain()
      .insertContent([
        { type: "paragraph", content: [{ type: "text", text: "styled" }] },
        { type: "paragraph", content: [{ type: "text", text: "linked" }] },
      ])
      .run();
    e.chain().selectAll().toggleBold().toggleItalic().toggleStrike().run();
    e.chain().setTextSelection({ from: 2, to: 4 }).setLink({ href: "https://example.test" }).run();
    e.chain()
      .focus("end")
      .setHardBreak()
      .insertContent({ type: "mention", attrs: { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", label: "Ada" } })
      .insertContent(" ")
      .toggleCode()
      .insertContent("x")
      .run();
    e.chain()
      .focus("end")
      .insertContent({ type: "orderedList", attrs: { start: 3 }, content: [{ type: "listItem", content: [{ type: "paragraph" }] }] })
      .run();
    e.chain().focus("end").toggleBulletList().run();
    e.chain().focus("end").insertTable({ rows: 2, cols: 2, withHeaderRow: true }).run();
    e.chain().focus("end").setCodeBlock({ language: "go" }).insertContent("x := 1").run();
    e.chain().focus("end").setHeading({ level: 3 }).insertContent("Scope").run();
    e.chain().focus("end").toggleBlockquote().setHorizontalRule().run();
    e.commands.insertContentAt(e.state.doc.content.size, [
      { type: "image", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02", alt: "The basket", width: 320 } },
      { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a03", fileName: "spec.pdf" } }] },
    ]);
    const doc = e.getJSON() as DocNode;
    expect(problems(doc)).toEqual([]);
    expect(JSON.stringify(doc)).toContain('"type":"image"');
    expect(JSON.stringify(doc)).toContain('"type":"attachment"');
  });

  it("refuses a link a browser would run", () => {
    const e = make();
    e.commands.insertContent({ type: "paragraph", content: [{ type: "text", text: "click" }] });
    e.chain().selectAll().setLink({ href: "javascript:alert(1)" }).run();
    expect(JSON.stringify(e.getJSON())).not.toContain("javascript:");
  });
});

describe("the comment editor against the server's comment allowlist", () => {
  it("has exactly the nodes and marks a comment may hold", () => {
    editor = new Editor({ element: document.createElement("div"), extensions: commentExtensions() });
    expect(Object.keys(commentAllowlist.nodes).sort()).toEqual(Object.keys(editor.schema.nodes).sort());
    expect(Object.keys(commentAllowlist.marks).sort()).toEqual(Object.keys(editor.schema.marks).sort());
    for (const [name, type] of Object.entries(editor.schema.nodes)) {
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(commentAllowlist.nodes[name]?.attrs ?? {}).sort());
    }
  });
});

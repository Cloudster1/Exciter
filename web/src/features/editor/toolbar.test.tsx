import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Editor as TiptapEditor } from "@tiptap/core";
import { stubApi } from "@/test/app";
import { Editor } from "./Editor";
import type { Doc, DocNode } from "./schema";

// The editor's toolbar and its link dialog (#309): a button says what the caret is in, the tools follow what the
// editor is given, and a link takes only an address a browser would not run.

const LINK_SENTENCE = "Give a web or mail address, such as https://example.com. Leave it empty to remove the link.";

function shown(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>);
}

/** The editor behind the editable element of id, which TipTap hangs on its element. */
function editorOf(id: string): TiptapEditor {
  const dom = document.getElementById(id) as (HTMLElement & { editor?: TiptapEditor }) | null;
  if (!dom?.editor) throw new Error(`No editor on #${id}.`);
  return dom.editor;
}

const paragraph = (...content: DocNode[]): Doc => ({ type: "doc", content: [{ type: "paragraph", content }] });

/** Where the caret goes in the paragraph: ProseMirror counts the paragraph's opening as one position. */
const at = (offset: number) => offset + 1;

/** How often the requirement's form around the editor was submitted. */
let formSubmits = 0;

/** The editor in a requirement's form, as the pages hold it, with what it was given and every document it handed
 *  out since. */
function writing(value: Doc, props: Partial<Parameters<typeof Editor>[0]> = {}) {
  stubApi({});
  formSubmits = 0;
  const docs: Doc[] = [];
  shown(
    <form
      onSubmit={(event) => {
        event.preventDefault();
        formSubmits++;
      }}
    >
      <Editor id="statement" value={value} onChange={(doc) => docs.push(doc)} aria-label="Statement" {...props} />
    </form>,
  );
  return docs;
}

function marksIn(doc: Doc | undefined): unknown[] {
  return (doc?.content?.[0]?.content ?? []).flatMap((node) => node.marks ?? []);
}

describe("the toolbar", () => {
  it("presses Bold while the caret is in bold words, and not elsewhere", async () => {
    writing(paragraph({ type: "text", text: "Pay for " }, { type: "text", text: "everything", marks: [{ type: "bold" }] }));
    const bold = await screen.findByRole("button", { name: "Bold" });
    act(() => void editorOf("statement").commands.setTextSelection(at("Pay for every".length)));
    expect(bold).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Italic" })).toHaveAttribute("aria-pressed", "false");
    act(() => void editorOf("statement").commands.setTextSelection(at("Pa".length)));
    expect(bold).toHaveAttribute("aria-pressed", "false");
  });

  it("offers a file only with somewhere to put it, and a term only with a glossary", async () => {
    stubApi({});
    const { unmount } = shown(<Editor id="plain" value={null} onChange={() => {}} aria-label="Statement" />);
    await screen.findByRole("button", { name: "Bold" });
    expect(screen.queryByRole("button", { name: "Attach a file here" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Link to term" })).toBeNull();
    unmount();

    shown(<Editor id="full" value={null} onChange={() => {}} upload={vi.fn()} glossary="CHK" aria-label="Statement" />);
    expect(await screen.findByRole("button", { name: "Attach a file here" })).toHaveAttribute("data-action", "format-attach");
    expect(screen.getByRole("button", { name: "Link to term" })).toHaveAttribute("data-action", "format-term");
  });

  it("offers a comment no divider, table, diagram, file or term", async () => {
    stubApi({});
    shown(<Editor id="comment" value={null} onChange={() => {}} variant="comment" upload={vi.fn()} glossary="CHK" aria-label="Comment" />);
    const toolbar = await screen.findByRole("toolbar", { name: "Formatting" });
    for (const name of ["Bold", "Italic", "Strike through", "Code", "Heading 2", "Bulleted list", "Numbered list", "Quote", "Code block", "Link"]) {
      expect(within(toolbar).getByRole("button", { name })).toBeInTheDocument();
    }
    for (const name of ["Divider", "Table", "PlantUML diagram", "Attach a file here", "Link to term"]) {
      expect(within(toolbar).queryByRole("button", { name })).toBeNull();
    }
  });
});

describe("the link dialog", () => {
  const linked = paragraph(
    { type: "text", text: "Pay at " },
    { type: "text", text: "the shop", marks: [{ type: "link", attrs: { href: "https://shop.test" } }] },
  );

  async function openOn(selection: number | { from: number; to: number }) {
    await screen.findByRole("button", { name: "Bold" });
    act(() => void editorOf("statement").commands.setTextSelection(selection));
    await userEvent.click(screen.getByRole("button", { name: "Link" }));
    return screen.getByRole("dialog", { name: "Link" });
  }

  it("opens with the address of the link the caret is in", async () => {
    writing(linked);
    const dialog = await openOn(at("Pay at the".length));
    expect(within(dialog).getByLabelText("Address")).toHaveValue("https://shop.test");
  });

  it("refuses an address a browser would run, and changes nothing", async () => {
    const docs = writing(paragraph({ type: "text", text: "Pay at once" }));
    const dialog = await openOn({ from: at(0), to: at("Pay".length) });
    const address = within(dialog).getByLabelText("Address");
    for (const href of ["javascript:alert(1)", "data:text/html,<b>x</b>"]) {
      await userEvent.clear(address);
      await userEvent.type(address, href);
      await userEvent.click(within(dialog).getByRole("button", { name: "Save link" }));
      expect(within(dialog).getByText(LINK_SENTENCE)).toBeInTheDocument();
      expect(address).toHaveAttribute("aria-invalid", "true");
    }
    expect(marksIn(docs.at(-1))).toEqual([]);
    expect(JSON.stringify(editorOf("statement").getJSON())).not.toContain("link");
    expect(formSubmits).toBe(0);
  });

  it("puts a safe address on the selected words", async () => {
    const docs = writing(paragraph({ type: "text", text: "Pay at once" }));
    const dialog = await openOn({ from: at(0), to: at("Pay".length) });
    await userEvent.type(within(dialog).getByLabelText("Address"), "https://example.com");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save link" }));
    expect(screen.queryByRole("dialog", { name: "Link" })).toBeNull();
    expect(docs.at(-1)?.content?.[0]?.content?.[0]).toMatchObject({ text: "Pay", marks: [{ type: "link", attrs: { href: "https://example.com" } }] });
    // The dialog's own submit saves the link, never the requirement's form it sits in.
    expect(formSubmits).toBe(0);
  });

  it("takes the link off when its address is emptied", async () => {
    const docs = writing(linked);
    const dialog = await openOn(at("Pay at the".length));
    await userEvent.clear(within(dialog).getByLabelText("Address"));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save link" }));
    expect(screen.queryByRole("dialog", { name: "Link" })).toBeNull();
    expect(marksIn(docs.at(-1))).toEqual([]);
    expect(
      docs
        .at(-1)
        ?.content?.[0]?.content?.map((node) => node.text)
        .join(""),
    ).toBe("Pay at the shop");
  });
});

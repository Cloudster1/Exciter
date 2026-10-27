import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Editor as TiptapEditor } from "@tiptap/core";
import { stubApi } from "@/test/app";
import { Editor } from "./Editor";

// The people an at sign offers sit in the landmark the editor is in (#377), where a screen reader finds them, and
// not loose at the end of the page.

const BOB = { id: "00000000-0000-4000-8000-000000000002", name: "Bob Builder", email: "bob@example.com" };

function writingIn(wrap: (editor: ReactNode) => ReactNode) {
  stubApi({});
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      {wrap(<Editor id="statement" value={null} onChange={() => {}} mentions={async () => [BOB]} aria-label="Statement" />)}
    </QueryClientProvider>,
  );
}

async function offerPeople() {
  const dom = document.getElementById("statement") as (HTMLElement & { editor?: TiptapEditor }) | null;
  if (!dom?.editor) throw new Error("No editor on #statement.");
  const editor = dom.editor;
  await act(async () => {
    editor.chain().focus().insertContent("@bo").run();
  });
  return screen.findByRole("listbox", { name: "People to mention" });
}

describe("the people an at sign offers", () => {
  it("are listed in the page's main landmark, fixed under the caret", async () => {
    writingIn((editor) => <main>{editor}</main>);
    const list = await offerPeople();
    expect(list.closest("main")).not.toBeNull();
    expect(list).toHaveClass("fixed");
    expect(screen.getByRole("option", { name: /Bob Builder/ })).toBeInTheDocument();
  });

  it("are listed in the dialog the editor is in", async () => {
    writingIn((editor) => (
      <main>
        <div role="dialog" aria-label="Edit">
          {editor}
        </div>
      </main>
    ));
    const list = await offerPeople();
    expect(list.closest('[role="dialog"]')).not.toBeNull();
  });
});

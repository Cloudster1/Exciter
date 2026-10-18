import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SpecDocument } from "@/api/documents";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-print-ready");
});

// A print view has no shell; it has arrived once it shows the document or why it cannot.
const printed = "[data-print-view], [data-print-failed]";

const statement = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aDocument(overrides: Partial<SpecDocument> = {}): SpecDocument {
  return {
    kind: "baseline",
    title: "Release 1",
    projectKey: "CHK",
    projectName: "Checkout",
    sectionKey: "",
    baseline: {
      id: "0195f000-0000-7000-8000-0000000008a1",
      name: "Release 1",
      description: "For the supplier.",
      takenAt: "2026-10-15T08:00:00Z",
      takenBy: "Ada Lovelace",
      lockedAt: "2026-10-15T09:00:00Z",
      signatures: 0,
      signaturesRequired: 0,
      signed: false,
    },
    exportedAt: "2026-10-15T10:00:00Z",
    exportedBy: "Ada Lovelace",
    total: 2,
    glossary: [],
    entries: [
      {
        id: "0195f000-0000-7000-8000-0000000008b1",
        key: "CHK-R1",
        kind: "section",
        outline: "1",
        depth: 0,
        title: "Paying",
        revision: 1,
        statement: { type: "doc", content: [] },
        rationale: null,
        properties: [],
        links: [],
      },
      {
        id: "0195f000-0000-7000-8000-0000000008b2",
        key: "CHK-R2",
        kind: "requirement",
        outline: "1.1",
        depth: 1,
        title: "Pay at once",
        revision: 3,
        statement: statement("The shopper pays at once."),
        rationale: statement("Nobody waits."),
        properties: [
          { field: "priority", name: "", text: "must", document: null, flag: null },
          { field: "", name: "Safety relevant", text: "", document: null, flag: true },
          { field: "", name: "Detail", text: "", document: statement("Measured in shops."), flag: null },
        ],
        links: [{ phrase: "derives from", key: "SYS-R4", title: "Take money" }],
      },
    ],
    ...overrides,
  };
}

describe("the print view of a document", () => {
  it("draws the cover, the contents and each entry with its table and links, and says when it is ready", async () => {
    stubApi({ "GET /baselines/0195f000-0000-7000-8000-0000000008a1/document": { status: 200, body: { document: aDocument() } } });
    await renderAt("/print/baselines/0195f000-0000-7000-8000-0000000008a1", { ready: printed });
    expect(await screen.findByRole("heading", { level: 1, name: "Release 1" })).toBeInTheDocument();
    expect(screen.getByText("Baseline of Checkout (CHK)")).toBeInTheDocument();
    expect(screen.getByText(/Locked on .*2026; it never changes./)).toBeInTheDocument();
    const contents = screen.getByRole("navigation", { name: "Contents" });
    expect(within(contents).getByRole("link", { name: /1\.1\s*Pay at once/ })).toHaveAttribute("href", "#entry-CHK-R2");
    const entry = screen.getByRole("heading", { name: /1\.1\s*Pay at once/ }).closest("[data-print-entry]") as HTMLElement;
    expect(within(entry).getByText("CHK-R2 · revision 3")).toBeInTheDocument();
    expect(within(entry).getByText("Nobody waits.")).toBeInTheDocument();
    expect(within(entry).getByRole("row", { name: "Priority Must" })).toBeInTheDocument();
    expect(within(entry).getByRole("row", { name: "Safety relevant Yes" })).toBeInTheDocument();
    expect(within(entry).getByText("Measured in shops.")).toBeInTheDocument();
    expect(within(entry).getByText("SYS-R4")).toBeInTheDocument();
    expect(screen.queryByText(/This document holds the first/)).not.toBeInTheDocument();
    const meta = document.querySelector("[data-print-meta]") as HTMLElement;
    expect(meta.dataset.header).toBe("Checkout (CHK)");
    expect(meta.dataset.title).toBe("Release 1");
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-print-ready"), { timeout: 3000 });
    expect(document.documentElement).toHaveAttribute("data-theme", "light");
  });

  it("says why a document cannot be printed, for the render service to pass on", async () => {
    stubApi({
      "GET /projects/CHK/document": { status: 404, body: { error: { code: "not_found", message: "That project was not found." } } },
    });
    await renderAt("/print/p/CHK/document", { ready: printed });
    expect(await screen.findByRole("alert")).toHaveTextContent("That project was not found.");
    expect(document.querySelector("[data-print-failed]")).not.toBeNull();
  });
});

describe("exporting a specification", () => {
  const spec = {
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/tree": { status: 200, body: { nodes: [] } },
    "GET /projects/CHK/spec": { status: 200, body: { entries: [], total: 0, limit: 50, offset: 0 } },
  };

  it("saves the Word file, and shows a refused PDF's reason with a way to try again", async () => {
    const saved = vi.fn(() => "blob:document");
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: saved, revokeObjectURL: vi.fn() }));
    const sent = stubApi({
      ...spec,
      "GET /projects/CHK/document.docx": { status: 200, body: {} },
      "GET /projects/CHK/document.pdf": {
        status: 503,
        body: { error: { code: "render_unavailable", message: "PDF export is not set up on this server. Export the document as Word instead." } },
      },
    });
    await renderAt("/p/CHK/spec");
    const menu = await screen.findByRole("button", { name: `Export ${aProject().name}` });
    await userEvent.click(menu);
    await userEvent.click(screen.getByRole("menuitem", { name: "Word" }));
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(sent.some((each) => each.path === "/projects/CHK/document.docx")).toBe(true);

    await userEvent.click(menu);
    await userEvent.click(screen.getByRole("menuitem", { name: "PDF" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/PDF export is not set up on this server/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /Retry/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

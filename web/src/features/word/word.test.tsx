import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReqIFImport } from "@/api/reqif";
import { IMPORT_POLL_MS, WORD_CHOICES_DEBOUNCE_MS } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = "0195f000-0000-7000-8000-0000000009a1";

function anImport(overrides: Partial<ReqIFImport> = {}): ReqIFImport {
  return {
    id,
    projectKey: "CHK",
    fileName: "brakes.docx",
    format: "docx",
    state: "uploaded",
    choices: { _t_table1: "create" },
    preview: {
      title: "Brakes",
      sourceTool: "",
      specifications: [],
      objects: 3,
      sections: 2,
      relations: 0,
      images: 0,
      new: 3,
      updates: 1,
      schema: [{ kind: "type", identifier: "_t_table1", name: "Table 1", by: "create", count: 2, means: null, meansName: "" }],
      losses: [{ kind: "formatting", where: "Pedal", message: "Underlining is not kept; the words are." }],
      lostCount: 1,
      tree: [
        { depth: 0, kind: "section", title: "Braking", source: "Heading 1, paragraph 1", losses: 0 },
        { depth: 1, kind: "section", title: "Pedal", source: "Heading 2, paragraph 2", losses: 1 },
        {
          depth: 2,
          kind: "requirement",
          title: "The brake pedal shall return within 1 s.",
          source: "paragraph 3",
          matchedBy: "key",
          existingKey: "CHK-R4",
          losses: 0,
        },
        { depth: 2, kind: "informative", title: "This text explains the pedal.", source: "paragraph 4", losses: 0 },
        { depth: 2, kind: "skipped", title: "Table of contents", source: "paragraph 5", losses: 0 },
      ],
      styles: [
        { id: "Requirement", name: "Requirement", count: 4 },
        { id: "Note", name: "Note", count: 1 },
      ],
      tables: 1,
      problems: [{ where: "Table 1", message: "Table 1 has no header row. Take it as one requirement, or give it a header row." }],
    },
    done: 0,
    total: 3,
    imported: [],
    updated: [],
    unchanged: 0,
    refused: [],
    error: "",
    byName: "Ada Lovelace",
    createdAt: "2026-10-18T08:00:00Z",
    finishedAt: null,
    ...overrides,
  };
}

const settings = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /projects/CHK/tree": {
    status: 200,
    body: {
      nodes: [
        { id: "0195f000-0000-7000-8000-0000000002a1", key: "CHK-R1", kind: "section", title: "Vehicle", outline: "1", depth: 0 },
        { id: "0195f000-0000-7000-8000-0000000002a2", key: "CHK-R2", kind: "requirement", title: "Start", outline: "1.1", depth: 1 },
      ],
    },
  },
  "GET /projects/CHK/types": { status: 200, body: { types: [{ id: "0195f000-0000-7000-8000-0000000003a1", name: "Requirement", retired: false }] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
  "GET /attribute-kinds": { status: 200, body: { kinds: [] } },
};

describe("importing a Word document", () => {
  it("uploads it below a section, sends every choice at once, shows the tree, and starts once nothing keeps it", async () => {
    const chosen = anImport({ choices: { _t_table1: "create", "word.unit": "heading" }, preview: { ...anImport().preview, problems: [] } });
    const sent = stubApi({
      ...settings,
      "POST /projects/CHK/word-imports": { status: 201, body: { import: anImport() } },
      [`PUT /reqif-imports/${id}/choices`]: { status: 200, body: { import: chosen } },
      [`POST /reqif-imports/${id}/start`]: { status: 202, body: { import: { ...chosen, state: "queued" } } },
      [`GET /reqif-imports/${id}`]: {
        status: 200,
        body: {
          import: { ...chosen, state: "done", done: 3, imported: ["CHK-R5", "CHK-R6", "CHK-R7"], updated: ["CHK-R4"], finishedAt: "2026-10-18T08:01:00Z" },
        },
      },
    });
    // The form never reaches the stub whole in jsdom, so what goes into it is watched instead.
    const appended = vi.spyOn(FormData.prototype, "append");
    await renderAt("/p/CHK/settings?tab=word");
    await userEvent.upload(
      await screen.findByLabelText("Word document"),
      new File(["PK"], "brakes.docx", { type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }),
    );
    const parent = screen.getByRole("combobox", { name: "Import below" });
    expect(within(parent).getByRole("option", { name: "The project's top" })).toBeInTheDocument();
    expect(within(parent).queryByRole("option", { name: /Start/ })).not.toBeInTheDocument();
    await userEvent.selectOptions(parent, "1 Vehicle");
    await userEvent.click(screen.getByRole("button", { name: "Upload" }));

    expect(await screen.findByText(/brakes.docx: check what each part becomes/)).toBeInTheDocument();
    expect(sent.some((each) => each.method === "POST" && each.path === "/projects/CHK/word-imports")).toBe(true);
    expect(appended.mock.calls.map(([name, value]) => [name, value instanceof File ? value.name : value])).toEqual([
      ["file", "brakes.docx"],
      ["parentKey", "CHK-R1"],
    ]);

    const tree = screen.getByRole("list", { name: "What the document becomes" });
    const parts = [...tree.querySelectorAll("[data-word-node]")].map((each) => each.getAttribute("data-word-node"));
    expect(parts).toEqual(["section", "section", "requirement", "informative", "skipped"]);
    expect(within(tree).getByText("Braking")).toBeInTheDocument();
    expect(within(tree).getAllByText("Section")).toHaveLength(2);
    expect(within(tree).getByText("Explaining text")).toBeInTheDocument();
    expect(within(tree).getByText("updates CHK-R4 (found by key)")).toBeInTheDocument();
    expect(within(tree).getByText("1 loss")).toBeInTheDocument();
    expect(screen.getByText(/Underlining is not kept/)).toBeInTheDocument();
    expect(screen.getByLabelText("Keywords")).toHaveValue("shall, must");
    expect(screen.getByLabelText("Requirement (used 4 times)")).not.toBeChecked();
    expect(screen.getByText(/Table 1 has no header row/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start the import" })).toBeDisabled();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("radio", { name: /One per heading/ }));
    await userEvent.click(screen.getByLabelText("Requirement (used 4 times)"));
    await userEvent.type(screen.getByLabelText("Pattern"), "REQ-");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Table 1" }), "One requirement per row");
    await waitFor(
      () => {
        const puts = sent.filter((each) => each.method === "PUT");
        expect(puts.at(-1)?.body).toEqual({
          choices: {
            _t_table1: "create",
            "word.unit": "heading",
            "word.styles": "Requirement",
            "word.pattern": "REQ-",
            "word.keywords": "shall, must",
            "word.front": "section",
            "word.table.1": "rows",
          },
        });
      },
      { timeout: WORD_CHOICES_DEBOUNCE_MS * 6 },
    );
    expect(await screen.findByText("Nothing keeps the import from starting.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Start the import" })).toBeEnabled());
    await userEvent.click(screen.getByRole("button", { name: "Start the import" }));
    // The page asks again once a poll has passed, which a slow machine stretches; a few polls are waited for.
    const result = await screen.findByText("3 requirements made", {}, { timeout: IMPORT_POLL_MS * 5 });
    expect(within(result.closest("[data-reqif-step]") as HTMLElement).getByText("1 requirement changed")).toBeInTheDocument();
  });

  it("tells somebody who does not run the project whose import it is", async () => {
    stubApi({
      ...settings,
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } }) },
      },
    });
    await renderAt("/p/CHK/settings?tab=word");
    expect(await screen.findByText("Only the project's administrators import into it. Ask one of them.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Word document")).not.toBeInTheDocument();
  });

  it("says why a file was refused", async () => {
    stubApi({
      ...settings,
      "POST /projects/CHK/word-imports": {
        status: 422,
        body: {
          error: {
            code: "validation",
            message: "Check the highlighted fields.",
            fields: { file: "notes.txt is not a Word document. Save it as .docx and upload it again." },
          },
        },
      },
    });
    await renderAt("/p/CHK/settings?tab=word");
    await userEvent.upload(await screen.findByLabelText("Word document"), new File(["hello"], "notes.txt", { type: "text/plain" }), { applyAccept: false });
    await userEvent.click(screen.getByRole("button", { name: "Upload" }));
    expect(await screen.findByText("notes.txt is not a Word document. Save it as .docx and upload it again.")).toBeInTheDocument();
  });
});

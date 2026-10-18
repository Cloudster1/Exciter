import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SpecDocument } from "@/api/documents";
import type { ExportTemplate } from "@/api/templates";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-print-ready");
});

const ownId = "0195f000-0000-7000-8000-0000000009a1";
const sharedId = "0195f000-0000-7000-8000-0000000009a2";
const baselineId = "0195f000-0000-7000-8000-0000000009b1";

function aTemplate(overrides: Partial<ExportTemplate> = {}): ExportTemplate {
  return {
    id: ownId,
    projectKey: "CHK",
    name: "Supplier",
    description: "For the supplier.",
    shared: false,
    fileName: "supplier.dotx",
    size: 20480,
    fields: [{ field: "status" }],
    include: { links: true, comments: false, changeMarks: true },
    placeholders: ["content", "project", "date"],
    createdBy: { id: "0195f000-0000-7000-8000-0000000000a1", name: "Ada Lovelace" },
    createdAt: "2026-10-18T08:00:00Z",
    updatedAt: "2026-10-18T08:00:00Z",
    can: { edit: true },
    ...overrides,
  } as ExportTemplate;
}

const templates = {
  "GET /projects/CHK/export-templates": {
    status: 200,
    body: {
      templates: [
        aTemplate(),
        aTemplate({
          id: sharedId,
          projectKey: "SYS",
          name: "House style",
          shared: true,
          fields: [],
          include: { links: true, comments: true, changeMarks: false },
          can: { edit: false },
        }),
      ],
    },
  },
};

describe("the template settings", () => {
  it("uploads a template, and orders the fields and inclusions of the project's own", async () => {
    const sent = stubApi({
      ...templates,
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/attributes": {
        status: 200,
        body: { attributes: [{ id: "0195f000-0000-7000-8000-0000000009c1", name: "Cost", retired: false }] },
      },
      "POST /projects/CHK/export-templates": { status: 201, body: { template: aTemplate({ id: "0195f000-0000-7000-8000-0000000009a3", name: "Board" }) } },
      [`PATCH /export-templates/${ownId}`]: { status: 200, body: { template: aTemplate() } },
    });
    // The form never reaches the stub whole in jsdom, so what goes into it is watched instead.
    const appended = vi.spyOn(FormData.prototype, "append");
    await renderAt("/p/CHK/settings?tab=templates");
    const own = (await screen.findByText("Supplier")).closest("[data-template]") as HTMLElement;
    const shared = screen.getByText("House style").closest("[data-template]") as HTMLElement;
    expect(within(shared).getByText(/From SYS/)).toBeInTheDocument();
    expect(within(shared).queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(within(own).getByText(/Uses \{\{project\}\}, \{\{date\}\}\./)).toBeInTheDocument();

    await within(own).findByRole("option", { name: "Cost" });
    await userEvent.selectOptions(within(own).getByRole("combobox", { name: "Add a field" }), "Cost");
    await userEvent.click(within(own).getByRole("button", { name: "Add" }));
    await userEvent.click(within(own).getByRole("button", { name: "Move Cost up" }));
    await userEvent.click(within(own).getByRole("checkbox", { name: "Its open comments" }));
    await userEvent.click(within(own).getByRole("checkbox", { name: "Offer it to every project" }));
    await userEvent.click(within(own).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH")).toBe(true));
    expect(sent.find((each) => each.method === "PATCH")?.body).toMatchObject({
      fields: [{ field: "attribute", attribute: "Cost" }, { field: "status" }],
      include: { links: true, comments: true, changeMarks: true },
      shared: true,
    });

    const upload = screen.getByRole("form", { name: "New template" });
    await userEvent.upload(within(upload).getByLabelText("Word file"), new File(["PK"], "board.docx", { type: "application/octet-stream" }));
    await userEvent.type(within(upload).getByRole("textbox", { name: "Name" }), "Board");
    await userEvent.click(within(upload).getByRole("button", { name: "Upload the template" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === "/projects/CHK/export-templates")).toBe(true));
    expect(appended).toHaveBeenCalledWith("name", "Board");
    expect(await axeViolations()).toEqual([]);
  });

  it("tells somebody who does not run the project who uploads templates", async () => {
    stubApi({
      ...templates,
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } }) },
      },
    });
    await renderAt("/p/CHK/settings?tab=templates");
    expect(await screen.findByText(/Only the project's administrators upload and change its templates./)).toBeInTheDocument();
    expect(screen.queryByLabelText("Word file")).not.toBeInTheDocument();
  });
});

describe("exporting through a template", () => {
  it("asks which template and since which baseline, and sends both", async () => {
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: vi.fn(() => "blob:document"), revokeObjectURL: vi.fn() }));
    const asked: string[] = [];
    stubApi({
      ...templates,
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes: [] } },
      "GET /projects/CHK/spec": { status: 200, body: { entries: [], total: 0, limit: 50, offset: 0 } },
      "GET /projects/CHK/baselines": { status: 200, body: { baselines: [{ id: baselineId, name: "Release 1" }] } },
      "GET /projects/CHK/document.html.zip": (request) => {
        asked.push(request.url);
        return { status: 200, body: {} };
      },
    });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: `Export ${aProject().name}` }));
    await userEvent.click(screen.getByRole("menuitem", { name: "HTML site" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.selectOptions(await within(dialog).findByRole("combobox", { name: "Template" }), "Supplier");
    await userEvent.selectOptions(await within(dialog).findByRole("combobox", { name: "Mark changes since" }), "Release 1");
    expect(within(dialog).getByRole("option", { name: "House style (SYS)" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Export" }));
    await waitFor(() => expect(asked).toHaveLength(1));
    const url = new URL(asked[0]!);
    expect(url.searchParams.get("template")).toBe(ownId);
    expect(url.searchParams.get("since")).toBe(baselineId);
  });
});

describe("the print view through a template", () => {
  const statement = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

  it("marks what changed, shows open comments and leads links to the entries it holds", async () => {
    const doc: SpecDocument = {
      kind: "project",
      title: "Checkout",
      projectKey: "CHK",
      projectName: "Checkout",
      sectionKey: "",
      baseline: null,
      exportedAt: "2026-10-18T10:00:00Z",
      exportedBy: "Ada Lovelace",
      total: 2,
      glossary: [],
      changesSince: "Release 1",
      entries: [
        {
          id: "0195f000-0000-7000-8000-0000000009d1",
          key: "CHK-R1",
          kind: "requirement",
          outline: "1",
          depth: 0,
          title: "Pay",
          revision: 2,
          statement: statement("Pay in 2 s."),
          changes: {
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [
                  { type: "text", text: "Pay in " },
                  { type: "text", text: "1", marks: [{ type: "diffDelete" }] },
                  { type: "text", text: "2", marks: [{ type: "diffInsert" }] },
                  { type: "text", text: " s." },
                ],
              },
            ],
          },
          changedFrom: 1,
          rationale: null,
          properties: [{ field: "revision", name: "", text: "2", document: null, flag: null }],
          links: [
            { phrase: "derives from", key: "CHK-R2", title: "Take money" },
            { phrase: "derives from", key: "SYS-R4", title: "Elsewhere" },
          ],
          comments: [{ author: "Grace Hopper", at: "2026-10-17T09:00:00Z", text: "Is 2 s enough?" }],
        },
        {
          id: "0195f000-0000-7000-8000-0000000009d2",
          key: "CHK-R2",
          kind: "requirement",
          outline: "2",
          depth: 0,
          title: "Take money",
          revision: 1,
          statement: statement("Take it."),
          rationale: null,
          properties: [],
          links: [],
        },
      ],
    } as SpecDocument;
    const asked: string[] = [];
    stubApi({
      "GET /projects/CHK/document": (request) => {
        asked.push(request.url);
        return { status: 200, body: { document: doc } };
      },
    });
    await renderAt(`/print/p/CHK/document?template=${ownId}&since=${baselineId}`, { ready: "[data-print-view], [data-print-failed]" });
    expect(new URL(asked[0]!).searchParams.get("since")).toBe(baselineId);
    expect(screen.getByText("Changes since the baseline Release 1 are marked.")).toBeInTheDocument();
    const entry = document.querySelector('[data-print-entry="CHK-R1"]') as HTMLElement;
    expect(entry.dataset.entryDepth).toBe("0");
    expect(within(entry).getByText("Changed since revision 1:")).toBeInTheDocument();
    expect(within(entry).getByRole("row", { name: "Revision 2" })).toBeInTheDocument();
    expect(within(entry).getByRole("link", { name: /CHK-R2\s*Take money/ })).toHaveAttribute("href", "#entry-CHK-R2");
    expect(within(entry).queryByRole("link", { name: /SYS-R4/ })).not.toBeInTheDocument();
    expect(within(entry).getByText("Is 2 s enough?")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { reqifExportHref, type ReqIFImport } from "@/api/reqif";
import { IMPORT_POLL_MS } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const weight = "0195f000-0000-7000-8000-0000000004a1";

function anImport(overrides: Partial<ReqIFImport> = {}): ReqIFImport {
  return {
    id: "0195f000-0000-7000-8000-0000000007a1",
    projectKey: "CHK",
    fileName: "supplier.reqif",
    format: "reqif",
    state: "uploaded",
    choices: {},
    preview: {
      title: "Brake controller",
      sourceTool: "Supplier Tool 7.2",
      specifications: ["Brake controller SRS"],
      objects: 3,
      sections: 2,
      relations: 1,
      images: 0,
      new: 4,
      updates: 0,
      schema: [
        { kind: "type", identifier: "_t_heading", name: "Heading", by: "section", count: 1, means: null, meansName: "" },
        { kind: "type", identifier: "_t_req", name: "Software Requirement", by: "create", count: 2, means: null, meansName: "" },
        { kind: "attribute", identifier: "_a_text", name: "ReqIF.Text", by: "field", field: "statement", count: 2, means: null, meansName: "" },
        { kind: "attribute", identifier: "_a_weight", name: "Weight", by: "none", fits: ["number"], count: 1, means: null, meansName: "" },
      ],
      losses: [{ kind: "formatting", where: "Warn the driver", message: "Subscript and superscript are not kept; the words are." }],
      lostCount: 1,
      problems: [
        {
          where: "Weight",
          message:
            "The file's Weight holds real values, and the project's Weight is a text attribute. Map it to another attribute, have the import make one, or leave it out.",
        },
      ],
    },
    done: 0,
    total: 3,
    imported: [],
    updated: [],
    unchanged: 0,
    refused: [],
    error: "",
    byName: "Ada Lovelace",
    createdAt: "2026-10-15T08:00:00Z",
    finishedAt: null,
    ...overrides,
  };
}

const settings = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /projects/CHK/types": { status: 200, body: { types: [{ id: "0195f000-0000-7000-8000-0000000003a1", name: "Requirement", retired: false }] } },
  "GET /projects/CHK/attributes": {
    status: 200,
    body: {
      attributes: [
        { id: weight, name: "Mass", kind: "number", retired: false, options: [] },
        { id: "0195f000-0000-7000-8000-0000000004a2", name: "Weight", kind: "text", retired: false, options: [] },
      ],
    },
  },
  "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
  "GET /attribute-kinds": {
    status: 200,
    body: {
      kinds: [
        { kind: "number", title: "Number" },
        { kind: "text", title: "Text" },
      ],
    },
  },
};

describe("the ReqIF exports", () => {
  it("download the project, or a section and what is below it", () => {
    expect(reqifExportHref("CHK", false)).toBe("/api/v1/projects/CHK/export.reqif");
    expect(reqifExportHref("CHK", true, "CHK-R4")).toBe("/api/v1/projects/CHK/export.reqifz?section=CHK-R4");
  });
});

describe("importing a ReqIF file", () => {
  it("uploads it, maps an attribute that did not fit, starts it, and shows what the worker did", async () => {
    const chosen = anImport({
      choices: { _a_weight: weight },
      preview: {
        ...anImport().preview,
        schema: anImport().preview.schema.map((item) => (item.identifier === "_a_weight" ? { ...item, by: "choice", means: weight, meansName: "Mass" } : item)),
        problems: [],
      },
    });
    let reads = 0;
    const sent = stubApi({
      ...settings,
      "POST /projects/CHK/reqif-imports": { status: 201, body: { import: anImport() } },
      [`PUT /reqif-imports/${anImport().id}/choices`]: { status: 200, body: { import: chosen } },
      [`POST /reqif-imports/${anImport().id}/start`]: { status: 202, body: { import: { ...chosen, state: "queued" } } },
      [`GET /reqif-imports/${anImport().id}`]: () => {
        reads += 1;
        return {
          status: 200,
          body: { import: { ...chosen, state: "done", done: 3, imported: ["CHK-R1", "CHK-R2", "CHK-R3", "CHK-R4"], finishedAt: "2026-10-15T08:01:00Z" } },
        };
      },
    });
    await renderAt("/p/CHK/settings?tab=reqif");
    expect(await screen.findByRole("link", { name: /Download .reqifz/ })).toHaveAttribute("href", "/api/v1/projects/CHK/export.reqifz");
    await userEvent.upload(await screen.findByLabelText("ReqIF file"), new File(["<REQ-IF/>"], "supplier.reqif", { type: "application/xml" }));
    await userEvent.click(screen.getByRole("button", { name: "Upload" }));
    expect(await screen.findByText(/supplier.reqif: 3 objects from Supplier Tool 7.2/)).toBeInTheDocument();
    expect(screen.getByText("It makes 4 requirements and changes 0 requirements.")).toBeInTheDocument();
    expect(screen.getByText("Fills the statement")).toBeInTheDocument();
    expect(screen.getByText("Becomes sections")).toBeInTheDocument();
    expect(screen.getByText(/Subscript and superscript are not kept/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start the import" })).toBeDisabled();
    expect(await axeViolations()).toEqual([]);

    const chooser = screen.getByRole("combobox", { name: "What Weight becomes" });
    expect(within(chooser).queryByRole("option", { name: /^Weight/ })).not.toBeInTheDocument();
    await userEvent.selectOptions(chooser, "Mass (Number)");
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ choices: { _a_weight: weight } }));
    expect(await screen.findByText("Nothing keeps the import from starting.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Start the import" }));
    // The page asks again once a poll has passed, which a slow machine stretches; a few polls are waited for.
    const result = await screen.findByText("4 requirements made", {}, { timeout: IMPORT_POLL_MS * 5 });
    expect(result.closest("[data-reqif-step]")).toHaveAttribute("data-reqif-step", "done");
    expect(reads).toBeGreaterThan(0);
    expect(within(result.closest("[data-reqif-step]") as HTMLElement).getByText("3 of 3 objects")).toBeInTheDocument();
  });

  it("lets somebody who does not run the project export, and tells them whose import it is", async () => {
    stubApi({
      ...settings,
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } }) },
      },
    });
    await renderAt("/p/CHK/settings?tab=reqif");
    expect(await screen.findByText("Only the project's administrators import into it. Ask one of them.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Download .reqif$/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("ReqIF file")).not.toBeInTheDocument();
  });
});

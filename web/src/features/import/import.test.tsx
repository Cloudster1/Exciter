import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { exportHref, type Import } from "@/api/imports";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function anImport(overrides: Partial<Import> = {}): Import {
  return {
    id: "0195f000-0000-7000-8000-0000000006a1",
    projectKey: "CHK",
    fileName: "spec.csv",
    state: "uploaded",
    mapping: { title: [0], type: [1] },
    targets: ["key", "title", "type", "status", "owner", "attr:Risk"],
    preview: {
      columns: [
        { at: 0, name: "Title", samples: ["Pay"] },
        { at: 1, name: "Type", samples: ["Feature"] },
      ],
      rows: 1,
      words: [{ target: "type", value: "Feature", count: 1, means: "" }],
      people: [],
      problems: [{ row: 0, column: "Type", message: 'The type "Feature" matches nothing in CHK. Map it to one the project has, or change the file.' }],
    },
    done: 0,
    total: 1,
    imported: [],
    updated: [],
    refused: [],
    error: "",
    byName: "Ada Lovelace",
    createdAt: "2026-10-13T08:00:00Z",
    finishedAt: null,
    ...overrides,
  };
}

const settings = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /projects/CHK/types": { status: 200, body: { types: [{ id: "0195f000-0000-7000-8000-0000000003a1", name: "Requirement", retired: false }] } },
  "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
};

describe("the export of a grid", () => {
  it("carries the grid's narrowing and columns", () => {
    expect(exportHref("CHK", { q: "pay", status: ["a", "b"], desc: true }, ["key", "title"])).toBe(
      "/api/v1/projects/CHK/export.csv?q=pay&status=a&status=b&desc=true&column=key&column=title",
    );
    expect(exportHref("CHK", {}, [])).toBe("/api/v1/projects/CHK/export.csv");
  });
});

describe("importing a file", () => {
  it("uploads it, maps a word nobody knew, starts it, and shows what the worker did", async () => {
    const answered = anImport({
      preview: { ...anImport().preview, words: [{ target: "type", value: "Feature", count: 1, means: "Requirement" }], problems: [] },
    });
    let reads = 0;
    const sent = stubApi({
      ...settings,
      "POST /projects/CHK/import": { status: 201, body: { import: anImport() } },
      [`PUT /imports/${anImport().id}/mapping`]: { status: 200, body: { import: answered } },
      [`POST /imports/${anImport().id}/start`]: { status: 202, body: { import: { ...answered, state: "queued" } } },
      [`GET /imports/${anImport().id}`]: () => {
        reads += 1;
        return { status: 200, body: { import: { ...answered, state: "done", done: 1, imported: ["CHK-R1"], finishedAt: "2026-10-13T08:01:00Z" } } };
      },
    });
    await renderAt("/p/CHK/settings?tab=import");
    await userEvent.upload(await screen.findByLabelText("CSV file"), new File(["Title,Type\nPay,Feature\n"], "spec.csv", { type: "text/csv" }));
    await userEvent.click(screen.getByRole("button", { name: "Upload" }));
    expect(await screen.findByText(/spec.csv: 1 row/)).toBeInTheDocument();
    expect(screen.getByText(/matches nothing in CHK/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start the import" })).toBeDisabled();
    expect(await axeViolations()).toEqual([]);

    await userEvent.selectOptions(screen.getByRole("combobox", { name: "What Feature means here" }), "Requirement");
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
        mapping: { title: [0], type: [1] },
        values: { type: { Feature: "Requirement" } },
        people: {},
      }),
    );
    expect(await screen.findByText("Nothing keeps the import from starting.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Start the import" }));
    const result = await screen.findByText("1 requirement made");
    expect(result.closest("[data-import-step]")).toHaveAttribute("data-import-step", "done");
    expect(reads).toBeGreaterThan(0);
    expect(within(result.closest("[data-import-step]") as HTMLElement).getByText("1 of 1 rows")).toBeInTheDocument();
  });

  it("tells somebody who does not run the project whose import it is", async () => {
    stubApi({
      ...settings,
      "GET /projects/CHK": {
        status: 200,
        body: { project: aProject({ can: { view: true, comment: true, edit: true, approve: false, administer: false, purge: false } }) },
      },
    });
    await renderAt("/p/CHK/settings?tab=import");
    expect(await screen.findByText("Only the project's administrators import into it. Ask one of them.")).toBeInTheDocument();
  });
});

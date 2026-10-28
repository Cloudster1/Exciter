import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Import } from "@/api/imports";
import { IMPORT_MAX_BYTES, IMPORT_POLL_MS } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = "0195f000-0000-7000-8000-0000000006b1";

function anImport(overrides: Partial<Import> = {}): Import {
  return {
    id,
    projectKey: "CHK",
    fileName: "long.csv",
    state: "queued",
    mapping: { title: [0] },
    targets: ["key", "title"],
    preview: { columns: [{ at: 0, name: "Title", samples: ["Pay"] }], rows: 300, words: [], people: [], problems: [] },
    done: 0,
    total: 300,
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
  "GET /projects/CHK/types": { status: 200, body: { types: [] } },
  "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
};

/** A file of the size given, without holding its bytes. */
function fileOf(size: number): File {
  const file = new File(["title\nPay\n"], "long.csv", { type: "text/csv" });
  Object.defineProperty(file, "size", { value: size });
  return file;
}

async function uploaded() {
  await renderAt("/p/CHK/settings?tab=import");
  await userEvent.upload(await screen.findByLabelText("CSV file"), fileOf(1));
  await userEvent.click(screen.getByRole("button", { name: "Upload" }));
}

describe("a CSV import past the happy path", () => {
  it("does not upload a file over the limit", async () => {
    stubApi(settings);
    await renderAt("/p/CHK/settings?tab=import");
    const input = await screen.findByLabelText("CSV file");
    await userEvent.upload(input, fileOf(IMPORT_MAX_BYTES + 1));
    expect(screen.getByRole("button", { name: "Upload" })).toBeDisabled();
    await userEvent.upload(input, fileOf(IMPORT_MAX_BYTES));
    expect(screen.getByRole("button", { name: "Upload" })).toBeEnabled();
  });

  it("offers Stop while the import is queued or running, and another file once it has stopped", async () => {
    let state: Import["state"] = "queued";
    const sent = stubApi({
      ...settings,
      "POST /projects/CHK/import": { status: 201, body: { import: anImport() } },
      [`GET /imports/${id}`]: () => ({ status: 200, body: { import: anImport({ state, done: state === "queued" ? 0 : 40 }) } }),
      [`DELETE /imports/${id}`]: () => {
        state = "cancelled";
        return { status: 204 };
      },
    });
    await uploaded();
    expect(await screen.findByText("Waiting for the worker")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Import another file" })).not.toBeInTheDocument();

    state = "running";
    expect(await screen.findByText("Importing", {}, { timeout: IMPORT_POLL_MS * 5 })).toBeInTheDocument();
    expect(screen.getByText("40 of 300 rows")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(await screen.findByText("Stopped")).toBeInTheDocument();
    expect(sent.filter((each) => each.method === "DELETE")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Import another file" }));
    expect(await screen.findByLabelText("CSV file")).toBeInTheDocument();
  });

  it("waits for an answer about a person nobody is, which Nobody gives", async () => {
    const asking = anImport({
      state: "uploaded",
      mapping: { title: [0], owner: [1] },
      preview: {
        ...anImport().preview,
        rows: 1,
        people: [{ name: "nobody@exciter.test", count: 1, member: null, email: "" }],
        problems: [{ row: 2, column: "Owner", message: "nobody@exciter.test is no member of CHK. Say who it is, or Nobody." }],
      },
    });
    const sent = stubApi({
      ...settings,
      "GET /people": { status: 200, body: { people: [] } },
      "POST /projects/CHK/import": { status: 201, body: { import: asking } },
      [`GET /imports/${id}`]: { status: 200, body: { import: asking } },
      [`PUT /imports/${id}/mapping`]: { status: 200, body: { import: { ...asking, preview: { ...asking.preview, problems: [] } } } },
    });
    await uploaded();
    const who = await screen.findByRole("combobox", { name: "Who nobody@exciter.test is" });
    expect(who).toHaveDisplayValue("Pick who it is");
    await userEvent.selectOptions(who, "Nobody");
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
        mapping: { title: [0], owner: [1] },
        values: {},
        people: { "nobody@exciter.test": {} },
      }),
    );
    expect(await screen.findByText("Nothing keeps the import from starting.")).toBeInTheDocument();
    expect(who).toHaveDisplayValue("Nobody");
    expect(screen.queryByRole("option", { name: "Pick who it is" })).not.toBeInTheDocument();
  });

  it("says why a stop failed", async () => {
    stubApi({
      ...settings,
      "POST /projects/CHK/import": { status: 201, body: { import: anImport({ state: "running" }) } },
      [`GET /imports/${id}`]: { status: 200, body: { import: anImport({ state: "running" }) } },
      [`DELETE /imports/${id}`]: { status: 503, body: { error: { code: "unavailable", message: "The server is busy. Try again in a minute." } } },
    });
    await uploaded();
    await userEvent.click(await screen.findByRole("button", { name: "Stop" }));
    expect(await screen.findByText("The server is busy. Try again in a minute.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Importing")).toBeInTheDocument());
  });
});

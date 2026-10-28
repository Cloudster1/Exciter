import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReqIFImport } from "@/api/reqif";
import { IMPORT_POLL_MS, REQIF_MAX_BYTES } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = "0195f000-0000-7000-8000-0000000007b1";

function anImport(overrides: Partial<ReqIFImport> = {}): ReqIFImport {
  return {
    id,
    projectKey: "CHK",
    fileName: "many.reqif",
    format: "reqif",
    state: "queued",
    choices: {},
    preview: {
      title: "Many",
      sourceTool: "",
      specifications: ["Many"],
      objects: 300,
      sections: 0,
      relations: 0,
      images: 0,
      new: 300,
      updates: 0,
      schema: [],
      losses: [],
      lostCount: 0,
      tree: [],
      styles: [],
      tables: 0,
      problems: [],
    },
    done: 0,
    total: 300,
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
  "GET /projects/CHK/types": { status: 200, body: { types: [] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
};

/** A file of the size given, without holding its bytes. */
function fileOf(size: number): File {
  const file = new File(["<REQ-IF/>"], "many.reqif", { type: "application/xml" });
  Object.defineProperty(file, "size", { value: size });
  return file;
}

async function uploaded() {
  await renderAt("/p/CHK/settings?tab=reqif");
  await userEvent.upload(await screen.findByLabelText("ReqIF file"), fileOf(1));
  await userEvent.click(screen.getByRole("button", { name: "Upload" }));
}

describe("a ReqIF import past the happy path", () => {
  it("does not upload a file over the limit", async () => {
    stubApi(settings);
    await renderAt("/p/CHK/settings?tab=reqif");
    const input = await screen.findByLabelText("ReqIF file");
    await userEvent.upload(input, fileOf(REQIF_MAX_BYTES + 1));
    expect(screen.getByRole("button", { name: "Upload" })).toBeDisabled();
    await userEvent.upload(input, fileOf(REQIF_MAX_BYTES));
    expect(screen.getByRole("button", { name: "Upload" })).toBeEnabled();
  });

  it("offers Stop while the import is queued or running, and another file once it has stopped", async () => {
    let state: ReqIFImport["state"] = "queued";
    const sent = stubApi({
      ...settings,
      "POST /projects/CHK/reqif-imports": { status: 201, body: { import: anImport() } },
      [`GET /reqif-imports/${id}`]: () => ({ status: 200, body: { import: anImport({ state, done: state === "queued" ? 0 : 40 }) } }),
      [`DELETE /reqif-imports/${id}`]: () => {
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
    expect(screen.getByText("40 of 300 objects")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(await screen.findByText("Stopped")).toBeInTheDocument();
    expect(sent.filter((each) => each.method === "DELETE")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Import another file" }));
    expect(await screen.findByLabelText("ReqIF file")).toBeInTheDocument();
  });

  it("says why a stop failed", async () => {
    stubApi({
      ...settings,
      "POST /projects/CHK/reqif-imports": { status: 201, body: { import: anImport({ state: "running" }) } },
      [`GET /reqif-imports/${id}`]: { status: 200, body: { import: anImport({ state: "running" }) } },
      [`DELETE /reqif-imports/${id}`]: { status: 503, body: { error: { code: "unavailable", message: "The server is busy. Try again in a minute." } } },
    });
    await uploaded();
    await userEvent.click(await screen.findByRole("button", { name: "Stop" }));
    expect(await screen.findByText("The server is busy. Try again in a minute.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Importing")).toBeInTheDocument());
  });
});

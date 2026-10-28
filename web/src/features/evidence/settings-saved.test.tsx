import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

// Both evidence cards start their form again from what the server saved, and still say that it saved (#323).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const SAVED_AT = "2026-10-27T10:00:00Z";

/** The trace settings of CHK, whose counting branch and tag words read back what was last put. */
function stubTraceSettings() {
  let settings = { countingBranch: "main", updatedAt: null as string | null };
  let words = { implements: ["implements"], verifies: ["verifies"], projectKeys: ["CHK"], updatedAt: null as string | null };
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
    "GET /projects/CHK/trace-model": { status: 200, body: { model: { levels: [], updatedAt: null, updatedBy: null, can: { edit: true } } } },
    "GET /projects/CHK/evidence-settings": () => ({ status: 200, body: { settings } }),
    "PUT /projects/CHK/evidence-settings": async (request) => {
      settings = { ...((await request.json()) as { countingBranch: string }), updatedAt: SAVED_AT };
      return { status: 200, body: { settings } };
    },
    "GET /projects/CHK/code-tag-words": () => ({ status: 200, body: { words } }),
    "PUT /projects/CHK/code-tag-words": async (request) => {
      words = { ...words, ...((await request.json()) as { implements: string[]; verifies: string[] }), updatedAt: SAVED_AT };
      return { status: 200, body: { words } };
    },
  });
}

describe("the evidence settings once saved", () => {
  it("show the new counting branch and say it was saved", async () => {
    stubTraceSettings();
    await renderAt("/p/CHK/settings?tab=trace");
    const branch = await screen.findByRole("textbox", { name: "Counting branch" });
    await userEvent.clear(branch);
    await userEvent.type(branch, "release");
    const save = () => within(screen.getByRole("form", { name: "Evidence" })).getByRole("button", { name: "Save" });
    await userEvent.click(save());
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Counting branch" })).toHaveValue("release");
    expect(save()).toBeDisabled();
  });

  it("show the new tag words and say they were saved", async () => {
    stubTraceSettings();
    await renderAt("/p/CHK/settings?tab=trace");
    const implementing = await screen.findByRole("textbox", { name: "Words for implements" });
    await userEvent.type(implementing, ", satisfies");
    await userEvent.click(within(screen.getByRole("form", { name: "Code tag words" })).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Words for implements" })).toHaveValue("implements, satisfies");
  });
});

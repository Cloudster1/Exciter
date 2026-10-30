import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TraceModel } from "@/api/trace";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { derives, verifies } from "@/test/links";
import { aProject } from "@/test/projects";

// The trace model card's levels moved, removed and of issues (#315).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const NEED = "0195f000-0000-7000-8000-0000000003b1";
const SYSTEM = "0195f000-0000-7000-8000-0000000003b2";
const type = (id: string, name: string) => ({ id, name, icon: "test", colour: "#2255aa", retired: false });
const unset: TraceModel = { levels: [], updatedAt: null, updatedBy: null, can: { edit: true } };
const model: TraceModel = {
  levels: [
    { name: "Need", kind: "requirements", types: [{ id: NEED, name: "Need", icon: "test", colour: "#2255aa" }], steps: [] },
    {
      name: "System",
      kind: "requirements",
      types: [{ id: SYSTEM, name: "System", icon: "test", colour: "#2255aa" }],
      steps: [{ typeId: derives.id, typeName: "Derives", direction: "incoming" }],
    },
  ],
  updatedAt: "2026-10-16T08:00:00Z",
  updatedBy: { id: "0195f000-0000-7000-8000-0000000000aa", name: "Ada Lovelace" },
  can: { edit: true },
};

function stubCard(answer: TraceModel, more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [type(NEED, "Need"), type(SYSTEM, "System")] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, verifies] } },
    "GET /projects/CHK/trace-model": { status: 200, body: { model: answer } },
    ...more,
  });
}

const button = (name: string) => screen.getByRole("button", { name });

describe("the trace model's levels", () => {
  it("move up and down within the model, the first never up and the last never down", async () => {
    stubCard(unset);
    await renderAt("/projects/CHK/settings/trace");
    for (let i = 0; i < 3; i++) await userEvent.click(await screen.findByRole("button", { name: "Add a level" }));
    expect(button("Move Level 1 up")).toBeDisabled();
    expect(button("Move Level 1 down")).toBeEnabled();
    expect(button("Move Level 2 up")).toBeEnabled();
    expect(button("Move Level 2 down")).toBeEnabled();
    expect(button("Move Level 3 down")).toBeDisabled();
    expect(button("Add a level")).toBeEnabled();
  });

  it("removed, renumber the levels after it", async () => {
    stubCard(unset);
    await renderAt("/projects/CHK/settings/trace");
    for (let i = 0; i < 3; i++) await userEvent.click(await screen.findByRole("button", { name: "Add a level" }));
    await userEvent.type(screen.getAllByLabelText("Name")[0]!, "Need");
    await userEvent.click(button("Remove Level 2"));
    expect(screen.getAllByLabelText("Name")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Remove Level 3" })).toBeNull();
    expect(button("Remove Level 2")).toBeInTheDocument();
    expect(button("Move Level 2 down")).toBeDisabled();
    expect(button("Move Need down")).toBeEnabled();
  });

  it("moved, are saved in their new order, the new first one reached by nothing", async () => {
    const sent = stubCard(model, { "PUT /projects/CHK/trace-model": { status: 200, body: { model } } });
    await renderAt("/projects/CHK/settings/trace");
    await userEvent.click(await screen.findByRole("button", { name: "Move System up" }));
    expect(screen.getAllByLabelText("Name").map((field) => (field as HTMLInputElement).value)).toEqual(["System", "Need"]);
    expect(button("Move System up")).toBeDisabled();
    await userEvent.click(button("Save the model"));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
        levels: [
          { name: "System", kind: "requirements", typeIds: [SYSTEM], steps: [] },
          { name: "Need", kind: "requirements", typeIds: [NEED], steps: [] },
        ],
      }),
    );
  });

  it("of issues are reached by relations rather than link types, and hold no types", async () => {
    const sent = stubCard(model, { "PUT /projects/CHK/trace-model": { status: 200, body: { model } } });
    await renderAt("/projects/CHK/settings/trace");
    await userEvent.click(await screen.findByRole("button", { name: "Add a level" }));
    await userEvent.type(screen.getAllByLabelText("Name")[2]!, "Work");
    await userEvent.selectOptions(screen.getAllByLabelText("Holds")[2]!, "Armature issues");
    const steps = screen.getByRole("group", { name: "Reached from the level before by: Work" });
    expect(
      within(steps)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Implemented by", "Verified by", "Related to"]);
    expect(screen.queryByRole("group", { name: "Types: Work" })).toBeNull();
    expect(
      within(screen.getByRole("group", { name: "Reached from the level before by: System" })).getByRole("button", { name: "Is derived into" }),
    ).toBeInTheDocument();
    await userEvent.click(within(steps).getByRole("button", { name: "Verified by" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(button("Save the model"));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PUT")?.body).toMatchObject({
        levels: [{ name: "Need" }, { name: "System" }, { name: "Work", kind: "issues", typeIds: [], steps: [{ relation: "verifies" }] }],
      }),
    );
  });
});

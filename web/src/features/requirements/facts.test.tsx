import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => vi.unstubAllGlobals());

// The owner and the priority beside what a requirement says (#309): changed at once where they may change, and
// read only in the trash and in an archived project.

const OWNER_REFUSED = "Pick an owner who may view this project.";
const owned = aRequirement({ owner: { id: "0195f000-0000-7000-8000-00000000000b", name: "Bob Builder" }, priority: "must" });

function facts() {
  return document.querySelector<HTMLElement>("[data-requirement-facts]")!;
}

describe("a requirement's facts", () => {
  it("removes the owner by sending none", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: owned } },
      "PATCH /requirements/CHK-R1": { status: 200, body: { requirement: { ...owned, owner: null } } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await screen.findByText("Bob Builder");
    await userEvent.click(within(facts()).getByRole("button", { name: "Remove the owner" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PATCH").map((r) => r.body)).toEqual([{ ownerId: null }]));
  });

  it("shows the server's refusal of an owner under the owner", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: owned } },
      "PATCH /requirements/CHK-R1": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { ownerId: OWNER_REFUSED } } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await screen.findByText("Bob Builder");
    await userEvent.click(within(facts()).getByRole("button", { name: "Remove the owner" }));
    const refusal = await within(facts()).findByText(OWNER_REFUSED);
    expect(refusal.parentElement).toContainElement(within(facts()).getByText("Bob Builder"));
    expect(within(facts()).queryByText("Some fields need attention.")).toBeNull();
  });

  it.each([
    ["in the trash", aProject(), { ...owned, trashedAt: "2026-10-12T08:00:00Z" }],
    ["in an archived project", aProject({ archivedAt: "2026-10-12T08:00:00Z" }), owned],
  ])("offers no owner to pick and no priority to change %s", async (_, project, requirement) => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await screen.findByText("Bob Builder");
    expect(within(facts()).queryByRole("combobox", { name: "Change the owner" })).toBeNull();
    expect(within(facts()).queryByRole("button", { name: "Remove the owner" })).toBeNull();
    expect(within(facts()).getByRole("combobox", { name: "Priority" })).toBeDisabled();
  });
});

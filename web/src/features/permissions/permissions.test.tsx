import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AccessReport, Grant } from "@/api/permissions";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const checkout = aProject();
const ada = signedIn.user;
const paul = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Paul Plain", email: "paul@exciter.test" };
const saved: Grant[] = [
  { subject: { type: "everyone", id: null, name: "Everyone" }, role: "editor" },
  { subject: { type: "user", id: ada.id, name: ada.name }, role: "admin" },
];

describe("a project's permissions", () => {
  it("changes a role, adds a person through the picker, and saves the whole table", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/CHK/grants": { status: 200, body: { grants: saved } },
      "GET /people": { status: 200, body: { people: [paul] } },
      "GET /groups": { status: 200, body: { groups: [] } },
      "PUT /projects/CHK/grants": async (request) => {
        const body = (await request.json()) as { grants: { subject: { type: string; id?: string }; role: string }[] };
        const names: Record<string, string> = { [ada.id]: ada.name, [paul.id]: paul.name };
        const grants = body.grants.map((g) => ({
          subject: { ...g.subject, id: g.subject.id ?? null, name: g.subject.id ? names[g.subject.id] : "Everyone" },
          role: g.role,
        }));
        return { status: 200, body: { grants } };
      },
    });
    await renderAt("/p/CHK/permissions");
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Role of Everyone" }), "viewer");
    await userEvent.type(screen.getByRole("combobox", { name: "Add a person, a group or everyone" }), "pau");
    await userEvent.click(await screen.findByRole("option", { name: /Paul Plain/ }));
    expect(screen.getByText("Paul Plain added as viewer. Choose a role, then save.")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Role of Paul Plain" }), "reviewer");
    await userEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    expect(await screen.findByText("Permissions saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      grants: [
        { subject: { type: "everyone" }, role: "viewer" },
        { subject: { type: "user", id: ada.id }, role: "admin" },
        { subject: { type: "user", id: paul.id }, role: "reviewer" },
      ],
    });
  });

  it("offers everyone no admin role, and shows the row the server refused", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/CHK/grants": { status: 200, body: { grants: saved } },
      "PUT /projects/CHK/grants": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Check the fields.",
            fields: { "grants.1.role": "Ada Lovelace is a guest, who can be given at most editor." },
          },
        },
      },
    });
    await renderAt("/p/CHK/permissions");
    const everyone = await screen.findByRole("combobox", { name: "Role of Everyone" });
    expect(within(everyone).getByRole("option", { name: "Admin" })).toBeDisabled();
    await userEvent.selectOptions(everyone, "viewer");
    await userEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    const row = (await screen.findByText(/is a guest, who can be given at most editor/)).closest("tr");
    expect(row).toHaveAttribute("data-grant", "Ada Lovelace");
  });

  it("tells somebody who does not administer the project that its administrators decide", async () => {
    const sent = stubApi({ "GET /projects/CHK": { status: 200, body: { project: aProject({ can: { ...checkout.can, administer: false, purge: false } }) } } });
    await renderAt("/p/CHK/permissions");
    expect(await screen.findByText("Only the project's administrators see and change who may do what in it.")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/projects/CHK/grants")).toBe(false);
  });
});

describe("the access inspector", () => {
  it("shows each right of the member picked, and marks what decides a no", async () => {
    const report: AccessReport = {
      person: paul,
      role: "guest",
      roleSource: "manual",
      rights: [
        {
          right: "view",
          allowed: false,
          steps: [
            { kind: "guest", passed: false, grants: [] },
            { kind: "grant", passed: false, grants: [] },
          ],
        },
      ],
    };
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/CHK/grants": { status: 200, body: { grants: saved } },
      "GET /people": { status: 200, body: { people: [paul] } },
      [`GET /projects/CHK/access/${paul.id}`]: { status: 200, body: { access: report } },
    });
    await renderAt("/p/CHK/permissions");
    await userEvent.click(await screen.findByRole("button", { name: "Check somebody's access" }));
    const dialog = screen.getByRole("dialog", { name: "Access to Checkout" });
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Member" }), "pau");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Paul Plain/ }));
    const view = await within(dialog).findByText("View");
    const card = view.closest("li")!;
    expect(card).toHaveAttribute("data-allowed", "false");
    const deciding = card.querySelector("[data-decides]");
    expect(deciding).toHaveAttribute("data-access-step", "guest");
    expect(deciding).toHaveTextContent("Is a guest, so everyone's grant does not reach them.");
  });
});

describe("the sidebar inside a project", () => {
  it("links the permissions for the project's administrators only", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    await renderAt("/p/CHK/settings");
    const nav = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-project-nav="CHK"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(nav).getByRole("link", { name: "Permissions" })).toHaveAttribute("href", "/p/CHK/permissions");
  });
});

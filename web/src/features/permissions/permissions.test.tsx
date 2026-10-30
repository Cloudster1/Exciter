import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment } from "@/api/access";
import type { AccessReport } from "@/api/permissions";
import { accessAnswers, accessIds, grantPage, roleList } from "@/test/access";
import { answerConfirm, arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const checkout = aProject();
const ada = signedIn.user;
const paul = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Paul Plain", email: "paul@exciter.test" };
const inProject = (grant: Omit<Assignment, "projectId" | "projectKey" | "projectName" | "createdAt">): Assignment => ({
  ...grant,
  projectId: checkout.id,
  projectKey: checkout.key,
  projectName: checkout.name,
  createdAt: "2026-10-01T08:00:00Z",
});
const saved: Assignment[] = [
  inProject({ id: "p-1", role: "editor", roleName: "Editor", groupId: accessIds.everyone, groupName: "Everyone", groupBuiltin: "everyone" }),
  inProject({ id: "p-2", role: "admin", roleName: "Project administrator", userId: ada.id, userName: ada.name }),
];

/** The project's grants as the API keeps them, changed by what the page sends. */
function projectApi(extra: Parameters<typeof stubApi>[0] = {}) {
  let grants = saved;
  return stubApi({
    ...accessAnswers,
    "GET /projects/CHK": { status: 200, body: { project: checkout } },
    "GET /people": { status: 200, body: { people: [paul] } },
    "GET /role-assignments": () => ({ status: 200, body: grantPage(grants) }),
    "POST /role-assignments": async (request) => {
      const body = (await request.json()) as { role: string; userId: string | null };
      const made = inProject({ id: "p-3", role: body.role, roleName: roleList.find((r) => r.role === body.role)!.name, userId: paul.id, userName: paul.name });
      grants = [...grants, made];
      return { status: 201, body: { assignment: made } };
    },
    "DELETE /role-assignments/p-1": () => {
      grants = grants.filter((grant) => grant.id !== "p-1");
      return { status: 204 };
    },
    ...extra,
  });
}

describe("a project's permissions", () => {
  it("lists who holds which role in the project, Everyone in the reader's words, and offers no role of the installation alone", async () => {
    const sent = projectApi();
    await renderAt("/projects/CHK/settings/permissions");
    const table = await screen.findByRole("table", { name: "Who holds which role" });
    expect(within(table).getByText("Everyone").closest("tr")).toHaveTextContent("Editor");
    expect(within(table).getByText("Ada Lovelace").closest("tr")).toHaveTextContent("Project administrator");
    expect(sent.find((r) => r.path === "/role-assignments")).toBeDefined();
    const role = screen.getByRole("combobox", { name: "Role" });
    expect(within(role).queryByRole("option", { name: "Administrator" })).toBeNull();
    expect(within(role).getByRole("option", { name: "Auditor" })).toBeInTheDocument();
    expect(document.querySelector('[data-project-role="reviewer"]')).toHaveTextContent("Approve what is up for review");
    expect(await axeViolations()).toEqual([]);
  });

  it("grants a role to a person one at a time", async () => {
    const sent = projectApi();
    await renderAt("/projects/CHK/settings/permissions");
    const form = await waitFor(() => {
      const found = document.querySelector<HTMLElement>("[data-grant-form]");
      expect(found).not.toBeNull();
      return found!;
    });
    await userEvent.type(within(form).getByRole("combobox", { name: "Person or group to grant a role to" }), "pau");
    await userEvent.click(await within(form).findByRole("option", { name: /Paul Plain/ }));
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "reviewer");
    await userEvent.click(within(form).getByRole("button", { name: "Grant" }));
    expect(await screen.findByText("Paul Plain holds Reviewer in this project now.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "reviewer", projectKey: "CHK", userId: paul.id, groupId: null });
    expect(await within(screen.getByRole("table", { name: "Who holds which role" })).findByText("Paul Plain")).toBeInTheDocument();
  });

  it("takes a grant away once asked", async () => {
    const sent = projectApi();
    await renderAt("/projects/CHK/settings/permissions");
    await userEvent.click(await screen.findByRole("button", { name: "Revoke Editor from Everyone" }));
    await answerConfirm(true);
    expect(await screen.findByText("Everyone no longer holds Editor in this project.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/role-assignments/p-1")).toBe(true);
    await waitFor(() => expect(screen.queryByRole("button", { name: "Revoke Editor from Everyone" })).toBeNull());
  });

  it("shows the server's refusal to take the last grant that runs the project", async () => {
    const refusal = "Somebody has to administer the project. Grant Project administrator to somebody else first.";
    projectApi({ "DELETE /role-assignments/p-1": { status: 409, body: { error: { code: "no_administrator", message: refusal } } } });
    await renderAt("/projects/CHK/settings/permissions");
    await userEvent.click(await screen.findByRole("button", { name: "Revoke Editor from Everyone" }));
    await answerConfirm(true);
    expect(await screen.findByText(refusal)).toBeInTheDocument();
  });

  it("shows a role by the name it was given", async () => {
    projectApi({
      "GET /roles": { status: 200, body: { roles: roleList.map((role) => (role.role === "editor" ? { ...role, name: "Author" } : role)) } },
      "GET /role-assignments": { status: 200, body: grantPage([{ ...saved[0]!, roleName: "Author" }]) },
    });
    await renderAt("/projects/CHK/settings/permissions");
    expect(await screen.findByRole("button", { name: "Revoke Author from Everyone" })).toBeInTheDocument();
    expect(within(screen.getByRole("combobox", { name: "Role" })).getByRole("option", { name: "Author" })).toBeInTheDocument();
    expect(document.querySelector('[data-project-role="editor"] dt')).toHaveTextContent("Author");
  });

  it("changes nothing in an archived project", async () => {
    projectApi({ "GET /projects/CHK": { status: 200, body: { project: aProject({ archivedAt: "2026-10-02T08:00:00Z" }) } } });
    await renderAt("/projects/CHK/settings/permissions");
    expect(await screen.findByText(/its grants stay as they are/)).toBeInTheDocument();
    await screen.findByRole("table", { name: "Who holds which role" });
    expect(document.querySelector("[data-grant-form]")).toBeNull();
    expect(screen.queryByRole("button", { name: /^Revoke/ })).toBeNull();
  });

  it("tells somebody who does not administer the project that its administrators decide", async () => {
    const sent = stubApi({ "GET /projects/CHK": { status: 200, body: { project: aProject({ can: { ...checkout.can, administer: false, purge: false } }) } } });
    await renderAt("/projects/CHK/settings/permissions");
    const refused = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-settings-refused="permissions"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(refused).toHaveTextContent("Only the project's administrators change its settings");
    expect(refused).toHaveTextContent("Ask one of them, or ask for the role.");
    expect(screen.queryByRole("tab", { name: "Permissions" })).toBeNull();
    expect(sent.some((r) => r.path === "/role-assignments")).toBe(false);
  });
});

describe("the access inspector", () => {
  it("shows each right of the member picked, and marks what decides a no", async () => {
    const report: AccessReport = {
      person: paul,
      role: "guest",
      administers: false,
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
      ...accessAnswers,
      "GET /people": { status: 200, body: { people: [paul] } },
      [`GET /projects/CHK/access/${paul.id}`]: { status: 200, body: { access: report } },
    });
    await renderAt("/projects/CHK/settings/permissions");
    await userEvent.click(await screen.findByRole("button", { name: "Check somebody's access" }));
    const dialog = screen.getByRole("dialog", { name: "Access to Checkout" });
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Member" }), "pau");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Paul Plain/ }));
    const view = await within(dialog).findByText("View");
    const card = view.closest("li")!;
    expect(card).toHaveAttribute("data-allowed", "false");
    const deciding = card.querySelector("[data-decides]");
    expect(deciding).toHaveAttribute("data-access-step", "guest");
    expect(deciding).toHaveTextContent("Is a guest, so grants to Everyone do not reach them.");
  });
});

describe("the project's settings", () => {
  it("offer the permissions as a tab to the project's administrators", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    const router = await renderAt("/projects/CHK/settings/details");
    await userEvent.click(await screen.findByRole("tab", { name: "Permissions" }));
    await arrival(router, "/projects/CHK/settings/permissions");
  });

  it("are where the old permissions address leads", async () => {
    projectApi();
    const router = await renderAt("/p/CHK/permissions");
    await arrival(router, "/projects/CHK/settings/permissions");
  });
});

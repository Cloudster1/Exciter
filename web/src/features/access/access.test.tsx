import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Group, RoleDescription } from "@/api/access";
import { ACCESS_PATH } from "@/config";
import { accessAnswers, accessIds, groupList, roleList } from "@/test/access";
import { answerConfirm, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const checkout = aProject();
const bob = { id: accessIds.bob, name: "Bob Builder", email: "bob@exciter.test" };

function stubAccess(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    ...accessAnswers,
    "GET /projects": { status: 200, body: { projects: [checkout] } },
    "GET /people": { status: 200, body: { people: [bob] } },
    "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl: "http://localhost/api/v1/auth/oidc/callback" } },
    "GET /users": { status: 200, body: { members: [] } },
    "GET /users/requests": { status: 200, body: { requests: [] } },
    ...more,
  });
}

describe("the Access page", () => {
  it("opens on its Roles tab, each tab is an address of its own, and an unknown tab leads to Roles", async () => {
    stubAccess();
    const router = await renderAt(ACCESS_PATH);
    await waitFor(() => expect(router.state.location.pathname).toBe(`${ACCESS_PATH}/roles`));
    expect(await screen.findByRole("tab", { name: "Roles" })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: "Matrix" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`${ACCESS_PATH}/matrix`));
    await router.navigate({ to: `${ACCESS_PATH}/$tab`, params: { tab: "nowhere" } });
    await waitFor(() => expect(router.state.location.pathname).toBe(`${ACCESS_PATH}/roles`));
  });

  it("keeps a tab's address from the settings area of before", async () => {
    stubAccess();
    const router = await renderAt("/settings/access/groups");
    await waitFor(() => expect(router.state.location.pathname).toBe(`${ACCESS_PATH}/groups`));
    expect(await screen.findByRole("tab", { name: "Groups" })).toHaveAttribute("aria-selected", "true");
  });

  it("is where the single sign-on page of before leads", async () => {
    stubAccess();
    const router = await renderAt("/settings/sso");
    await waitFor(() => expect(router.state.location.pathname).toBe(`${ACCESS_PATH}/sso`));
    expect(await screen.findByLabelText("Issuer")).toBeInTheDocument();
    expect(document.querySelector("[data-group-roles]")).toBeNull();
  });

  it("tells somebody who does not administer the installation whose page it is, and asks nothing", async () => {
    const sent = stubAccess();
    await renderAt(`${ACCESS_PATH}/roles`, { me: { ...signedIn, administers: false } });
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/role-assignments" || r.path === "/roles")).toBe(false);
  });
});

describe("the Roles tab", () => {
  it("lists who holds what where, and grants a role in a project", async () => {
    const sent = stubAccess({
      "POST /role-assignments": {
        status: 201,
        body: {
          assignment: {
            id: "g-4",
            role: "auditor",
            roleName: "Auditor",
            projectId: checkout.id,
            projectKey: checkout.key,
            projectName: checkout.name,
            userId: bob.id,
            userName: bob.name,
            createdAt: "2026-10-02T08:00:00Z",
          },
        },
      },
    });
    await renderAt(`${ACCESS_PATH}/roles`);
    const table = await screen.findByRole("table", { name: "Roles" });
    expect(within(table).getByText("Everyone").closest("tr")).toHaveTextContent("Whole installation");
    expect(within(table).getByText("Reviewers").closest("tr")).toHaveTextContent(`${checkout.name} (CHK)`);
    expect(await axeViolations()).toEqual([]);

    const form = document.querySelector<HTMLElement>("[data-grant-form]")!;
    await userEvent.type(within(form).getByRole("combobox", { name: "Person or group to grant the role to" }), "Bo");
    await userEvent.click(await within(form).findByRole("option", { name: /Bob Builder/ }));
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "administrator");
    expect(within(form).getByRole("combobox", { name: "Where" })).toBeDisabled();
    expect(within(form).getByText(/answers for the whole installation/)).toBeInTheDocument();
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "auditor");
    await waitFor(() => expect(within(form).getByRole("option", { name: `${checkout.name} (CHK)` })).toBeInTheDocument());
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Where" }), "CHK");
    await userEvent.click(within(form).getByRole("button", { name: "Grant" }));
    expect(await screen.findByText("Bob Builder holds Auditor now.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "auditor", projectKey: "CHK", userId: bob.id, groupId: null });
  });

  it("asks before a grant goes, and shows the server's refusal in its sentence", async () => {
    const refusal = "Exciter keeps at least one active administrator. Make somebody else an administrator first, then take it from this one.";
    stubAccess({ "DELETE /role-assignments/g-1": { status: 409, body: { error: { code: "last_administrator", message: refusal } } } });
    await renderAt(`${ACCESS_PATH}/roles`);
    const table = await screen.findByRole("table", { name: "Roles" });
    await userEvent.click(within(table).getByRole("button", { name: "Revoke Administrator from Ada Lovelace" }));
    await answerConfirm(true);
    expect(await screen.findByText(refusal)).toBeInTheDocument();
  });

  it("takes a grant away and says so", async () => {
    const sent = stubAccess({ "DELETE /role-assignments/g-2": { status: 204 } });
    await renderAt(`${ACCESS_PATH}/roles`);
    const table = await screen.findByRole("table", { name: "Roles" });
    await userEvent.click(within(table).getByRole("button", { name: "Revoke Viewer from Everyone" }));
    await answerConfirm(true);
    expect(await screen.findByText("Everyone no longer holds Viewer.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/role-assignments/g-2")).toBe(true);
  });

  it("narrows the grants to one role and to what was typed", async () => {
    const sent = stubAccess();
    await renderAt(`${ACCESS_PATH}/roles`);
    await screen.findByRole("table", { name: "Roles" });
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Only grants of" }), "editor");
    await waitFor(() => expect(sent.some((r) => r.path === "/role-assignments" && r.method === "GET")).toBe(true));
    await userEvent.type(screen.getByRole("searchbox", { name: "Find grants" }), "rev");
    await waitFor(() => expect(sent.filter((r) => r.path === "/role-assignments").length).toBeGreaterThanOrEqual(3));
  });

  it("adds a role from the Roles tab too", async () => {
    const sent = stubAccess({
      "POST /roles": {
        status: 201,
        body: { role: { role: "steward", name: "Steward", description: "", orgWideOnly: true, builtin: false, permissions: [], inUse: 0 } },
      },
    });
    await renderAt(`${ACCESS_PATH}/roles`);
    await userEvent.click(await screen.findByRole("button", { name: "New role" }));
    const dialog = await screen.findByRole("dialog", { name: "New role" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Steward");
    await userEvent.click(within(dialog).getByRole("switch", { name: "Answers for the whole installation only" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Add role" }));
    expect(await screen.findByText("Steward was added.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST" && r.path === "/roles")?.body).toEqual({
      name: "Steward",
      description: "",
      orgWideOnly: true,
      permissions: [],
    });
  });
});

describe("the Matrix tab", () => {
  it("saves a box the moment it is ticked, and keeps the administrator administering", async () => {
    const reviewer = roleList[3]!;
    const sent = stubAccess({ "PATCH /roles/reviewer": { status: 200, body: { role: { ...reviewer, permissions: ["view", "comment", "approve", "edit"] } } } });
    await renderAt(`${ACCESS_PATH}/matrix`);
    const matrix = await screen.findByRole("table", { name: "Matrix" });
    const fixed = matrix.querySelector<HTMLInputElement>('[data-permission-cell="administrator:administerInstallation"]')!;
    expect(fixed).toBeChecked();
    expect(fixed).toBeDisabled();
    expect(matrix.querySelector('[data-permission-cell="admin:administer"]')).toBeChecked();
    expect(matrix.querySelector('[data-permission-cell="viewer:edit"]')).not.toBeChecked();
    expect(matrix.querySelector('[data-permission-section="project"]')).toHaveTextContent("In a project");
    expect(matrix.querySelector('[data-permission-section="installation"]')).toHaveTextContent("Create projects");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(matrix.querySelector<HTMLInputElement>('[data-permission-cell="reviewer:edit"]')!);
    expect(await screen.findByText("Reviewer may now write, change, link and move requirements.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")).toMatchObject({ path: "/roles/reviewer", body: { permissions: ["view", "comment", "approve", "edit"] } });
  });

  it("adds a role, renames one so the page shows the name at once, and deletes one with its grants", async () => {
    let current: RoleDescription[] = roleList;
    const sent = stubAccess({
      "GET /roles": () => ({ status: 200, body: { roles: current } }),
      "POST /roles": () => {
        const made: RoleDescription = { role: "tester", name: "Tester", description: "", orgWideOnly: false, builtin: false, permissions: ["view"], inUse: 0 };
        current = [...current, made];
        return { status: 201, body: { role: made } };
      },
      "PATCH /roles/editor": () => {
        current = current.map((role) => (role.role === "editor" ? { ...role, name: "Author" } : role));
        return { status: 200, body: { role: current[2] } };
      },
      "DELETE /roles/auditor": () => {
        current = current.filter((role) => role.role !== "auditor");
        return { status: 204 };
      },
    });
    await renderAt(`${ACCESS_PATH}/matrix`);
    await userEvent.click(await screen.findByRole("button", { name: "New role" }));
    const dialog = await screen.findByRole("dialog", { name: "New role" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Tester");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add role" }));
    expect(await screen.findByText("Tester was added.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Tester", description: "", orgWideOnly: false, permissions: ["view"] });

    const matrix = screen.getByRole("table", { name: "Matrix" });
    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Editor" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));
    const rename = await screen.findByRole("dialog", { name: "Rename Editor" });
    await userEvent.clear(within(rename).getByLabelText("Name"));
    await userEvent.type(within(rename).getByLabelText("Name"), "Author");
    await userEvent.click(within(rename).getByRole("button", { name: "Save" }));
    expect(await within(matrix).findByText("Author")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ name: "Author", description: "" });

    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Viewer" }));
    expect(await screen.findByRole("menuitem", { name: "Delete" })).toHaveAttribute("aria-disabled", "true");
    await userEvent.keyboard("{Escape}");
    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Auditor" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    expect(await screen.findByText(/2 grants of Auditor go with it/)).toBeInTheDocument();
    await answerConfirm(true);
    expect(await screen.findByText("Auditor was deleted.")).toBeInTheDocument();
  });
});

describe("the Groups tab", () => {
  it("shows Everyone as built in, makes a group the provider fills, and fills one kept by hand", async () => {
    const sent = stubAccess({
      "POST /groups": { status: 201, body: { group: { ...groupList[2], id: "g-new", name: "Leads", externalRef: "leads" } } },
      [`POST /groups/${accessIds.reviewers}/members`]: { status: 200, body: { group: { ...groupList[1], memberCount: 2 } } },
    });
    await renderAt(`${ACCESS_PATH}/groups`);
    const everyone = await screen.findByText("Every member of the installation, now and later, never a guest. Grant it a role to reach everybody at once.");
    expect(everyone.closest("[data-group-builtin]")).toHaveAttribute("data-group-builtin", "everyone");
    expect(within(everyone.closest("[data-group-builtin]") as HTMLElement).queryByRole("button")).toBeNull();
    const staff = document.querySelector<HTMLElement>('[data-group="Staff"]')!;
    expect(within(staff).getByText("From the identity provider")).toBeInTheDocument();
    expect(within(staff).queryByRole("combobox")).toBeNull();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(screen.getByRole("textbox", { name: "New group" }), "Leads");
    await userEvent.type(screen.getByLabelText("Provider claim"), "leads");
    await userEvent.click(screen.getByRole("button", { name: "Make group" }));
    expect(await screen.findByText("Leads was made.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST" && r.path === "/groups")?.body).toEqual({ name: "Leads", externalRef: "leads" });

    const reviewers = document.querySelector<HTMLElement>('[data-group="Reviewers"]')!;
    await userEvent.type(within(reviewers).getByRole("combobox", { name: "Add somebody to Reviewers" }), "Bo");
    await userEvent.click(await within(reviewers).findByRole("option", { name: /Bob Builder/ }));
    expect(await screen.findByText("Bob Builder is in Reviewers now.")).toBeInTheDocument();
    expect(sent.find((r) => r.path === `/groups/${accessIds.reviewers}/members`)?.body).toEqual({ userId: bob.id });
  });

  it("takes somebody out of a group kept by hand once asked", async () => {
    const sent = stubAccess({
      [`DELETE /groups/${accessIds.reviewers}/members/${accessIds.ada}`]: { status: 200, body: { group: { ...groupList[1], memberCount: 0 } } },
    });
    await renderAt(`${ACCESS_PATH}/groups`);
    await userEvent.click(await screen.findByRole("button", { name: "Remove Ada Lovelace" }));
    await answerConfirm(true);
    expect(await screen.findByText("Ada Lovelace left Reviewers.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/groups/${accessIds.reviewers}/members/${accessIds.ada}`)).toBe(true);
  });

  it("renames a group", async () => {
    const sent = stubAccess({ [`PATCH /groups/${accessIds.reviewers}`]: { status: 200, body: { group: { ...groupList[1], name: "Checkers" } } } });
    await renderAt(`${ACCESS_PATH}/groups`);
    const reviewers = await waitFor(() => {
      const card = document.querySelector<HTMLElement>('[data-group="Reviewers"]');
      expect(card).not.toBeNull();
      return card!;
    });
    await userEvent.click(within(reviewers).getByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Reviewers" });
    await userEvent.clear(within(dialog).getByLabelText("Name"));
    await userEvent.type(within(dialog).getByLabelText("Name"), "Checkers");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Checkers was saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ name: "Checkers", description: "", externalRef: "" });
  });
});

describe("deleting a group", () => {
  it("says so once the group and its card are gone", async () => {
    let current: Group[] = groupList;
    stubAccess({
      "GET /groups": () => ({ status: 200, body: { groups: current } }),
      [`DELETE /groups/${accessIds.reviewers}`]: () => {
        current = current.filter((group) => group.id !== accessIds.reviewers);
        return { status: 204 };
      },
    });
    await renderAt(`${ACCESS_PATH}/groups`);
    const reviewers = await waitFor(() => {
      const card = document.querySelector<HTMLElement>('[data-group="Reviewers"]');
      expect(card).not.toBeNull();
      return card!;
    });
    await userEvent.click(within(reviewers).getByRole("button", { name: "Delete" }));
    await answerConfirm(true);
    expect(await screen.findByText("Reviewers was deleted.")).toBeInTheDocument();
    expect(document.querySelector('[data-group="Reviewers"]')).toBeNull();
  });
});

describe("the Members tab", () => {
  it("shows each person's standing and the roles they hold, by name or through a group", async () => {
    stubAccess({
      "GET /users": {
        status: 200,
        body: {
          members: [
            {
              userId: accessIds.ada,
              email: "ada@exciter.test",
              name: "Ada Lovelace",
              role: "member",
              administers: true,
              active: true,
              joinedAt: "2026-09-01T08:00:00Z",
              roles: [
                { role: "administrator", name: "Administrator", groupId: null, groupName: null, groupBuiltin: null },
                { role: "viewer", name: "Viewer", groupId: accessIds.everyone, groupName: "Everyone", groupBuiltin: "everyone" },
              ],
            },
            {
              userId: bob.id,
              email: bob.email,
              name: bob.name,
              role: "guest",
              administers: false,
              active: true,
              joinedAt: "2026-09-02T08:00:00Z",
              roles: [],
            },
          ],
        },
      },
    });
    await renderAt(`${ACCESS_PATH}/members`);
    const ada = (await screen.findByText("ada@exciter.test")).closest("tr")!;
    expect(ada.querySelector('[data-member-holds="administrator"]')).toHaveTextContent("Administrator");
    expect(ada.querySelector('[data-member-holds="viewer"]')).toHaveTextContent("Viewer through Everyone");
    const guest = screen.getByText(bob.email).closest("tr")!;
    expect(within(guest).getByRole("combobox", { name: "Standing of Bob Builder" })).toHaveValue("guest");
    expect(within(guest).getByText("None")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

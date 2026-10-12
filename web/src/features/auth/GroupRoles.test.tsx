import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MEMBERS_PATH, SSO_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const provider = {
  provider: {
    issuer: "http://localhost:8180/realms/suite-dev",
    clientId: "exciter",
    hasSecret: true,
    groupsClaim: "groups",
    scopes: "openid profile email",
    createGroups: false,
    enabled: true,
    updatedAt: "2026-09-29T08:00:00Z",
  },
  callbackUrl: "http://localhost:5173/api/v1/auth/oidc/callback",
};
const admins = { id: "0195f000-0000-7000-8000-0000000000a1", group: "exciter-administrators", role: "admin", updatedAt: "2026-09-29T08:00:00Z" };
const engineering = { id: "0195f000-0000-7000-8000-0000000000e1", group: "engineering", role: "member", updatedAt: "2026-09-29T08:01:00Z" };

const owner = {
  userId: "0195f000-0000-7000-8000-00000000000a",
  email: "admin@suite.test",
  name: "Admin",
  role: "owner",
  roleSource: "manual",
  joinedAt: "2026-09-01T08:00:00Z",
};
const ada = {
  userId: signedIn.user.id,
  email: signedIn.user.email,
  name: signedIn.user.name,
  role: "admin",
  roleSource: "manual",
  joinedAt: "2026-09-02T08:00:00Z",
};
const alice = {
  userId: "0195f000-0000-7000-8000-0000000a11ce",
  email: "alice@suite.test",
  name: "Alice Admin",
  role: "admin",
  roleSource: "oidc",
  joinedAt: "2026-09-03T08:00:00Z",
};
const bob = {
  userId: "0195f000-0000-7000-8000-000000000b0b",
  email: "bob@suite.test",
  name: "Bob Builder",
  role: "member",
  roleSource: "manual",
  joinedAt: "2026-09-04T08:00:00Z",
};

const settings = (extra: Parameters<typeof stubApi>[0] = {}) =>
  stubApi({
    "GET /oidc-provider": { status: 200, body: provider },
    "GET /users/requests": { status: 200, body: { requests: [] } },
    "GET /users": { status: 200, body: { members: [owner, ada, alice, bob] } },
    "GET /oidc-provider/group-roles": { status: 200, body: { groupRoles: [admins] } },
    ...extra,
  });

async function mappingCard() {
  const heading = await screen.findByRole("heading", { name: "Roles from groups" });
  return heading.closest("[data-group-roles]") as HTMLElement;
}

describe("the roles groups grant", () => {
  it("lists what is mapped, and maps another group to the role chosen", async () => {
    let mapping = [admins];
    const sent = settings({
      "GET /oidc-provider/group-roles": () => ({ status: 200, body: { groupRoles: mapping } }),
      "POST /oidc-provider/group-roles": () => {
        mapping = [engineering, admins];
        return { status: 200, body: { groupRole: engineering } };
      },
    });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    const row = await within(card).findByText("exciter-administrators");
    expect(row.closest("tr")).toHaveTextContent("Administrator");

    await userEvent.type(within(card).getByLabelText("Group"), "engineering");
    expect(within(card).getByLabelText("Role")).toHaveValue("member");
    await userEvent.click(within(card).getByRole("button", { name: "Map group" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ group: "engineering", role: "member" }));
    expect(await within(card).findByText("engineering now grants Member.")).toBeInTheDocument();
    expect(await within(card).findByText("engineering", { selector: "td" })).toBeInTheDocument();
    expect(within(card).getByLabelText("Group")).toHaveValue("");
  });

  it("maps a group to admin when admin is chosen", async () => {
    const sent = settings({ "POST /oidc-provider/group-roles": { status: 200, body: { groupRole: { ...engineering, role: "admin" } } } });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    await userEvent.type(within(card).getByLabelText("Group"), "ops");
    await userEvent.selectOptions(within(card).getByLabelText("Role"), "admin");
    await userEvent.click(within(card).getByRole("button", { name: "Map group" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ group: "ops", role: "admin" }));
  });

  it("unmaps a group", async () => {
    let mapping = [admins, engineering];
    const sent = settings({
      "GET /oidc-provider/group-roles": () => ({ status: 200, body: { groupRoles: mapping } }),
      [`DELETE /oidc-provider/group-roles/${admins.id}`]: () => {
        mapping = [engineering];
        return { status: 204 };
      },
    });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    await userEvent.click(await within(card).findByRole("button", { name: "Unmap exciter-administrators" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/oidc-provider/group-roles/${admins.id}`)).toBe(true));
    expect(await within(card).findByText("exciter-administrators no longer grants a role.")).toBeInTheDocument();
    await waitFor(() => expect(within(card).queryByText("exciter-administrators", { selector: "td" })).toBeNull());
  });

  it("shows the server's word on a field beside it", async () => {
    settings({
      "POST /oidc-provider/group-roles": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { group: "Enter the group's name as the provider sends it in the groups claim." },
          },
        },
      },
    });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    await userEvent.click(within(card).getByRole("button", { name: "Map group" }));
    expect(await within(card).findByText("Enter the group's name as the provider sends it in the groups claim.")).toBeInTheDocument();
    expect(within(card).getByLabelText("Group")).toHaveAttribute("aria-invalid", "true");
  });

  it("says so when nothing is mapped yet", async () => {
    settings({ "GET /oidc-provider/group-roles": { status: 200, body: { groupRoles: [] } } });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    expect(await within(card).findByText("No group is mapped yet. Roles stay as administrators set them.")).toBeInTheDocument();
  });

  it("waits for a provider before offering to map anything", async () => {
    settings({ "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl: provider.callbackUrl } } });
    await renderAt(SSO_PATH);
    const card = await mappingCard();
    expect(within(card).getByText("Set up single sign-on above before mapping its groups to roles.")).toBeInTheDocument();
    expect(within(card).queryByRole("button", { name: "Map group" })).toBeNull();
  });
});

describe("the members list", () => {
  it("marks the roles that come from the identity provider, and only those", async () => {
    settings();
    await renderAt(MEMBERS_PATH);
    const aliceRow = (await screen.findByText("Alice Admin")).closest("tr") as HTMLElement;
    expect(within(aliceRow).getByText("From identity provider")).toBeInTheDocument();
    const bobRow = screen.getByText("Bob Builder").closest("tr") as HTMLElement;
    expect(within(bobRow).queryByText("From identity provider")).toBeNull();
    expect(document.querySelectorAll("[data-role-source]")).toHaveLength(1);
  });
});

it("the single sign-on page with its mapping passes axe", async () => {
  settings({ "GET /oidc-provider/group-roles": { status: 200, body: { groupRoles: [admins, engineering] } } });
  await renderAt(SSO_PATH);
  await screen.findByText("engineering", { selector: "td" });
  expect(await axeViolations()).toEqual([]);
});

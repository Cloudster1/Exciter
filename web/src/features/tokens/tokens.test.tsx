import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ApiToken } from "@/api/tokens";
import { DAY_MS, ORG_TOKENS_PATH, TOKENS_PATH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { expiryFrom, mcpClientSettings, reachOf } from "./TokensPage";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function aToken(overrides: Partial<ApiToken> = {}): ApiToken {
  return {
    id: "0195f000-0000-7000-8000-0000000005a1",
    name: "Nightly export",
    scopes: [],
    projects: [],
    allProjects: true,
    lastUsedAt: null,
    expiresAt: null,
    createdAt: "2026-10-01T08:00:00Z",
    ...overrides,
  };
}

const owner = { id: "0195f000-0000-7000-8000-00000000000b", email: "bob@exciter.test", name: "Bob", locale: "" as const };

describe("what a token reaches and when it ends", () => {
  it("says every project, some, or none any more, and counts days from now", () => {
    expect(reachOf({ allProjects: true, projects: [] })).toBe("Every project you can reach");
    expect(reachOf({ allProjects: false, projects: ["CHK", "SYS"] })).toBe("Only CHK, SYS");
    expect(reachOf({ allProjects: false, projects: [] })).toMatch(/No project any more/);
    const now = new Date("2026-10-13T12:00:00Z");
    expect(expiryFrom(0, now)).toBeUndefined();
    expect(expiryFrom(7, now)).toBe(new Date(now.getTime() + 7 * DAY_MS).toISOString());
    expect(JSON.parse(mcpClientSettings("https://x.test/api/v1/mcp", "T"))).toEqual({
      mcpServers: { exciter: { type: "http", url: "https://x.test/api/v1/mcp", headers: { Authorization: "Bearer T" } } },
    });
  });
});

describe("personal access tokens", () => {
  it("makes a read-only token limited to a project and shows its secret once", async () => {
    const made = aToken({ scopes: ["read"], projects: ["CHK"], allProjects: false, secret: "exciter_pat_secret" });
    let mine: ApiToken[] = [];
    const sent = stubApi({
      "GET /tokens": () => ({ status: 200, body: { tokens: mine } }),
      "GET /projects": { status: 200, body: { projects: [aProject()] } },
      "POST /tokens": () => {
        mine = [{ ...made, secret: undefined }];
        return { status: 201, body: { token: made } };
      },
    });
    await renderAt(TOKENS_PATH);
    expect(await screen.findByText("No tokens yet")).toBeInTheDocument();
    await userEvent.type(screen.getByRole("textbox", { name: "Token name" }), "Nightly export");
    await userEvent.click(screen.getByRole("checkbox", { name: "Read only" }));
    await userEvent.click(await screen.findByRole("button", { name: /Checkout/ }));
    expect(screen.getByText(/reaches only the projects picked here/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Create token" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST")?.body).toMatchObject({ name: "Nightly export", scopes: ["read"], projects: ["CHK"] }),
    );
    expect(await screen.findByRole("textbox", { name: "Your new token" })).toHaveValue("exciter_pat_secret");
    const row = (await screen.findByText("Only CHK")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Read only")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("textbox", { name: "Your new token" })).not.toBeInTheDocument();
  });

  it("revokes a token once its owner agrees", async () => {
    const token = aToken();
    const sent = stubApi({
      "GET /tokens": { status: 200, body: { tokens: [token] } },
      "GET /projects": { status: 200, body: { projects: [] } },
      [`DELETE /tokens/${token.id}`]: { status: 204 },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt(TOKENS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke Nightly export" }));
    expect(await screen.findByText("Nightly export was revoked.")).toBeInTheDocument();
    expect(sent.some((each) => each.method === "DELETE" && each.path === `/tokens/${token.id}`)).toBe(true);
  });
});

describe("the organization's tokens", () => {
  it("lists everybody's with whose it is, and revokes one", async () => {
    const theirs = { ...aToken(), owner };
    const sent = stubApi({
      "GET /org/tokens": { status: 200, body: { tokens: [theirs] } },
      [`DELETE /org/tokens/${theirs.id}`]: { status: 204 },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt(ORG_TOKENS_PATH);
    const table = await screen.findByRole("table", { name: "Access tokens" });
    expect(within(table).getByText("Bob")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(table).getByRole("button", { name: "Revoke Nightly export of Bob" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/org/tokens/${theirs.id}`)).toBe(true));
  });

  it("tells a member whose page it is", async () => {
    stubApi({});
    await renderAt(ORG_TOKENS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText(/Only the organization's owners and administrators see everybody's tokens/)).toBeInTheDocument();
  });
});

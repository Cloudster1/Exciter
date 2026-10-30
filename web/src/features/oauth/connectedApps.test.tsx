import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ALL_CONNECTED_APPS_PATH, CONNECTED_APPS_PATH } from "@/config";
import { answerConfirm, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const aGrant = (over: Record<string, unknown> = {}) => ({
  id: "0195f000-0000-7000-8000-0000000000c1",
  client: { id: "c1", name: "Desk assistant", uri: "https://assistant.example", kind: "registered" },
  redirectHost: "127.0.0.1:33418",
  scopes: ["read"],
  projects: ["CHK"],
  allProjects: false,
  createdAt: "2026-10-17T09:00:00Z",
  lastUsedAt: "2026-10-17T10:00:00Z",
  ...over,
});
const owner = { id: "0195f000-0000-7000-8000-00000000000b", email: "bob@exciter.test", name: "Bob", locale: "", timezone: "" };

describe("connected apps", () => {
  it("says a partner app was connected at the person's sign-in there, not where it sends back to", async () => {
    stubApi({
      "GET /oauth/grants": {
        status: 200,
        body: { grants: [aGrant({ client: { id: "p1", name: "Stator", uri: "", kind: "partner" }, redirectHost: "", allProjects: true, projects: [] })] },
      },
    });
    await renderAt(CONNECTED_APPS_PATH);
    const table = await screen.findByRole("table", { name: "Connected apps" });
    const row = within(table).getByText("Stator").closest("tr")!;
    expect(within(row).getByText("Connected when you signed in to it with the shared sign-in")).toBeInTheDocument();
    expect(within(row).queryByText(/Sends back to/)).not.toBeInTheDocument();
  });

  it("lists the apps with what each reaches and when it was last used, and disconnects one", async () => {
    let grants = [
      aGrant(),
      aGrant({
        id: "0195f000-0000-7000-8000-0000000000c2",
        client: { id: "c2", name: "Phone assistant", uri: "", kind: "metadata" },
        scopes: [],
        allProjects: true,
        projects: [],
        lastUsedAt: null,
      }),
    ];
    const sent = stubApi({
      "GET /oauth/grants": () => ({ status: 200, body: { grants } }),
      "DELETE /oauth/grants/0195f000-0000-7000-8000-0000000000c1": () => {
        grants = grants.slice(1);
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    await renderAt(CONNECTED_APPS_PATH);
    const table = await screen.findByRole("table", { name: "Connected apps" });
    const desk = within(table).getByText("Desk assistant").closest("tr")!;
    expect(within(desk).getByText("Read only")).toBeInTheDocument();
    expect(within(desk).getByText("Only CHK")).toBeInTheDocument();
    expect(within(desk).getByText("Sends back to 127.0.0.1:33418")).toBeInTheDocument();
    const phone = within(table).getByText("Phone assistant").closest("tr")!;
    expect(within(phone).getByText("Never used")).toBeInTheDocument();
    expect(within(phone).getByText("Every project you can reach")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await user.click(within(desk).getByRole("button", { name: "Disconnect Desk assistant" }));
    expect(await answerConfirm(true)).toHaveTextContent("Disconnect app Desk assistant?");
    expect(await screen.findByText("Desk assistant was disconnected.")).toBeInTheDocument();
    expect(sent.some((each) => each.method === "DELETE")).toBe(true);
    await waitFor(() => expect(screen.queryByText("Desk assistant")).not.toBeInTheDocument());
  });

  it("says how to connect one when there is none", async () => {
    stubApi({ "GET /oauth/grants": { status: 200, body: { grants: [] } } });
    await renderAt(CONNECTED_APPS_PATH);
    expect(await screen.findByText("No apps connected")).toBeInTheDocument();
    expect(screen.getByText(/Add Exciter's MCP address in an app that connects with OAuth/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("keeps an app connected when the person thinks again", async () => {
    const sent = stubApi({ "GET /oauth/grants": { status: 200, body: { grants: [aGrant()] } } });
    await renderAt(CONNECTED_APPS_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Disconnect Desk assistant" }));
    await answerConfirm(false);
    expect(sent.some((each) => each.method === "DELETE")).toBe(false);
  });
});

describe("everybody's connected apps", () => {
  it("lists everybody's with whose each is, and disconnects one", async () => {
    const theirs = { ...aGrant(), owner };
    const sent = stubApi({
      "GET /oauth/grants/everyone": { status: 200, body: { grants: [theirs] } },
      [`DELETE /oauth/grants/everyone/${theirs.id}`]: { status: 204 },
    });
    await renderAt(ALL_CONNECTED_APPS_PATH);
    const table = await screen.findByRole("table", { name: "All connected apps" });
    expect(within(table).getByText("Bob")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(table).getByRole("button", { name: "Disconnect Desk assistant of Bob" }));
    await answerConfirm(true);
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/oauth/grants/everyone/${theirs.id}`)).toBe(true));
  });

  it("tells a member whose page it is", async () => {
    stubApi({});
    await renderAt(ALL_CONNECTED_APPS_PATH, { me: { ...signedIn, role: "member" } });
    expect(await screen.findByText(/Only an administrator sees everybody.s connected apps/)).toBeInTheDocument();
  });
});

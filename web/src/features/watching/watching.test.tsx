import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Watch, Watching } from "@/api/watching";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const none: Watching = { requirement: false, project: false };

function stubRequirement({
  watching = none,
  more = {},
}: {
  watching?: Watching | (() => Watching);
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const now = () => (typeof watching === "function" ? watching() : watching);
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": () => ({ status: 200, body: { requirement: aRequirement({ watching: now() }) } }),
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads: [] } },
    "GET /requirements/CHK-R1/mentionable": { status: 200, body: { people: [] } },
    ...more,
  });
}

async function openMenu() {
  await userEvent.click(await screen.findByRole("button", { name: /^(Watch|Watching)$/ }));
  return screen.getByRole("menu", { name: "Watching CHK-R1" });
}

describe("the requirement's watch button", () => {
  it("watches the requirement, and stops", async () => {
    let watching = none;
    const sent = stubRequirement({
      watching: () => watching,
      more: {
        "PUT /requirements/CHK-R1/watch": () => {
          watching = { ...watching, requirement: true };
          return { status: 200, body: { watching } };
        },
        "DELETE /requirements/CHK-R1/watch": () => {
          watching = { ...watching, requirement: false };
          return { status: 204 };
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await screen.findByRole("button", { name: "Watch" })).toHaveAttribute("aria-haspopup", "menu");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Watch this requirement" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PUT" && each.path === "/requirements/CHK-R1/watch")).toBe(true));
    expect(await screen.findByRole("button", { name: "Watching" })).toBeInTheDocument();
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Stop watching this requirement" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === "/requirements/CHK-R1/watch")).toBe(true));
    expect(await screen.findByRole("button", { name: "Watch" })).toBeInTheDocument();
  });

  it("says a project watch covers the requirement, and offers to stop it", async () => {
    const sent = stubRequirement({
      watching: { requirement: false, project: true },
      more: { "DELETE /projects/CHK/watch": { status: 204 } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const menu = await openMenu();
    expect(within(menu).getByRole("menuitem", { name: "Covered by your watch on the whole project" })).toHaveAttribute("aria-disabled", "true");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Stop watching the project" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === "/projects/CHK/watch")).toBe(true));
  });

  it("shows who is watching, and through which watch", async () => {
    stubRequirement({
      more: {
        "GET /requirements/CHK-R1/watchers": {
          status: 200,
          body: {
            watchers: [
              { userId: "0195f000-0000-7000-8000-0000000000b1", name: "Bob Builder", email: "bob@exciter.test", via: "requirement" },
              { userId: "0195f000-0000-7000-8000-0000000000b2", name: "Carol Clerk", email: "carol@exciter.test", via: "project" },
            ],
            total: 2,
            limit: 100,
            offset: 0,
          },
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Who is watching" }));
    const dialog = await screen.findByRole("dialog", { name: "Who is watching CHK-R1" });
    const list = await within(dialog).findByRole("list", { name: "Who is watching" });
    expect(list.querySelector('[data-watcher="Bob Builder"]')).toHaveTextContent("Watches this requirement");
    expect(list.querySelector('[data-watcher="Carol Clerk"]')).toHaveTextContent("Watches the whole project");
    expect(await axeViolations()).toEqual([]);
  });

  it("says so when watching cannot be changed", async () => {
    stubRequirement({ more: { "PUT /requirements/CHK-R1/watch": { status: 500, body: { error: { code: "internal", message: "Broken." } } } } });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Watch this requirement" }));
    expect(await screen.findByText("Watching could not be changed. Try again in a moment.")).toBeInTheDocument();
  });
});

describe("the project's watch button", () => {
  it("watches every requirement of the project, pressed while it does", async () => {
    let watching = false;
    const sent = stubApi({
      "GET /projects/CHK": () => ({ status: 200, body: { project: aProject({ watching }) } }),
      "GET /projects/CHK/requirements": { status: 200, body: { requirements: [], total: 0, limit: 50, offset: 0 } },
      "PUT /projects/CHK/watch": () => {
        watching = true;
        return { status: 200, body: { watch: { id: "w1", kind: "project", projectKey: "CHK", requirement: null, createdAt: "2026-10-01T08:00:00Z" } } };
      },
    });
    await renderAt("/p/CHK");
    const button = await screen.findByRole("button", { name: "Watch project" });
    expect(button).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(button);
    await waitFor(() => expect(sent.some((each) => each.method === "PUT" && each.path === "/projects/CHK/watch")).toBe(true));
    expect(await screen.findByRole("button", { name: "Watching project" })).toHaveAttribute("aria-pressed", "true");
  });
});

describe("the watching list", () => {
  const watches: Watch[] = [
    {
      id: "0195f000-0000-7000-8000-0000000000d1",
      kind: "requirement",
      projectKey: "CHK",
      requirement: {
        id: "0195f000-0000-7000-8000-0000000000a1",
        key: "CHK-R1",
        projectKey: "CHK",
        kind: "requirement",
        title: "Pay the basket",
        revision: 2,
        status: null,
        trashed: false,
      },
      createdAt: "2026-10-01T08:00:00Z",
    },
    { id: "0195f000-0000-7000-8000-0000000000d2", kind: "project", projectKey: "OPS", requirement: null, createdAt: "2026-09-30T08:00:00Z" },
  ];

  it("lists what the reader watches and stops one", async () => {
    const sent = stubApi({
      "GET /watches": { status: 200, body: { watches, total: 2, limit: 20, offset: 0 } },
      "DELETE /projects/OPS/watch": { status: 204 },
    });
    await renderAt("/settings/watching");
    const row = (await screen.findByText("Project OPS")).closest("tr") as HTMLElement;
    expect(screen.getByRole("link", { name: "CHK-R1 Pay the basket" })).toHaveAttribute("href", "/p/CHK/r/CHK-R1");
    expect(row).toHaveTextContent("Every requirement of the project");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(row).getByRole("button", { name: "Stop watching Project OPS" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === "/projects/OPS/watch")).toBe(true));
    expect(await screen.findByText("You no longer watch Project OPS.")).toBeInTheDocument();
  });

  it("says what to do when the reader watches nothing", async () => {
    stubApi({ "GET /watches": { status: 200, body: { watches: [], total: 0, limit: 20, offset: 0 } } });
    await renderAt("/settings/watching");
    expect(await screen.findByText("You watch nothing yet")).toBeInTheDocument();
  });
});

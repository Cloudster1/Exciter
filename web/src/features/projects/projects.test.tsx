import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import { arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member" } };
const checkout = aProject();
const billing = aProject({ id: "0195f000-0000-7000-8000-0000000000f3", key: "BIL", name: "Billing", description: "" });

describe("the project list", () => {
  it("lists the projects by name with their keys, and offers administrators a new one", async () => {
    stubApi({ "GET /projects": { status: 200, body: { projects: [billing, checkout] } } });
    await renderAt(PROJECTS_PATH);
    const row = await screen.findByText("Checkout");
    expect(row.closest("tr")).toHaveAttribute("data-project-row", "CHK");
    expect(within(row.closest("tr")!).getByText("CHK")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Checkout" })).toHaveAttribute("href", "/p/CHK");
    expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument();
  });

  it("tells a member that administrators make projects, and offers no button", async () => {
    await renderAt(PROJECTS_PATH, { me: member });
    expect(await screen.findByText(/An owner or administrator of your organization makes projects/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New project" })).toBeNull();
  });

  it("asks for archived projects when told to show them", async () => {
    const sent = stubApi({ "GET /projects": { status: 200, body: { projects: [] } } });
    await renderAt(PROJECTS_PATH);
    await userEvent.click(await screen.findByRole("switch", { name: "Show archived projects" }));
    await waitFor(() => expect(sent.some((r) => r.method === "GET" && r.path === "/projects")).toBe(true));
    await waitFor(() => expect(document.querySelector("[data-show-archived]")).toHaveAttribute("aria-checked", "true"));
  });
});

describe("a new project", () => {
  it("takes the free key the server suggests for the name, and opens the project it made", async () => {
    const sent = stubApi({
      "GET /projects/key-check": (request) => {
        const name = new URL(request.url).searchParams.get("name");
        return { status: 200, body: { check: { key: "", valid: false, available: false, suggestion: name === "Checkout service" ? "CS2" : "" } } };
      },
      "POST /projects": { status: 201, body: { project: aProject({ key: "CS2", name: "Checkout service" }) } },
      "GET /projects/CS2": { status: 200, body: { project: aProject({ key: "CS2", name: "Checkout service" }) } },
    });
    const router = await renderAt(NEW_PROJECT_PATH);
    await userEvent.type(await screen.findByLabelText("Name"), "Checkout service");
    await waitFor(() => expect(screen.getByLabelText("Key")).toHaveValue("CS2"));
    await userEvent.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CS2/settings"));
    await arrival(router, "/p/CS2/settings");
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Checkout service", key: "CS2", description: "" });
  });

  it("says whether a key typed is free, and shows the field the server refused", async () => {
    stubApi({
      "GET /projects/key-check": { status: 200, body: { check: { key: "CHK", valid: true, available: false, suggestion: "" } } },
      "POST /projects": {
        status: 422,
        body: {
          error: { code: "validation_failed", message: "Check the fields.", fields: { key: "The key CHK is taken by another project. Choose another." } },
        },
      },
    });
    await renderAt(NEW_PROJECT_PATH);
    await userEvent.type(await screen.findByLabelText("Name"), "Checkout");
    const key = screen.getByLabelText("Key");
    await userEvent.clear(key);
    await userEvent.type(key, "chk");
    expect(await screen.findByText("CHK is taken by another project, now or before.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create project" }));
    expect(await screen.findByText("The key CHK is taken by another project. Choose another.")).toBeInTheDocument();
  });
});

describe("a project's settings", () => {
  it("moves to the current key when opened by a key the project had", async () => {
    stubApi({ "GET /projects/PAY": { status: 200, body: { project: aProject({ formerKeys: ["PAY"] }) } } });
    const router = await renderAt("/p/PAY/settings");
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CHK/settings"));
    await arrival(router, "/p/CHK/settings");
    expect(await screen.findByText("Former keys, which still work: PAY")).toBeInTheDocument();
  });

  it("changes the key and follows it", async () => {
    const moved = aProject({ key: "PAY", formerKeys: ["CHK"] });
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/PAY": { status: 200, body: { project: moved } },
      "PUT /projects/CHK/key": { status: 200, body: { project: moved } },
    });
    const router = await renderAt("/p/CHK/settings");
    await userEvent.type(await screen.findByLabelText("New key"), "pay");
    await userEvent.click(screen.getByRole("button", { name: "Change key" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/PAY/settings"));
    await arrival(router, "/p/PAY/settings");
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ key: "PAY" });
    expect(await screen.findByText("Former keys, which still work: CHK")).toBeInTheDocument();
  });

  it("saves the details and the revision comment setting", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "PATCH /projects/CHK": async (request) => {
        const body = (await request.json()) as { name: string };
        return { status: 200, body: { project: aProject({ name: body.name, settings: { ...checkout.settings, requireRevisionComment: true } }) } };
      },
    });
    await renderAt("/p/CHK/settings");
    const name = await screen.findByLabelText("Name");
    await userEvent.clear(name);
    await userEvent.type(name, "Checkout and payment");
    await userEvent.click(screen.getByRole("checkbox", { name: "Ask for a comment on every new revision" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({
        name: "Checkout and payment",
        description: checkout.description,
        settings: { requireRevisionComment: true },
      }),
    );
  });

  it("shows a member the settings without a way to change them", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: aProject({ can: { ...checkout.can, administer: false, purge: false } }) } } });
    await renderAt("/p/CHK/settings", { me: member });
    expect(await screen.findByText("Only the project's administrators change its settings.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Archive project" })).toBeNull();
  });

  it("archives, and deletes an archived project only after asking", async () => {
    let project = checkout;
    const sent = stubApi({
      "GET /projects/CHK": () => ({ status: 200, body: { project } }),
      "PUT /projects/CHK/archive": () => {
        project = aProject({ archivedAt: "2026-10-12T08:00:00Z" });
        return { status: 200, body: { project } };
      },
      "DELETE /projects/CHK": { status: 204 },
    });
    const router = await renderAt("/p/CHK/settings");
    expect(screen.queryByRole("button", { name: "Delete project" })).toBeNull();
    await userEvent.click(await screen.findByRole("button", { name: "Archive project" }));
    expect(await screen.findByText(/This project is archived/)).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    await userEvent.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = screen.getByRole("dialog", { name: "Delete this project for good?" });
    expect(within(dialog).getByText(/Checkout \(CHK\) and everything in it/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete for good" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(PROJECTS_PATH));
    await arrival(router, PROJECTS_PATH);
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/projects/CHK")).toBe(true);
  });
});

describe("the sidebar inside a project", () => {
  it("names the project and switches to another", async () => {
    stubApi({
      "GET /projects": { status: 200, body: { projects: [billing, checkout] } },
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/BIL": { status: 200, body: { project: billing } },
    });
    const router = await renderAt("/p/CHK/settings");
    const nav = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-project-nav="CHK"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(nav).getByRole("link", { name: "Project settings" })).toHaveAttribute("href", "/p/CHK/settings");
    await userEvent.click(within(nav).getByRole("button", { name: /Switch project/ }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Billing (BIL)" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/BIL/settings"));
    await arrival(router, "/p/BIL/settings");
  });
});

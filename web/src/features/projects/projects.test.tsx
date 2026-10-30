import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { NEW_PROJECT_PATH, PROJECTS_PATH } from "@/config";
import { answerConfirm, arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const member: Me = { ...signedIn, administers: false };
const checkout = aProject();
const billing = aProject({ id: "0195f000-0000-7000-8000-0000000000f3", key: "BIL", name: "Billing", description: "" });

describe("the project list", () => {
  it("lists the projects by name with their keys, and offers administrators a new one", async () => {
    stubApi({ "GET /projects": { status: 200, body: { projects: [billing, checkout] } } });
    await renderAt(PROJECTS_PATH);
    const row = await screen.findByText("Checkout");
    expect(row.closest("tr")).toHaveAttribute("data-project-row", "CHK");
    expect(within(row.closest("tr")!).getByText("CHK")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Checkout" })).toHaveAttribute("href", "/projects/CHK");
    expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument();
  });

  it("tells a member that administrators make projects, and offers no button", async () => {
    await renderAt(PROJECTS_PATH, { me: member });
    expect(await screen.findByText(/An administrator makes projects/)).toBeInTheDocument();
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
      "GET /projects/CS2/requirements": { status: 200, body: { rows: [], total: 0, limit: 50, offset: 0 } },
    });
    const router = await renderAt(NEW_PROJECT_PATH);
    await userEvent.type(await screen.findByLabelText("Name"), "Checkout service");
    await waitFor(() => expect(screen.getByLabelText("Key")).toHaveValue("CS2"));
    await userEvent.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CS2"));
    await arrival(router, "/projects/CS2");
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Checkout service", key: "CS2", description: "" });
  });

  it("starts a project from a template the server offers, named in the reader's language", async () => {
    const sent = stubApi({
      "GET /projects/key-check": { status: 200, body: { check: { key: "", valid: false, available: false, suggestion: "TST" } } },
      "GET /project-templates": {
        status: 200,
        body: {
          templates: [
            {
              key: "test",
              name: "Test specification",
              description: "From the server.",
              types: ["Test case", "Test procedure"],
              attributes: ["Automated"],
              statuses: ["Draft"],
              linkTypes: ["verifies", "relates"],
              sections: ["Test strategy", "Test cases"],
            },
          ],
        },
      },
      "POST /projects": { status: 422, body: { error: { code: "validation_failed", message: "Check the fields.", fields: { name: "Name it." } } } },
    });
    await renderAt(NEW_PROJECT_PATH);
    await userEvent.type(await screen.findByLabelText("Name"), "Tests");
    const chooser = screen.getByRole("radiogroup", { name: "Start from" });
    expect(within(chooser).getByRole("radio", { name: /Blank project/ })).toHaveAttribute("aria-checked", "true");
    const test = await within(chooser).findByRole("radio", { name: /Test specification/ });
    expect(test).toHaveTextContent("Types: Test case, Test procedure. Sections: Test strategy, Test cases.");
    await userEvent.click(test);
    expect(test).toHaveAttribute("aria-checked", "true");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toMatchObject({ name: "Tests", template: "test" }));
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
    const router = await renderAt("/projects/PAY/settings/details");
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/settings/details"));
    await arrival(router, "/projects/CHK/settings/details");
    expect(await screen.findByText("Former keys, which still work: PAY")).toBeInTheDocument();
  });

  it("changes the key and follows it", async () => {
    const moved = aProject({ key: "PAY", formerKeys: ["CHK"] });
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects/PAY": { status: 200, body: { project: moved } },
      "PUT /projects/CHK/key": { status: 200, body: { project: moved } },
    });
    const router = await renderAt("/projects/CHK/settings/details");
    await userEvent.type(await screen.findByLabelText("New key"), "pay");
    await userEvent.click(screen.getByRole("button", { name: "Change key" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/PAY/settings/details"));
    await arrival(router, "/projects/PAY/settings/details");
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ key: "PAY" });
    expect(await screen.findByText("Former keys, which still work: CHK")).toBeInTheDocument();
  });

  it("saves the details and the revision comment setting, and says so once the form shows the saved project", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "PATCH /projects/CHK": async (request) => {
        const body = (await request.json()) as { name: string };
        const changed = aProject({ name: body.name, settings: { ...checkout.settings, requireRevisionComment: true }, updatedAt: "2026-10-02T08:00:00Z" });
        return { status: 200, body: { project: changed } };
      },
    });
    await renderAt("/projects/CHK/settings/details");
    const name = await screen.findByLabelText("Name");
    await userEvent.clear(name);
    await userEvent.type(name, "Checkout and payment");
    await userEvent.click(screen.getByRole("checkbox", { name: "Ask for a comment on every new revision" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({
        name: "Checkout and payment",
        description: checkout.description,
        settings: { requireRevisionComment: true, signedApprovals: false, library: false },
      }),
    );
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Checkout and payment");
  });

  it("turns signed approvals on", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "PATCH /projects/CHK": { status: 200, body: { project: aProject({ settings: { ...checkout.settings, signedApprovals: true } }) } },
    });
    await renderAt("/projects/CHK/settings/details");
    const box = await screen.findByRole("checkbox", { name: "Approvals are electronic signatures" });
    expect(box).not.toBeChecked();
    await userEvent.click(box);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect((sent.find((r) => r.method === "PATCH")?.body as { settings: object }).settings).toEqual({
        requireRevisionComment: false,
        signedApprovals: true,
        library: false,
      }),
    );
  });

  it("marks the project a library", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "PATCH /projects/CHK": { status: 200, body: { project: aProject({ settings: { ...checkout.settings, library: true } }) } },
    });
    await renderAt("/projects/CHK/settings/details");
    await userEvent.click(await screen.findByRole("checkbox", { name: "This project is a library" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect((sent.find((r) => r.method === "PATCH")?.body as { settings: { library: boolean } }).settings.library).toBe(true));
  });

  it("shows a member the settings without a way to change them", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: aProject({ can: { ...checkout.can, administer: false, purge: false } }) } } });
    await renderAt("/projects/CHK/settings/details", { me: member });
    expect(await screen.findByText("Only the project's administrators change its settings. Ask one of them, or ask for the role.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Archive project" })).toBeNull();
    const tabs = screen.getAllByRole("tab").map((tab) => tab.getAttribute("data-settings-tab"));
    expect(tabs).toEqual(["details", "types", "attributes", "fields", "workflow", "approval", "links", "trace", "quality", "risks", "templates", "trash"]);
  });

  it("archives, and deletes an archived project only after asking", async () => {
    let project = checkout;
    const sent = stubApi({
      "GET /projects/CHK": () => ({ status: 200, body: { project } }),
      "PUT /projects/CHK/archive": () => {
        project = aProject({ archivedAt: "2026-10-12T08:00:00Z" });
        return { status: 200, body: { project } };
      },
      "DELETE /projects/CHK/archive": () => {
        project = checkout;
        return { status: 200, body: { project } };
      },
      "DELETE /projects/CHK": { status: 204 },
    });
    const router = await renderAt("/projects/CHK/settings/details");
    expect(screen.queryByRole("button", { name: "Delete project" })).toBeNull();
    await userEvent.click(await screen.findByRole("button", { name: "Archive project" }));
    expect(await screen.findByText(/This project is archived/)).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "Undo" }));
    await waitFor(() => expect(screen.queryByText(/This project is archived/)).toBeNull());
    expect(await screen.findByText("Checkout is back from the archive.")).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "Archive project" }));
    expect(await screen.findByText(/This project is archived/)).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    await userEvent.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = await answerConfirm(false);
    expect(dialog.textContent).toMatch(/Checkout \(CHK\) and everything in it/);
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/projects/CHK")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Delete project" }));
    await answerConfirm(true);
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
      "GET /projects/BIL/requirements": { status: 200, body: { rows: [], total: 0, limit: 50, offset: 0 } },
    });
    const router = await renderAt("/projects/CHK/settings/details");
    const nav = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-project-nav="CHK"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(nav).queryByRole("button", { name: "Project admin" })).toBeNull();
    expect(within(nav).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/projects/CHK/settings");
    await userEvent.click(within(nav).getByRole("button", { name: /Switch project/ }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Billing (BIL)" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/BIL"));
    await arrival(router, "/projects/BIL");
  });

  it("shows the last project visited where the route names none", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: checkout } },
      "GET /projects": { status: 200, body: { projects: [checkout] } },
    });
    const router = await renderAt("/projects/CHK/settings/details");
    await waitFor(() => expect(document.querySelector('[data-project-nav="CHK"]')).not.toBeNull());
    await router.navigate({ to: "/search" });
    await arrival(router, "/search");
    expect(document.querySelector('[data-project-nav="CHK"]')).not.toBeNull();
  });
});

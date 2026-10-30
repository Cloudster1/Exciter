import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { arrival, renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { PROJECT_SETTINGS_SECTIONS, movedProjectAddress, sectionsFor, settingsAddress } from "./settingsSections";

afterEach(() => vi.unstubAllGlobals());

const checkout = aProject();
const member = aProject({ can: { ...checkout.can, administer: false, purge: false }, roles: ["editor"] });

function shown(section: string) {
  return waitFor(() => {
    const found = document.querySelector<HTMLElement>(`[data-settings-section="${section}"]`);
    expect(found).not.toBeNull();
    return found!;
  });
}

describe("where a project's settings live", () => {
  it("sends an old /p/ address to /projects/ with its search and hash", () => {
    expect(movedProjectAddress("CHK")).toBe("/projects/CHK");
    expect(movedProjectAddress("CHK/r/CHK-R1/history", "?tab=activity", "x")).toBe("/projects/CHK/r/CHK-R1/history?tab=activity#x");
    expect(movedProjectAddress("CHK/settings", "?tab=links")).toBe("/projects/CHK/settings?tab=links");
    expect(movedProjectAddress("CHK/permissions")).toBe("/projects/CHK/settings/permissions");
    expect(movedProjectAddress("CHK/trash")).toBe("/projects/CHK/settings/trash");
  });

  it("opens the section an old tab named, and Details for any other", () => {
    expect(settingsAddress("CHK", "reqif")).toBe("/projects/CHK/settings/reqif");
    expect(settingsAddress("CHK", undefined)).toBe("/projects/CHK/settings/details");
    expect(settingsAddress("CHK", "nothing", "add")).toBe("/projects/CHK/settings/details#add");
  });

  it("lists every section to an administrator and hides those only administrators use from the rest", () => {
    expect(sectionsFor(checkout)).toEqual([...PROJECT_SETTINGS_SECTIONS]);
    expect(sectionsFor(member)).not.toContain("permissions");
    expect(sectionsFor(member)).not.toContain("import");
    expect(sectionsFor(member)).toContain("trash");
  });
});

describe("a project's settings", () => {
  it.each(PROJECT_SETTINGS_SECTIONS)("open the %s section by its own address", async (section) => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    const router = await renderAt(`/projects/CHK/settings/${section}`);
    await shown(section);
    expect(screen.getByRole("tab", { selected: true })).toHaveAttribute("data-settings-tab", section);
    await arrival(router, `/projects/CHK/settings/${section}`);
  });

  it("move to a section's address when its tab is picked, and step back with Back", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    const router = await renderAt("/projects/CHK/settings/details");
    await shown("details");
    await userEvent.click(screen.getByRole("tab", { name: "Links" }));
    await arrival(router, "/projects/CHK/settings/links");
    await shown("links");
    router.history.back();
    await arrival(router, "/projects/CHK/settings/details");
    await shown("details");
  });

  it("open the section an old ?tab= named, and the redirect from /p/ keeps it", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    const router = await renderAt("/p/CHK/settings?tab=quality");
    await arrival(router, "/projects/CHK/settings/quality");
    await shown("quality");
  });

  it("are read-only to a member, who is told who to ask for a section only administrators use", async () => {
    const sent = stubApi({ "GET /projects/CHK": { status: 200, body: { project: member } } });
    const router = await renderAt("/projects/CHK/settings/import");
    const refused = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-settings-refused="import"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(refused).toHaveTextContent("Only the project's administrators change its settings");
    expect(refused).toHaveTextContent("Ask one of them, or ask for the role.");
    expect(document.querySelector("[data-settings-read-only]")).toBeNull();
    expect(screen.queryByRole("tab", { name: "Import" })).toBeNull();
    // None of the tabs is the one shown, so the first keeps the list reachable by keyboard.
    expect(screen.getByRole("tab", { name: "Details" })).toHaveAttribute("tabindex", "0");
    expect(sent.some((r) => r.path.startsWith("/projects/CHK/imports"))).toBe(false);
    await arrival(router, "/projects/CHK/settings/import");
  });

  it("name the project, then Settings, in the crumbs, and Settings leads back from the workflow designer", async () => {
    stubApi({ "GET /projects/CHK": { status: 200, body: { project: checkout } } });
    const router = await renderAt("/projects/CHK/settings/types");
    await shown("types");
    const crumbs = () => within(document.querySelector<HTMLElement>("[data-shell-strip]")!).getByRole("navigation", { name: "Breadcrumb" });
    await waitFor(() => expect(within(crumbs()).getByRole("link", { name: "Checkout" })).toHaveAttribute("href", "/projects/CHK"));
    expect(within(crumbs()).getByText("Settings")).toHaveAttribute("aria-current", "page");

    await router.navigate({ to: "/projects/$projectKey/settings/workflows/$workflowId", params: { projectKey: "CHK", workflowId: "new" } });
    await arrival(router, "/projects/CHK/settings/workflows/new");
    await waitFor(() => expect(within(crumbs()).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/projects/CHK/settings/workflow"));
  });
});

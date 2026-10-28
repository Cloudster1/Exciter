import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NEW_PROJECT_PATH } from "@/config";
import { t } from "@/i18n";
import { arrival, renderAt, signedIn, stubApi } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const noProjects = { "GET /projects": { status: 200, body: { projects: [] } } };

describe("the home screen with no projects", () => {
  it("leads an administrator to the new project form", async () => {
    stubApi(noProjects);
    const router = await renderAt("/", { ready: "[data-home]" });
    expect(await screen.findByText(t.home.emptyTitle)).toBeInTheDocument();
    expect(screen.getByText(t.home.emptyBody)).toBeInTheDocument();
    expect(screen.queryByText(t.home.emptyBodyMember)).not.toBeInTheDocument();
    const first = screen.getByRole("button", { name: t.projects.create });
    expect(first).toHaveAttribute("data-action", "first-project");
    await userEvent.click(first);
    await arrival(router, NEW_PROJECT_PATH);
    expect(document.querySelector("[data-new-project]")).not.toBeNull();
  });

  it("tells a member an administrator makes the projects, and offers no button", async () => {
    stubApi(noProjects);
    await renderAt("/", { me: { ...signedIn, role: "member" }, ready: "[data-home]" });
    expect(await screen.findByText(t.home.emptyTitle)).toBeInTheDocument();
    expect(screen.getByText(t.home.emptyBodyMember)).toBeInTheDocument();
    expect(screen.queryByText(t.home.emptyBody)).not.toBeInTheDocument();
    expect(document.querySelector('[data-action="first-project"]')).toBeNull();
    expect(screen.queryByRole("button", { name: t.projects.create })).not.toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NEW_PROJECT_PATH } from "@/config";
import { t } from "@/i18n";
import { arrival, renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { aProject } from "@/test/projects";

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, role: "member" as const };
const failed: Answer = {
  status: 500,
  body: { error: { code: "internal", message: "Something went wrong on our side. Try again, and quote the request id if it keeps happening." } },
};

const home = () => document.querySelector<HTMLElement>("[data-home]")!;
const projectsAsked = (sent: { method: string; path: string }[]) => sent.filter((each) => each.method === "GET" && each.path === "/projects").length;

describe("the home screen's states", () => {
  it("offers an administrator with no projects the first one, which opens the new project form", async () => {
    stubApi({ "GET /projects": { status: 200, body: { projects: [] } } });
    const router = await renderAt("/", { ready: "[data-home]" });
    expect(await within(home()).findByText(t.home.emptyTitle)).toBeInTheDocument();
    expect(within(home()).getByText(t.home.emptyBody)).toBeInTheDocument();
    const first = within(home()).getByRole("button", { name: t.projects.create });
    expect(first).toHaveAttribute("data-action", "first-project");

    await userEvent.click(first);
    await arrival(router, NEW_PROJECT_PATH);
    expect(router.state.location.pathname).toBe(NEW_PROJECT_PATH);
  });

  it("tells a member with no projects that an administrator makes them, with no button", async () => {
    stubApi({ "GET /projects": { status: 200, body: { projects: [] } } });
    await renderAt("/", { me: member, ready: "[data-home]" });
    expect(await within(home()).findByText(t.home.emptyTitle)).toBeInTheDocument();
    expect(within(home()).getByText(t.home.emptyBodyMember)).toBeInTheDocument();
    expect(within(home()).queryByText(t.home.emptyBody)).toBeNull();
    expect(home().querySelector('[data-action="first-project"]')).toBeNull();
  });

  it("shows a failed read of the projects, and asks again on Retry", async () => {
    let answer = failed;
    const sent = stubApi({ "GET /projects": () => answer });
    await renderAt("/", { ready: "[data-home]" });
    const banner = await within(home()).findByRole("alert");
    expect(banner).toHaveTextContent("Something went wrong on our side.");
    expect(within(home()).queryByText(t.home.emptyTitle)).toBeNull();
    const before = projectsAsked(sent);

    answer = { status: 200, body: { projects: [aProject()] } };
    await userEvent.click(within(banner).getByRole("button", { name: t.kit.retry }));
    await waitFor(() => expect(home().querySelector('[data-project-row="CHK"]')).not.toBeNull());
    expect(projectsAsked(sent)).toBe(before + 1);
    expect(within(home()).queryByRole("alert")).toBeNull();
  });

  it("lists the projects under Your projects, with no empty state", async () => {
    stubApi({
      "GET /projects": { status: 200, body: { projects: [aProject(), aProject({ id: "0195f000-0000-7000-8000-0000000000f2", key: "BRK", name: "Brakes" })] } },
    });
    await renderAt("/", { ready: "[data-home]" });
    const section = await within(home()).findByRole("region", { name: t.home.yourProjects });
    await waitFor(() => expect(section.querySelectorAll("[data-project-row]")).toHaveLength(2));
    expect(Array.from(section.querySelectorAll("[data-project-row]")).map((row) => row.getAttribute("data-project-row"))).toEqual(["CHK", "BRK"]);
    expect(within(section).getByText("Brakes")).toBeInTheDocument();
    expect(within(home()).queryByText(t.home.emptyTitle)).toBeNull();
    expect(home().querySelector('[data-action="first-project"]')).toBeNull();
  });
});

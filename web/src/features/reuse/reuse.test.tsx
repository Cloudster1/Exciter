import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Requirement } from "@/api/requirements";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Reuse and variants (#49): the banner of a reused requirement, its editor while it follows, and reusing a library's
// requirement in another project.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const product = aProject({ key: "APP", name: "Product" });
const library = aProject({ id: "0195f000-0000-7000-8000-0000000000f2", key: "LIB", name: "Library", settings: { ...aProject().settings, library: true } });
const source = {
  id: "0195f000-0000-7000-8000-0000000000b1",
  key: "LIB-R2",
  projectKey: "LIB",
  kind: "requirement" as const,
  title: "Brakes engage",
  revision: 3,
  status: null,
  trashed: false,
};

function reused(over: Partial<Requirement> = {}): Requirement {
  return aRequirement({ key: "APP-R1", projectKey: "APP", title: "Brakes engage", reuse: { state: "following", revision: 2, source, behind: true }, ...over });
}

function stubPage(requirement: Requirement, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /projects/APP": { status: 200, body: { project: product } },
    [`GET /requirements/${requirement.key}`]: { status: 200, body: { requirement } },
    ...more,
  });
}

describe("a reused requirement", () => {
  it("says what it follows and that its source moved on, and takes the source's latest revision", async () => {
    const taken = reused({ revision: 2, reuse: { state: "following", revision: 3, source, behind: false } });
    const sent = stubPage(reused(), { "POST /requirements/APP-R1/reuse/take": { status: 200, body: { requirement: taken } } });
    await renderAt("/p/APP/r/APP-R1");
    const banner = await screen.findByText("Follows LIB-R2 from the library, at its revision 2.");
    expect(banner.closest("a")).toHaveAttribute("href", "/p/LIB/r/LIB-R2");
    expect(screen.getByText("LIB-R2 has revision 3 now.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "What changed in the source" })).toHaveAttribute("href", "/p/LIB/r/LIB-R2/history?from=2&to=3");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Take revision 3" }));
    await waitFor(() => expect(sent.find((r) => r.path === "/requirements/APP-R1/reuse/take")?.body).toEqual({ revision: 3 }));
    expect(await screen.findByText("Took revision 3 of the source.")).toBeInTheDocument();
  });

  it("is edited without its words while it follows, and made a variant to change them", async () => {
    const variant = reused({ reuse: { state: "variant", revision: 2, source, behind: true } });
    const sent = stubPage(reused(), { "POST /requirements/APP-R1/reuse/variant": { status: 200, body: { requirement: variant } } });
    await renderAt("/p/APP/r/APP-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const form = document.querySelector<HTMLElement>("[data-requirement-edit]")!;
    expect(within(form).getByText(/follow the library source/)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Title/)).toHaveAttribute("readonly");
    expect(within(form).queryByRole("textbox", { name: "Statement" })).toBeNull();
    await userEvent.click(within(form).getByRole("button", { name: "Cancel" }));
    await userEvent.click(screen.getByRole("button", { name: "Make a variant" }));
    await waitFor(() => expect(sent.some((r) => r.path === "/requirements/APP-R1/reuse/variant")).toBe(true));
    expect(await screen.findByText("This is a variant now. Its words are its own.")).toBeInTheDocument();
  });

  it("tells a reader who may not read the library what it follows without naming it", async () => {
    stubPage(
      reused({
        reuse: { state: "variant", revision: 2, source: null, behind: false },
        can: { edit: false, transition: false, delete: false, link: false, comment: false },
      }),
    );
    await renderAt("/p/APP/r/APP-R1");
    expect(await screen.findByText("A variant of a library requirement you may not read, branched at its revision 2.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Take revision/ })).toBeNull();
  });
});

describe("reusing a library's requirement", () => {
  it("offers the other projects the reader edits, and opens the copy it made", async () => {
    const libReq = aRequirement({ key: "LIB-R2", projectKey: "LIB", title: "Brakes engage" });
    const made = reused({ reuse: { state: "following", revision: 1, source, behind: false } });
    const sent = stubApi({
      "GET /projects/LIB": { status: 200, body: { project: library } },
      "GET /requirements/LIB-R2": { status: 200, body: { requirement: libReq } },
      "GET /projects": { status: 200, body: { projects: [library, product] } },
      "GET /projects/APP/tree": { status: 200, body: { nodes: [] } },
      "POST /requirements/LIB-R2/reuse": { status: 201, body: { requirement: made, copied: 1 } },
      "GET /projects/APP": { status: 200, body: { project: product } },
      "GET /requirements/APP-R1": { status: 200, body: { requirement: made } },
    });
    await renderAt("/p/LIB/r/LIB-R2");
    await userEvent.click(await screen.findByRole("button", { name: "Reuse in a project" }));
    const dialog = await screen.findByRole("dialog", { name: "Reuse LIB-R2" });
    const projects = within(dialog).getByLabelText("Project");
    expect(
      within(projects)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Product (APP)"]);
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Reuse" }));
    await waitFor(() =>
      expect(sent.find((r) => r.path === "/requirements/LIB-R2/reuse")?.body).toEqual({
        projectKey: "APP",
        place: { parentId: null, afterId: null },
        children: true,
      }),
    );
    expect(await screen.findByText("Follows LIB-R2 from the library, at its revision 1.")).toBeInTheDocument();
  });
});

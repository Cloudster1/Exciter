import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CodeLink, CodeTag } from "@/api/code";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Code tags (#201): the places that implement or verify a requirement, the project's outdated and orphaned tags, and
// the words tags use.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function aLink(over: Partial<CodeLink> = {}): CodeLink {
  return {
    repository: "acme/checkout",
    branch: "main",
    commit: "4f2c9a1e7b3d",
    path: "brakes/engage.go",
    line: 12,
    word: "implements",
    relation: "implements",
    revision: 1,
    outdated: false,
    counts: true,
    url: "https://git.example/acme/checkout/blob/4f2c9a1e7b3d/brakes/engage.go#L12",
    scannedAt: "2026-10-17T10:00:00Z",
    ...over,
  };
}

describe("a requirement's code", () => {
  it("lists each file and line by branch, linking to it, an outdated tag marked", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
      "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
      "GET /requirements/CHK-R1/code-links": {
        status: 200,
        body: {
          links: [aLink(), aLink({ path: "brakes/engage_test.go", line: 3, word: "verifies", relation: "verifies", outdated: true })],
          countingBranch: "main",
        },
      },
    });
    await renderAt("/projects/CHK/r/CHK-R1");
    const panel = (await screen.findByRole("heading", { name: "Code" })).closest("[data-code-links]") as HTMLElement;
    expect(await within(panel).findByRole("link", { name: "brakes/engage.go:12" })).toHaveAttribute(
      "href",
      "https://git.example/acme/checkout/blob/4f2c9a1e7b3d/brakes/engage.go#L12",
    );
    expect(within(panel).getByText("Outdated: written against revision 1")).toBeInTheDocument();
    expect(within(panel).getByText("Verifies")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the project's code tags", () => {
  const outdated: CodeTag = {
    ...aLink({ outdated: true }),
    key: "CHK-R1",
    requirement: {
      id: aRequirement().id,
      key: "CHK-R1",
      projectKey: "CHK",
      kind: "requirement",
      title: "Brakes engage",
      revision: 3,
      status: null,
      trashed: false,
    },
    state: "outdated",
  };
  const orphaned: CodeTag = { ...aLink({ path: "lights.go", line: 4 }), key: "CHK-R99", requirement: null, state: "orphaned", problem: "unknown" };

  it("lists the outdated and orphaned ones, and keeps one state on request", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/evidence-settings": { status: 200, body: { settings: { countingBranch: "main", updatedAt: null } } },
      "GET /projects/CHK/code-tags": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { tags: [outdated, orphaned], total: 2, limit: 50, offset: 0 } };
      },
    });
    await renderAt("/projects/CHK/code-tags");
    const table = await screen.findByRole("table", { name: "Code tags" });
    expect(within(table).getByText("Outdated: revision 1 of 3")).toBeInTheDocument();
    expect(within(table).getByText("names no requirement")).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: /CHK-R1/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Show" }), "orphaned");
    await waitFor(() => expect(asked.at(-1)?.get("state")).toBe("orphaned"));
  });
});

describe("the code tag words", () => {
  it("are set by an administrator, a word to each relation", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [] } },
      "GET /projects/CHK/trace-model": { status: 200, body: { model: { levels: [], updatedAt: null, updatedBy: null, can: { edit: true } } } },
      "GET /projects/CHK/evidence-settings": { status: 200, body: { settings: { countingBranch: "main", updatedAt: null } } },
      "GET /projects/CHK/code-tag-words": {
        status: 200,
        body: { words: { implements: ["implements"], verifies: ["verifies"], projectKeys: ["CHK"], updatedAt: null } },
      },
      "PUT /projects/CHK/code-tag-words": {
        status: 200,
        body: { words: { implements: ["implements", "satisfies"], verifies: ["verifies"], projectKeys: ["CHK"], updatedAt: "2026-10-17T10:00:00Z" } },
      },
    });
    await renderAt("/projects/CHK/settings/trace");
    const words = await screen.findByRole("textbox", { name: "Words for implements" });
    expect(words).toHaveValue("implements");
    await userEvent.type(words, ", satisfies");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(screen.getByRole("form", { name: "Code tag words" })).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(sent.find((each) => each.method === "PUT" && each.path.endsWith("code-tag-words"))?.body).toEqual({
        implements: ["implements", "satisfies"],
        verifies: ["verifies"],
      }),
    );
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DuplicatePair, SimilarRequirement } from "@/api/duplicates";
import { DUPLICATES_PAGE_SIZE } from "@/config";
import { arrival, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { derives, relates } from "@/test/links";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// Duplicates (#208): the hint below the statement while writing, the project's page of likely duplicates with its
// decisions, and the threshold in the quality settings.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// The hint waits out the pause the writing rules wait out, so finding it allows for that.
const PAUSED = { timeout: 3000 };

const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const ref = (key: string, title: string, projectKey = "CHK") => ({
  id: `0195f000-0000-7000-8000-0000000${key.replace(/\D/g, "").padStart(5, "0")}`,
  key,
  kind: "requirement" as const,
  projectKey,
  revision: 1,
  status: null,
  title,
  trashed: false,
});
const hit = (key: string, title: string, score: number, library = false, projectKey = "CHK"): SimilarRequirement => ({
  requirement: ref(key, title, projectKey),
  score,
  library,
});
const reader = aProject({ can: { view: true, comment: true, edit: false, approve: false, administer: false, purge: false } });

describe("the hint below the statement", () => {
  it("lists what the draft is like, most alike first, and says nothing while there is none", async () => {
    const sent = stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "POST /projects/CHK/similar": async (request) => {
        const body = (await request.json()) as { title: string };
        const similar = body.title.startsWith("Brakes")
          ? [hit("CHK-R1", "Brakes engage within 100 ms", 0.92), hit("CHK-R2", "Brakes engage within 100 milliseconds", 0.71)]
          : [];
        return { status: 200, body: { similar, threshold: 0.6 } };
      },
    });
    await renderAt("/projects/CHK/new");
    const title = await screen.findByLabelText("Title");
    await userEvent.type(title, "Wipers");
    await waitFor(() => expect(sent.some((r) => r.path === "/projects/CHK/similar")).toBe(true), PAUSED);
    expect(screen.queryByRole("region", { name: "Similar requirements" })).toBeNull();

    await userEvent.clear(title);
    await userEvent.type(title, "Brakes engage within 100 ms");
    const hint = await screen.findByRole("region", { name: "Similar requirements" }, PAUSED);
    expect(document.querySelector("[data-similar-hint] [role=status]")).toHaveTextContent("2 similar requirements.");
    const items = within(hint).getAllByRole("listitem");
    expect(items.map((item) => item.getAttribute("data-similar"))).toEqual(["CHK-R1", "CHK-R2"]);
    expect(within(items[0]!).getByRole("link", { name: /CHK-R1 Brakes engage within 100 ms/ })).toHaveAttribute("target", "_blank");
    expect(within(items[0]!).getByText("92% alike")).toBeInTheDocument();
    expect(within(hint).queryByText("Library")).toBeNull();
    expect(within(hint).queryByRole("button")).toBeNull();
    expect(sent.filter((r) => r.path === "/projects/CHK/similar").at(-1)?.body).toMatchObject({ title: "Brakes engage within 100 ms" });
    expect(await axeViolations()).toEqual([]);
  });

  it("offers to reuse a library's requirement instead, into this project", async () => {
    const library = aProject({ id: "0195f000-0000-7000-8000-0000000000f2", key: "LIB", name: "Brake library" });
    library.settings = { ...library.settings, library: true };
    const reused = aRequirement({ key: "CHK-R7", number: 7, title: "Brakes engage within 100 ms" });
    const sent = stubApi({
      "GET /projects": { status: 200, body: { projects: [aProject(), library] } },
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/tree": { status: 200, body: { nodes: [] } },
      "POST /projects/CHK/similar": { status: 200, body: { similar: [hit("LIB-R4", "Brakes engage within 100 ms", 0.88, true, "LIB")], threshold: 0.6 } },
      "POST /requirements/LIB-R4/reuse": { status: 201, body: { requirement: reused, requirements: [reused] } },
      "GET /requirements/CHK-R7": { status: 200, body: { requirement: reused } },
    });
    const router = await renderAt("/projects/CHK/new");
    await userEvent.type(await screen.findByLabelText("Title"), "Brakes engage");
    const hint = await screen.findByRole("region", { name: "Similar requirements" }, PAUSED);
    expect(within(hint).getByText("Library")).toHaveAttribute("title", "From the library LIB");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(hint).getByRole("button", { name: "Reuse LIB-R4 instead" }));
    const dialog = await screen.findByRole("dialog", { name: "Reuse LIB-R4" });
    expect(within(dialog).getByRole("combobox", { name: "Project" })).toHaveValue("CHK");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reuse" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/CHK/r/CHK-R7"));
    await arrival(router, "/projects/CHK/r/CHK-R7");
    expect(sent.find((r) => r.path === "/requirements/LIB-R4/reuse")?.body).toMatchObject({ projectKey: "CHK", children: true });
  });
});

const pair: DuplicatePair = {
  a: ref("CHK-R1", "Brakes engage within 100 ms"),
  b: ref("CHK-R2", "Brakes engage within 100 milliseconds"),
  score: 0.84,
};
const first = aRequirement({ id: pair.a.id, key: "CHK-R1", title: pair.a.title, statement: doc("The brakes shall engage within 100 ms.") });
const second = aRequirement({
  id: pair.b.id,
  key: "CHK-R2",
  number: 2,
  title: pair.b.title,
  statement: doc("The brakes shall engage within 100 milliseconds."),
});

function pageStubs(listed: () => DuplicatePair[], extra: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/duplicates": () => ({ status: 200, body: { pairs: listed(), next: "", threshold: 0.6 } }),
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: first } },
    "GET /requirements/CHK-R2": { status: 200, body: { requirement: second } },
    ...extra,
  });
}

describe("the duplicates page", () => {
  it("shows each pair side by side with what differs, and dismisses one with a reason", async () => {
    let dismissed = false;
    const sent = pageStubs(() => (dismissed ? [] : [pair]), {
      "POST /projects/CHK/duplicates/dismiss": () => {
        dismissed = true;
        return { status: 201, body: { decision: {} } };
      },
    });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    expect(within(card).getByText("84% alike")).toBeInTheDocument();
    expect(screen.getByText("Listed from 60% alike, as the project's quality settings set it.")).toBeInTheDocument();
    expect(await within(card).findByText("The brakes shall engage within 100 milliseconds.")).toBeInTheDocument();
    const differences = card.querySelector("[data-pair-differences]")!;
    expect(differences.querySelector('del[data-only-in="CHK-R1"]')).toHaveTextContent("ms.");
    expect(differences.querySelector('ins[data-only-in="CHK-R2"]')).toHaveTextContent("milliseconds.");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(card).getByRole("button", { name: "Dismiss CHK-R1 and CHK-R2" }));
    const dialog = await screen.findByRole("dialog", { name: "Dismiss the pair" });
    expect(within(dialog).getByRole("button", { name: "Dismiss" })).toBeDisabled();
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Reason" }), "One is about the front axle.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Dismiss" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({
        a: { key: "CHK-R1", revision: 1 },
        b: { key: "CHK-R2", revision: 1 },
        reason: "One is about the front axle.",
      }),
    );
    expect(await screen.findByText("CHK-R1 and CHK-R2 are no longer listed as duplicates.")).toBeInTheDocument();
    expect(await screen.findByText("No likely duplicates.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "CHK-R1 and CHK-R2" })).toBeNull();
  });

  it("says when a side changed since the pairs were read, and reads them again", async () => {
    const sent = pageStubs(() => [pair], {
      "POST /projects/CHK/duplicates/dismiss": {
        status: 409,
        body: { error: { code: "stale_revision", message: "One of the two changed since you read them. Read the pair again, then decide." } },
      },
    });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    await userEvent.click(within(card).getByRole("button", { name: "Dismiss CHK-R1 and CHK-R2" }));
    const dialog = await screen.findByRole("dialog", { name: "Dismiss the pair" });
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Reason" }), "Not the same.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Dismiss" }));
    expect(await within(dialog).findByText("One of the two changed since the pairs were read. Read them again, then decide.")).toBeInTheDocument();
    const reads = sent.filter((r) => r.path === "/projects/CHK/duplicates").length;
    await userEvent.click(within(dialog).getByRole("button", { name: "Read the pairs again" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(sent.filter((r) => r.path === "/projects/CHK/duplicates").length).toBeGreaterThan(reads));
  });

  it("merges a pair into the side kept and lists which links moved and which were skipped", async () => {
    let merged = false;
    const sent = pageStubs(() => (merged ? [] : [pair]), {
      "POST /projects/CHK/duplicates/merge": () => {
        merged = true;
        return {
          status: 200,
          body: {
            merge: {
              decision: {},
              links: [
                { linkTypeKey: "refines", otherKey: "CHK-R9", outgoing: true },
                { linkTypeKey: "relates", otherKey: "CHK-R2", outgoing: false, skipped: "self" },
              ],
            },
          },
        };
      },
    });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    await userEvent.click(within(card).getByRole("button", { name: "Merge CHK-R1 and CHK-R2" }));
    const dialog = await screen.findByRole("dialog", { name: "Merge the two" });
    await userEvent.click(within(within(dialog).getByRole("group", { name: "Keep" })).getByRole("button", { name: "CHK-R2" }));
    expect(within(dialog).getByText("CHK-R1 goes to the trash.")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Comment" }), "Same braking time.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Merge" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({
        keep: { key: "CHK-R2", revision: 1 },
        trash: { key: "CHK-R1", revision: 1 },
        comment: "Same braking time.",
      }),
    );
    const result = await screen.findByRole("dialog", { name: "Merge the two" });
    expect(await within(result).findByText("refines to CHK-R9")).toBeInTheDocument();
    expect(within(result).getByText("Made again on the one kept.")).toBeInTheDocument();
    expect(within(result).getByText("Skipped: It would link the requirement kept to itself.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(result).getByRole("button", { name: "Done" }));
    expect(await screen.findByText("No likely duplicates.")).toBeInTheDocument();
    expect(screen.getByText("CHK-R1 is in the trash, and CHK-R2 is kept.")).toBeInTheDocument();
  });

  it("links a pair by the way chosen and takes it off the list", async () => {
    let linked = false;
    const retired = { ...relates, id: "0195f000-0000-7000-8000-0000000001a9", key: "old", retired: true };
    const sent = pageStubs(() => (linked ? [] : [pair]), {
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [derives, relates, retired] } },
      "POST /projects/CHK/duplicates/link": () => {
        linked = true;
        return { status: 201, body: { decision: {} } };
      },
    });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    await userEvent.click(within(card).getByRole("button", { name: "Link CHK-R1 and CHK-R2" }));
    const dialog = await screen.findByRole("dialog", { name: "Link the two" });
    const way = within(dialog).getByRole("combobox", { name: "Link" });
    // A retired type offers no way, and a symmetric one reads the same both ways, so it is offered once.
    expect(
      within(way)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["CHK-R1 derives from CHK-R2", "CHK-R1 is derived into CHK-R2", "CHK-R1 relates to CHK-R2"]);
    await userEvent.selectOptions(way, "CHK-R1 is derived into CHK-R2");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Link" }));
    // The incoming way makes the second side the link's source.
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")?.body).toEqual({
        source: { key: "CHK-R2", revision: 1 },
        target: { key: "CHK-R1", revision: 1 },
        linkTypeKey: "derives",
      }),
    );
    expect(await screen.findByText("CHK-R1 and CHK-R2 are linked.")).toBeInTheDocument();
    expect(await screen.findByText("No likely duplicates.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("shows a link type the project refuses where the way is chosen", async () => {
    const refused = "CHK does not use the link type Relates. Pick another, or ask an administrator of CHK to enable it.";
    pageStubs(() => [pair], {
      "GET /projects/CHK/link-types": { status: 200, body: { linkTypes: [relates] } },
      "POST /projects/CHK/duplicates/link": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields are not valid.", fields: { linkTypeKey: refused } } },
      },
    });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    await userEvent.click(within(card).getByRole("button", { name: "Link CHK-R1 and CHK-R2" }));
    const dialog = await screen.findByRole("dialog", { name: "Link the two" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Link" }));
    expect(await within(dialog).findByText(refused)).toBeInTheDocument();
    expect(within(dialog).getByRole("combobox", { name: "Link" })).toHaveAccessibleDescription(refused);
  });

  it("reads the next page with the cursor the last one answered, and narrows the pairs by a section, a type and a query", async () => {
    const third = { a: ref("CHK-R3", "Lights dim at dusk"), b: ref("CHK-R4", "Lights dim at dusk automatically"), score: 0.7 };
    const functional = { id: "0195f000-0000-7000-8000-0000000000c1", name: "Functional", retired: false };
    const asked: URLSearchParams[] = [];
    pageStubs(() => [], {
      "GET /projects/CHK/types": { status: 200, body: { types: [functional] } },
      "GET /projects/CHK/duplicates": (request) => {
        const query = new URL(request.url).searchParams;
        asked.push(query);
        const later = Boolean(query.get("cursor"));
        return { status: 200, body: { pairs: later ? [third] : [pair], next: later ? "" : "1", threshold: 0.6 } };
      },
      "GET /requirements/CHK-R3": { status: 200, body: { requirement: aRequirement({ id: third.a.id, key: "CHK-R3", number: 3, title: third.a.title }) } },
      "GET /requirements/CHK-R4": { status: 200, body: { requirement: aRequirement({ id: third.b.id, key: "CHK-R4", number: 4, title: third.b.title }) } },
    });
    await renderAt("/projects/CHK/duplicates");
    await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    expect(asked[0]?.get("limit")).toBe(String(DUPLICATES_PAGE_SIZE));
    expect(asked[0]?.has("cursor")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "More" }));
    expect(await screen.findByRole("region", { name: "CHK-R3 and CHK-R4" })).toBeInTheDocument();
    expect(asked.at(-1)?.get("cursor")).toBe("1");
    expect(screen.getByRole("region", { name: "CHK-R1 and CHK-R2" })).toBeInTheDocument();
    expect(screen.getByText("2 pairs shown.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "More" })).toBeNull();

    const filters = screen.getByRole("form", { name: "Narrow the pairs" });
    await userEvent.selectOptions(within(filters).getByRole("combobox", { name: "Type" }), "Functional");
    await userEvent.type(within(filters).getByRole("textbox", { name: "Section" }), "chk-r9");
    await userEvent.click(within(filters).getByRole("button", { name: "Filter" }));
    await waitFor(() => expect(asked.at(-1)?.get("section")).toBe("CHK-R9"));
    expect(asked.at(-1)?.getAll("type")).toEqual([functional.id]);
    expect(asked.at(-1)?.has("cursor")).toBe(false);
    await userEvent.type(screen.getByRole("combobox", { name: "Query either side matches" }), 'title ~ "Lights"{Enter}');
    await waitFor(() => expect(asked.at(-1)?.get("query")).toBe('title ~ "Lights"'));
    expect(asked.at(-1)?.get("section")).toBe("CHK-R9");
  });

  it("lets a reader read the pairs without deciding", async () => {
    pageStubs(() => [pair], { "GET /projects/CHK": { status: 200, body: { project: reader } } });
    await renderAt("/projects/CHK/duplicates");
    const card = await screen.findByRole("region", { name: "CHK-R1 and CHK-R2" });
    expect(within(card).queryByRole("button", { name: /Dismiss|Link|Merge/ })).toBeNull();
    expect(screen.getByText("You can read the pairs. Deciding about them needs the right to edit the project.")).toBeInTheDocument();
  });
});

describe("the threshold", () => {
  const settings = { language: "en", updatedAt: null, rules: [], words: {} };
  const stubs = (project = aProject()) =>
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project } },
      "GET /projects/CHK/types": { status: 200, body: { types: [] } },
      "GET /projects/CHK/quality-settings": { status: 200, body: { settings } },
      "GET /quality-rules": { status: 200, body: { rules: [] } },
      "GET /projects/CHK/duplicate-settings": { status: 200, body: { settings: { threshold: 0.6, updatedAt: null } } },
      "PUT /projects/CHK/duplicate-settings": async (request) => {
        const body = (await request.json()) as { threshold: number };
        return { status: 200, body: { settings: { threshold: body.threshold, updatedAt: "2026-10-18T08:00:00Z" } } };
      },
    });

  it("is set by an administrator in the quality settings", async () => {
    const sent = stubs();
    await renderAt("/projects/CHK/settings/quality");
    const card = await screen.findByRole("form", { name: "Duplicates" });
    const threshold = within(card).getByRole("spinbutton", { name: "Similarity threshold in percent" });
    expect(threshold).toHaveValue(60);
    expect(within(card).getByText("From 30% to 95%, in steps of 5.")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Save threshold" })).toBeDisabled();
    await userEvent.clear(threshold);
    await userEvent.type(threshold, "75");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(card).getByRole("button", { name: "Save threshold" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ threshold: 0.75 }));
    expect(await screen.findByText("The threshold is saved.")).toBeInTheDocument();
  });

  it("is only read by somebody who does not administer the project", async () => {
    stubs(aProject({ can: { view: true, comment: true, edit: true, approve: true, administer: false, purge: false } }));
    await renderAt("/projects/CHK/settings/quality");
    const card = await screen.findByRole("form", { name: "Duplicates" });
    expect(within(card).getByRole("spinbutton", { name: "Similarity threshold in percent" })).toBeDisabled();
    expect(within(card).queryByRole("button", { name: "Save threshold" })).toBeNull();
    expect(screen.getByText("Only the project's administrators set the threshold.")).toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AvailableTransition } from "@/api/workflows";
import { renderAt, type stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { can, entry, entryOf, node, pay, scope, ship, stubDocument } from "@/test/spec";
import { readSpecAddress } from "./address";
import { nextChange } from "./DocumentChanges";
import { rangeOf, sharedTransitions } from "./DocumentStates";
import { addedPlace, editableOf, entriesBelow, lastBelow, stepTo } from "./SpecPage";

afterEach(() => vi.unstubAllGlobals());

describe("the document's pieces", () => {
  it("adds after an entry, or first below it", () => {
    expect(addedPlace(pay, "after")).toEqual({ parentId: "s", afterId: "p" });
    expect(addedPlace(scope, "below")).toEqual({ parentId: "s", afterId: null });
  });

  it("puts a requirement added after an entry below everything under it", () => {
    const entries = [entry(scope, ""), entry(pay, ""), entry(ship, ""), entry(node({ id: "o", key: "CHK-R4", title: "Other", outline: "2" }), "")];
    expect(lastBelow(entries, "CHK-R1")).toBe("CHK-R3");
    expect(lastBelow(entries, "CHK-R2")).toBe("CHK-R2");
    expect(lastBelow(entries, "CHK-R4")).toBe("CHK-R4");
  });

  it("edits an entry from what it holds", () => {
    const type = { id: "t", name: "Functional", icon: "test", colour: "#22aa55" };
    expect(editableOf(entry(node({ ...pay, revision: 4 }), "Pay at once.", { type }))).toMatchObject({
      key: "CHK-R2",
      kind: "requirement",
      title: "Pay",
      revision: 4,
      type,
    });
  });

  it("steps between entries and stops at either end", () => {
    const keys = ["A", "B", "C"];
    expect(stepTo(keys, "A", "ArrowDown")).toBe("B");
    expect(stepTo(keys, "C", "ArrowDown")).toBe("C");
    expect(stepTo(keys, "A", "ArrowUp")).toBe("A");
    expect(stepTo(keys, "B", "End")).toBe("C");
    expect(stepTo(keys, "B", "Home")).toBe("A");
    expect(stepTo(keys, "B", "x")).toBeUndefined();
  });
});

describe("editing in the document", () => {
  it("turns the entry into its editor and saves over the revision it was read at", async () => {
    const sent = stubDocument([entry(scope, "What is in scope."), entry(node({ ...pay, revision: 3 }), "Pay at once.")], {
      "PATCH /requirements/CHK-R2": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", title: "Pay by card", revision: 4 }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    const title = within(entryOf("CHK-R2")).getByLabelText("Title");
    expect(title).toHaveFocus();
    expect(title).toHaveValue("Pay");
    await userEvent.clear(title);
    await userEvent.type(title, "Pay by card");
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")?.body).toMatchObject({ title: "Pay by card", baseRevision: 3 }));
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("CHK-R2: Saved as revision 4."));
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
  });

  it("shows what a newer revision changed and saves over it", async () => {
    let revision = 1;
    let saves = 0;
    const sent = stubDocument([], {
      "GET /projects/CHK/spec": () => ({ status: 200, body: { entries: [entry(node({ ...pay, revision }), "Pay at once.")], total: 1, limit: 50, offset: 0 } }),
      "PATCH /requirements/CHK-R2": () => {
        saves++;
        if (saves === 1) {
          revision = 2;
          return { status: 409, body: { error: { code: "revision_conflict", message: "Somebody saved a newer revision of CHK-R2." } } };
        }
        return { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", revision: 3 }) } };
      },
      "GET /requirements/CHK-R2/diff": {
        status: 200,
        body: {
          comparison: {
            from: { number: 1, title: "Pay", type: null, authorName: "Alice", createdAt: "2026-10-01T10:00:00Z" },
            to: { number: 2, title: "Pay in cash", type: null, authorName: "Bob", createdAt: "2026-10-02T10:00:00Z" },
            title: [],
            statement: [],
            rationale: [],
            preconditions: [],
            steps: [],
            parameters: [],
            attributes: [],
          },
        },
      },
    });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await userEvent.type(screen.getByLabelText("Title"), " now");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const newer = await screen.findByRole("region", { name: "What revision 2 changed since you opened it" });
    expect(await within(newer).findByText("Pay in cash")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "GET" && r.path === "/requirements/CHK-R2/diff")).toBe(true);
    expect(screen.getByLabelText("Title")).toHaveValue("Pay now");
    await userEvent.click(screen.getByRole("button", { name: "Save over revision 2" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PATCH").map((r) => (r.body as { baseRevision: number }).baseRevision)).toEqual([1, 2]));
  });

  it("asks before it lets go of unsaved changes, and opens one entry at a time", async () => {
    stubDocument([entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    await userEvent.type(within(entryOf("CHK-R2")).getByLabelText("Title"), " later");
    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    const dialog = screen.getByRole("dialog", { name: "Discard your changes?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep editing" }));
    expect(within(entryOf("CHK-R2")).getByLabelText("Title")).toHaveValue("Pay later");
    expect(within(entryOf("CHK-R3")).queryByLabelText("Title")).toBeNull();

    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Discard" }));
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
    expect(within(entryOf("CHK-R3")).getByLabelText("Title")).toHaveValue("Ship");
  });

  it("opens a draft left unchanged without asking", async () => {
    stubDocument([entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(entryOf("CHK-R3")).getByRole("button", { name: "Edit" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(within(entryOf("CHK-R3")).getByLabelText("Title")).toBeInTheDocument();
  });

  it("adds a requirement after an entry, written where it will stand", async () => {
    const sent = stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once.")], {
      "POST /projects/CHK/requirements": { status: 201, body: { requirement: aRequirement({ key: "CHK-R4", title: "Refund" }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R2")).getByRole("button", { name: "Add next to CHK-R2" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "After this" }));
    const adding = document.body.querySelector<HTMLElement>("[data-spec-adding]")!;
    expect(within(adding).getByText("New, after CHK-R2")).toBeInTheDocument();
    await userEvent.type(within(adding).getByLabelText("Title"), "Refund");
    await userEvent.click(within(adding).getByRole("button", { name: "New requirement" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST" && r.path.endsWith("/requirements"))?.body).toMatchObject({
        title: "Refund",
        place: { parentId: "s", afterId: "p" },
      }),
    );
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("CHK-R4 added."));
  });

  it("writes a requirement added after a section below the section's last entry", async () => {
    stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once."), entry(ship, "Ship in a day.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    await userEvent.click(within(entryOf("CHK-R1")).getByRole("button", { name: "Add next to CHK-R1" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "After this" }));
    const adding = document.body.querySelector<HTMLElement>("[data-spec-adding]")!;
    expect(adding.previousElementSibling).toBe(entryOf("CHK-R3"));
  });

  it("moves between entries and edits from the keyboard", async () => {
    stubDocument([entry(scope, "What is in scope."), entry(pay, "Pay at once.")]);
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    entryOf("CHK-R1").focus();
    await userEvent.keyboard("{ArrowDown}");
    await waitFor(() => expect(entryOf("CHK-R2")).toHaveFocus());
    await userEvent.keyboard("{Enter}");
    expect(within(entryOf("CHK-R2")).getByLabelText("Title")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(within(entryOf("CHK-R2")).queryByLabelText("Title")).toBeNull();
    await waitFor(() => expect(entryOf("CHK-R2")).toHaveFocus());
    expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("Left CHK-R2 as it was.");
    expect(await axeViolations()).toEqual([]);
  });

  it("offers no editing to a reader, nor in an archived project", async () => {
    stubDocument([entry(pay, "Pay at once.", { can: { ...can, edit: false } })], {
      "GET /projects/CHK": { status: 200, body: { project: aProject({ archivedAt: "2026-10-01T10:00:00Z" }) } },
    });
    await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay" });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Add next to/ })).toBeNull();
    expect(screen.getByText("Arrow keys move between entries.")).toBeInTheDocument();
  });
});

const draft = { id: "st-draft", name: "Draft", category: "draft" as const };
const review = { id: "st-review", name: "In review", category: "review" as const };
const submit = {
  id: "tr-submit",
  name: "Submit for review",
  toStatus: { ...review, description: "", position: 1, requirements: 0, locks: false },
  needsComment: false,
};
const reject = {
  id: "tr-reject",
  name: "Reject",
  toStatus: { id: "st-rejected", name: "Rejected", category: "closed" as const, description: "", position: 5, requirements: 0, locks: false },
  needsComment: true,
};
const statuses = [draft, review].map((status, position) => ({ ...status, description: "", position, requirements: 0 }));

function stubStates(answers: Parameters<typeof stubApi>[0] = {}) {
  return stubDocument(
    [
      entry(scope, "What is in scope."),
      entry(node({ ...pay, status: draft }), "Pay at once."),
      entry(node({ ...ship, status: draft }), "Ship in a day."),
      entry(node({ id: "o", key: "CHK-R4", title: "Other", outline: "2", status: review }), "Other things."),
    ],
    { "GET /projects/CHK/statuses": { status: 200, body: { statuses } }, ...answers },
  );
}

describe("states in the document", () => {
  it("offers only the transitions every picked requirement has", () => {
    const answer = (key: string, transitions: AvailableTransition[]) => ({ key, transitions });
    expect(sharedTransitions([answer("A", [submit, reject]), answer("B", [reject])], ["A", "B"]).map((each) => each.id)).toEqual(["tr-reject"]);
    expect(sharedTransitions([answer("A", [submit])], ["A", "B"])).toEqual([]);
    expect(sharedTransitions([], [])).toEqual([]);
  });

  it("picks a range either way, and what is below a section", () => {
    expect(rangeOf(["A", "B", "C", "D"], "C", "A")).toEqual(["A", "B", "C"]);
    expect(rangeOf(["A", "B"], "X", "B")).toEqual(["B"]);
    const entries = [entry(scope, ""), entry(pay, ""), entry(ship, ""), entry(node({ id: "o", key: "CHK-R4" }), "")];
    expect(entriesBelow(entries, "CHK-R1").map((each) => each.node.key)).toEqual(["CHK-R2", "CHK-R3"]);
  });

  it("reads statuses and categories from the address, and drops the rest", () => {
    expect(readSpecAddress({ status: ["a", ""], category: ["review", "lost"] })).toEqual({ status: ["a"], category: ["review"] });
    expect(readSpecAddress({ category: "draft" })).toEqual({ category: ["draft"] });
    expect(readSpecAddress({})).toEqual({});
  });

  it("takes a transition from a requirement's status, asking why where it wants a comment", async () => {
    const sent = stubStates({
      "GET /requirements/CHK-R2/transitions": { status: 200, body: { transitions: [submit, reject] } },
      "POST /requirements/CHK-R2/transitions": { status: 200, body: { requirement: aRequirement({ key: "CHK-R2", status: { ...reject.toStatus } }) } },
    });
    await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: "CHK-R2 is Draft. Show its transitions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Reject: Rejected" }));
    const dialog = screen.getByRole("dialog", { name: "Reject" });
    await userEvent.type(within(dialog).getByLabelText("Why"), "Out of scope.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reject" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "POST")).toMatchObject({
        path: "/requirements/CHK-R2/transitions",
        body: { transitionId: "tr-reject", comment: "Out of scope." },
      }),
    );
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("CHK-R2 moved to Rejected."));
  });

  it("moves the picked requirements at once, keeps the refused ones picked and says why", async () => {
    const sent = stubStates({
      "GET /projects/CHK/transitions": {
        status: 200,
        body: {
          requirements: [
            { key: "CHK-R2", transitions: [submit, reject] },
            { key: "CHK-R3", transitions: [submit] },
          ],
        },
      },
      "POST /projects/CHK/transitions": {
        status: 200,
        body: {
          results: [
            { key: "CHK-R2", outcome: "taken", requirement: aRequirement({ key: "CHK-R2", status: { ...review } }), error: null },
            {
              key: "CHK-R3",
              outcome: "refused",
              requirement: null,
              error: { code: "transition_unavailable", message: "Somebody moved CHK-R3 to In review meanwhile." },
            },
          ],
        },
      },
    });
    await renderAt("/p/CHK/spec");
    expect(await screen.findByRole("checkbox", { name: "Select everything in Scope" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select CHK-R1" })).toBeNull();
    await userEvent.click(screen.getByRole("checkbox", { name: "Select CHK-R2" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select CHK-R3" }));
    const bar = document.body.querySelector<HTMLElement>("[data-selection-bar]")!;
    expect(within(bar).queryByRole("button", { name: "Reject" })).toBeNull();
    await userEvent.click(await within(bar).findByRole("button", { name: "Submit for review" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ transitionId: "tr-submit", keys: ["CHK-R2", "CHK-R3"] }));
    expect(await within(bar).findByText(/Somebody moved CHK-R3 to In review meanwhile\./)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select CHK-R3" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select CHK-R2" })).not.toBeChecked();
    expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("1 requirement moved to In review, 1 not moved.");
    expect(await axeViolations()).toEqual([]);
  });

  it("picks a range with Shift, and everything in a section at once", async () => {
    stubStates({ "GET /projects/CHK/transitions": { status: 200, body: { requirements: [] } } });
    const user = userEvent.setup();
    await renderAt("/p/CHK/spec");
    await user.click(await screen.findByRole("checkbox", { name: "Select CHK-R2" }));
    await user.keyboard("{Shift>}");
    await user.click(screen.getByRole("checkbox", { name: "Select CHK-R4" }));
    await user.keyboard("{/Shift}");
    for (const key of ["CHK-R2", "CHK-R3", "CHK-R4"]) expect(screen.getByRole("checkbox", { name: `Select ${key}` })).toBeChecked();
    await userEvent.click(screen.getByRole("button", { name: "Clear the selection" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select everything in Scope" }));
    expect(screen.getByRole("checkbox", { name: "Select CHK-R2" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select CHK-R3" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select CHK-R4" })).not.toBeChecked();
    expect(await screen.findByText("They share no transition you may take. Pick requirements in the same status and workflow.")).toBeInTheDocument();
  });

  it("narrows the document to statuses, and keeps that in the address", async () => {
    const asked: string[] = [];
    stubStates({
      "GET /projects/CHK/spec": (request) => {
        asked.push(new URL(request.url).searchParams.getAll("category").join(","));
        return { status: 200, body: { entries: [entry(node({ ...pay, status: review }), "Pay at once.")], total: 1, limit: 50, offset: 0 } };
      },
    });
    const router = await renderAt("/p/CHK/spec");
    await userEvent.click(await screen.findByRole("button", { name: "In review" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ category: ["review"] }));
    await waitFor(() => expect(asked).toContain("review"));
    await userEvent.selectOptions(screen.getByLabelText("Status"), "st-draft");
    await waitFor(() => expect(router.state.location.search).toEqual({ category: ["review"], status: ["st-draft"] }));
    await userEvent.click(screen.getByRole("button", { name: "Show every status" }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });
});

const side = (number: number, title: string) => ({ number, title, type: null, authorName: "Ada", createdAt: "2026-10-01T10:00:00Z" });
const marked = (text: string, mark: "diffInsert" | "diffDelete") => ({ type: "text", text, marks: [{ type: mark }] });
const payChange = {
  kind: "changed" as const,
  from: 1,
  to: 2,
  movedFrom: null,
  comparison: {
    from: side(1, "Pay"),
    to: side(2, "Pay by card"),
    title: [{ change: "modified" as const, node: { type: "paragraph", content: [{ type: "text", text: "Pay" }, marked(" by card", "diffInsert")] } }],
    statement: [
      {
        change: "modified" as const,
        node: { type: "paragraph", content: [{ type: "text", text: "Pay " }, marked("at once", "diffDelete"), marked("by card", "diffInsert")] },
      },
    ],
    rationale: [],
    preconditions: [],
    steps: [],
    parameters: [],
    attributes: [{ attributeId: "a1", name: "Risk", change: "modified" as const, from: "low", to: "high", fromText: "Low", toText: "High" }],
  },
};

function stubChanges(answers: Parameters<typeof stubApi>[0] = {}) {
  const asked: URLSearchParams[] = [];
  const sent = stubDocument([], {
    "GET /projects/CHK/spec": (request) => {
      const query = new URL(request.url).searchParams;
      asked.push(query);
      const base = query.get("changes");
      const entries = [
        entry(scope, "What is in scope.", { change: null }),
        entry(node({ ...pay, title: "Pay by card", revision: 2 }), "Pay by card.", { change: base ? payChange : null }),
        entry(node({ ...ship, revision: 1 }), "Ship in a day.", {
          change: base
            ? {
                kind: base === "baseline" ? ("moved" as const) : ("unchanged" as const),
                from: 1,
                to: 1,
                movedFrom: base === "baseline" ? "2" : null,
                comparison: null,
              }
            : null,
        }),
      ];
      const changes = base
        ? {
            base,
            baselineId: query.get("baseline"),
            changed: base === "baseline" ? 2 : 1,
            removed: base === "baseline" ? [{ key: "CHK-R9", title: "Gone", outline: "3", revision: 4 }] : [],
          }
        : null;
      return { status: 200, body: { entries, total: 3, limit: 50, offset: 0, changes } };
    },
    "GET /projects/CHK/baselines": { status: 200, body: { baselines: [{ id: "b1", name: "Release 1.0" }] } },
    ...answers,
  });
  return { sent, asked };
}

describe("changes in the document", () => {
  it("steps to the next and previous changed entry, and past the ends to none", () => {
    const changed = { kind: "changed" as const, from: 1, to: 2, movedFrom: null, comparison: null };
    const unchanged = { ...changed, kind: "unchanged" as const };
    const entries = [
      entry(scope, ""),
      entry(pay, "", { change: changed }),
      entry(ship, "", { change: unchanged }),
      entry(node({ id: "o", key: "CHK-R4" }), "", { change: changed }),
    ];
    expect(nextChange(entries, null, 1)).toBe("CHK-R2");
    expect(nextChange(entries, null, -1)).toBe("CHK-R4");
    expect(nextChange(entries, "CHK-R2", 1)).toBe("CHK-R4");
    expect(nextChange(entries, "CHK-R3", -1)).toBe("CHK-R2");
    expect(nextChange(entries, "CHK-R4", 1)).toBeUndefined();
  });

  it("reads the base, the baseline and only changed from the address, and drops a baseline without its base", () => {
    expect(readSpecAddress({ changes: "baseline", baseline: "b1", onlyChanged: true })).toEqual({ changes: "baseline", baseline: "b1", onlyChanged: true });
    expect(readSpecAddress({ changes: "previous", baseline: "b1" })).toEqual({ changes: "previous" });
    expect(readSpecAddress({ changes: "lost", onlyChanged: true })).toEqual({});
  });

  it("marks what changed against the previous revision, in the title, the statement and the attributes", async () => {
    const { asked } = stubChanges();
    const router = await renderAt("/p/CHK/spec");
    await screen.findByRole("heading", { name: "1.1 Pay by card" });
    await userEvent.click(screen.getByRole("switch", { name: "Show changes" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ changes: "previous" }));
    await waitFor(() => expect(asked.some((query) => query.get("changes") === "previous" && query.get("limit") === "50")).toBe(true));
    const pay = entryOf("CHK-R2");
    await waitFor(() => expect(pay.querySelector('[data-spec-change="changed"]')).toHaveTextContent("Changed"));
    expect(within(pay).getByRole("heading").querySelector("[data-diff-title] ins")).toHaveTextContent("by card");
    expect(pay.querySelector('[data-diff-of="statement"] del')).toHaveTextContent("at once");
    expect(within(pay).getByRole("table", { name: "Attributes" })).toHaveTextContent("High");
    expect(within(pay).getByRole("link", { name: "Compare revision 1 with 2" })).toHaveAttribute("href", "/p/CHK/r/CHK-R2/history?from=1&to=2");
    expect(within(entryOf("CHK-R3")).getByText("Unchanged")).toBeInTheDocument();
    expect(screen.getByText("1 requirement differs")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("compares with a baseline, saying what moved and what is gone", async () => {
    const { asked } = stubChanges();
    const router = await renderAt("/p/CHK/spec?changes=previous");
    await userEvent.selectOptions(await screen.findByLabelText("Compared with"), "baseline");
    expect(await screen.findByText("Pick the baseline to compare with.")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText("Baseline"), "b1");
    await waitFor(() => expect(router.state.location.search).toEqual({ changes: "baseline", baseline: "b1" }));
    await waitFor(() => expect(asked.some((query) => query.get("changes") === "baseline" && query.get("baseline") === "b1")).toBe(true));
    expect(await within(entryOf("CHK-R3")).findByText("Moved, was 2")).toBeInTheDocument();
    const removed = document.body.querySelector<HTMLElement>("[data-spec-removed]")!;
    expect(within(removed).getByText("CHK-R9")).toBeInTheDocument();
    expect(removed.querySelector("del")).toHaveTextContent("3 Gone");
  });

  it("collapses what did not change to its heading, and steps between changes", async () => {
    stubChanges();
    const router = await renderAt("/p/CHK/spec?changes=previous");
    await screen.findByRole("article", { name: "Specification" });
    await waitFor(() => expect(entryOf("CHK-R2").querySelector('[data-spec-change="changed"]')).not.toBeNull());
    await userEvent.click(screen.getByRole("button", { name: "Only changed" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ changes: "previous", onlyChanged: true }));
    expect(within(entryOf("CHK-R3")).getByRole("heading", { name: "1.2 Ship" })).toBeInTheDocument();
    expect(within(entryOf("CHK-R3")).queryByText("Ship in a day.")).toBeNull();
    expect(within(entryOf("CHK-R1")).getByText("What is in scope.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Next change" }));
    await waitFor(() => expect(entryOf("CHK-R2")).toHaveFocus());
    await userEvent.click(screen.getByRole("button", { name: "Next change" }));
    await waitFor(() => expect(document.body.querySelector("[data-spec-announced]")).toHaveTextContent("No further change among the entries shown."));
  });
});

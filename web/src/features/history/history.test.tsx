import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Comparison, RevisionEntry } from "@/api/history";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => vi.unstubAllGlobals());

const project = aProject();
const current = aRequirement({ revision: 2 });
const entry = (number: number, comment: string): RevisionEntry => ({
  number,
  title: "Pay the basket",
  comment,
  authorId: null,
  authorName: "Ada Lovelace",
  createdAt: "2026-10-01T08:00:00Z",
  restoredFrom: null,
  hash: `h${number}`,
});
const base = {
  "GET /projects/CHK": { status: 200, body: { project } },
  "GET /requirements/CHK-R1": { status: 200, body: { requirement: current } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
  "GET /requirements/CHK-R1/revisions": { status: 200, body: { revisions: [entry(2, "Later."), entry(1, "First.")], total: 2, limit: 20, offset: 0 } },
};

describe("a requirement's history", () => {
  it("lists its revisions, the latest first, and compares the two selected", async () => {
    stubApi(base);
    const router = await renderAt("/p/CHK/r/CHK-R1/history");
    const rows = await screen.findAllByRole("row");
    expect(rows[1]).toHaveAttribute("data-revision-row", "2");
    expect(within(rows[1]!).getByText("Latest")).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText("Select Revision 2 to compare"));
    await userEvent.click(screen.getByLabelText("Select Revision 1 to compare"));
    await userEvent.click(screen.getByRole("button", { name: "Compare selected" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ from: 1, to: 2 }));
  });

  it("shows a comparison with the words changed marked and the values before and after", async () => {
    const comparison: Comparison = {
      from: { number: 1, title: "Pay the basket", type: null, authorName: "Ada", createdAt: "2026-10-01T08:00:00Z" },
      to: { number: 2, title: "Pay the basket", type: null, authorName: "Ada", createdAt: "2026-10-02T08:00:00Z" },
      title: [{ change: "equal", node: { type: "paragraph", content: [{ type: "text", text: "Pay the basket" }] } }],
      statement: [
        {
          change: "modified",
          node: {
            type: "paragraph",
            content: [
              { type: "text", text: "Pay " },
              { type: "text", text: "now", marks: [{ type: "diffDelete" }] },
              { type: "text", text: "later", marks: [{ type: "diffInsert" }] },
            ],
          },
        },
      ],
      rationale: [],
      attributes: [
        { attributeId: "0195f000-0000-7000-8000-0000000000d1", name: "Risk", change: "modified", from: "a", to: "b", fromText: "Low", toText: "High" },
      ],
    };
    stubApi({ ...base, "GET /requirements/CHK-R1/diff": { status: 200, body: { comparison } } });
    await renderAt("/p/CHK/r/CHK-R1/history?from=1&to=2");
    const statement = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-diff-of="statement"]');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(within(statement).getByText("Changed")).toBeInTheDocument();
    expect(statement.querySelector('ins[data-diff="insert"]')).toHaveTextContent("later");
    expect(statement.querySelector('del[data-diff="delete"]')).toHaveTextContent("now");
    const risk = document.querySelector('[data-attribute-diff="Risk"]')!;
    expect(risk).toHaveTextContent("Low");
    expect(risk).toHaveTextContent("High");
  });

  it("restores a revision after asking, over the latest one", async () => {
    const sent = stubApi({
      ...base,
      "POST /requirements/CHK-R1/revisions/1/restore": {
        status: 200,
        body: { requirement: aRequirement({ revision: 3 }), revision: { ...entry(3, "Back."), restoredFrom: 1 } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1/history");
    const row = (await screen.findAllByRole("row")).find((r) => r.getAttribute("data-revision-row") === "1")!;
    await userEvent.click(within(row).getByRole("button", { name: "Restore" }));
    const dialog = screen.getByRole("dialog", { name: "Restore revision 1?" });
    await userEvent.type(within(dialog).getByLabelText("Comment"), "Back.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Restore" }));
    expect(await screen.findByText("Revision 1 restored as revision 3.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ baseRevision: 2, comment: "Back." });
  });

  it("merges revisions and changes in its activity", async () => {
    stubApi({
      ...base,
      "GET /requirements/CHK-R1/activity": {
        status: 200,
        body: {
          entries: [
            {
              id: "a",
              kind: "change",
              at: "2026-10-03T08:00:00Z",
              actorName: "Ada",
              revision: null,
              change: { field: "priority", from: "", to: "must", reason: "" },
            },
            { id: "b", kind: "revision", at: "2026-10-02T08:00:00Z", actorName: "Ada", revision: entry(2, "Later."), change: null },
          ],
          next: null,
        },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1/history?tab=activity");
    const list = await screen.findByRole("list", { name: "Activity" });
    expect(within(list).getByText("changed the priority from nothing to must")).toBeInTheDocument();
    expect(within(list).getByText("wrote revision 2")).toBeInTheDocument();
  });
});

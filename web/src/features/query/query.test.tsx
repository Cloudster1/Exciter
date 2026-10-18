import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequirementRow } from "@/api/requirements";
import type { Suggestions } from "@/api/search";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { draft } from "@/test/workflows";
import { applyCompletion } from "./QueryInput";
import { caretLine } from "./help";

afterEach(() => vi.unstubAllGlobals());

const row: RequirementRow = { ...aRequirement(), outline: "1", trashed: false, attributes: {}, labels: [], suspectLinks: 0, quality: null };

/** The suggestions for a query typed so far: the fields while a field is typed, the statuses after status =. */
function suggestions(q: string): Suggestions {
  const at = Array.from(q).length;
  if (q.startsWith("status = ")) {
    return {
      completion: { slot: "value", field: "status", prefix: q.slice(9), from: 9, to: at, quoted: false, words: [] },
      words: [
        { text: "Draft", detail: "status", label: "" },
        { text: '"In review"', detail: "status", label: "" },
      ],
      requirements: [],
    };
  }
  return {
    completion: { slot: "field", prefix: q, from: 0, to: at, quoted: false, words: ["status"] },
    words: [{ text: "status", detail: "field", label: "" }],
    requirements: [{ requirement: { ...aRequirement(), trashed: false }, projectName: "", outline: "1" }],
  };
}

function stubGrid(grid?: (params: URLSearchParams) => { status: number; body: unknown }) {
  const asked: URLSearchParams[] = [];
  stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/requirements": (request) => {
      const params = new URL(request.url).searchParams;
      asked.push(params);
      return grid?.(params) ?? { status: 200, body: { rows: [row], total: 1, limit: 100, offset: 0 } };
    },
    "GET /projects/CHK/requirements/suggest": (request) => ({ status: 200, body: suggestions(new URL(request.url).searchParams.get("q") ?? "") }),
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [] } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
  });
  return asked;
}

describe("the grid's query", () => {
  it("switches the words box for a query box that completes at the caret and runs on Enter", async () => {
    const asked = stubGrid();
    const router = await renderAt("/p/CHK/grid?q=basket");
    await screen.findByRole("table", { name: "Requirements of the project, as the grid shows them" });
    await userEvent.click(screen.getByRole("button", { name: "Query" }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    const box = screen.getByRole("combobox", { name: "Query" });
    await userEvent.type(box, "sta");
    const list = await screen.findByRole("listbox", { name: "Suggestions" });
    expect(within(list).getByRole("option", { name: /Pay the basket/ })).toBeInTheDocument();
    await userEvent.click(within(list).getByRole("option", { name: /status field/ }));
    expect(box).toHaveValue("status ");
    await userEvent.type(box, "= ");
    await userEvent.click(within(await screen.findByRole("listbox", { name: "Suggestions" })).getByRole("option", { name: /In review/ }));
    expect(box).toHaveValue('status = "In review" ');
    expect(await axeViolations()).toEqual([]);
    await userEvent.type(box, "{Escape}{Enter}");
    await waitFor(() => expect(router.state.location.search).toEqual({ query: 'status = "In review"' }));
    await waitFor(() => expect(asked.at(-1)?.get("query")).toBe('status = "In review"'));
    expect(asked.at(-1)?.get("q")).toBeNull();
  });

  it("points at the character a refused query went wrong at, instead of a banner", async () => {
    stubGrid(() => ({ status: 400, body: { error: { code: "bad_query", message: 'Unknown field "ownr". Write owner if you mean that one.', position: 9 } } }));
    await renderAt("/p/CHK/grid?query=title+~+x+ownr+%3D+me");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Unknown field");
    expect(alert.querySelector("[data-query-caret]")?.textContent).toBe("        ^");
    expect(screen.getByRole("combobox", { name: "Query" })).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("explains what a query can say, and takes an example", async () => {
    stubGrid();
    await renderAt("/p/CHK/grid?query=kind+%3D+section");
    await userEvent.click(await screen.findByRole("button", { name: "What a query can say" }));
    const help = await screen.findByRole("dialog", { name: "What a query can say" });
    expect(within(help).getByText("approved, pending, outdated or none")).toBeInTheDocument();
    expect(within(help).getByText("verifiedBy IS EMPTY")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(help).getByRole("button", { name: "kind = requirement AND verifiedBy IS EMPTY" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Query" })).toHaveValue("kind = requirement AND verifiedBy IS EMPTY");
  });

  it("runs the query on Enter when the list opened under a pointer that did not move", async () => {
    stubGrid();
    const router = await renderAt("/p/CHK/grid?query=x");
    const box = await screen.findByRole("combobox", { name: "Query" });
    await userEvent.clear(box);
    await userEvent.type(box, "kind = section");
    fireEvent.mouseEnter(within(await screen.findByRole("listbox", { name: "Suggestions" })).getAllByRole("option")[0]!);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.search).toEqual({ query: "kind = section" }));
  });

  it("opens a requirement the text finds", async () => {
    stubGrid();
    const router = await renderAt("/p/CHK/grid?query=x");
    const box = await screen.findByRole("combobox", { name: "Query" });
    await userEvent.clear(box);
    await userEvent.type(box, "pay");
    await userEvent.click(await screen.findByRole("option", { name: /Pay the basket/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CHK/r/CHK-R1"));
  });

  it("goes back to words, taking the query away", async () => {
    stubGrid();
    const router = await renderAt("/p/CHK/grid?query=kind+%3D+section");
    await screen.findByRole("combobox", { name: "Query" });
    await userEvent.click(screen.getByRole("button", { name: "Words" }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(screen.getByRole("searchbox", { name: "Words" })).toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: "Query" })).toBeNull();
  });
});

describe("a completion", () => {
  it("replaces the word at the caret, closing a quote it opened", () => {
    expect(applyCompletion("sta", { from: 0, to: 3, quoted: false }, "status")).toEqual({ text: "status ", caret: 7 });
    expect(applyCompletion('status = "In r', { from: 9, to: 14, quoted: true }, "In review")).toEqual({ text: 'status = "In review" ', caret: 21 });
    expect(applyCompletion("owner = x AND", { from: 8, to: 9, quoted: false }, "currentUser()")).toEqual({
      text: "owner = currentUser()  AND",
      caret: 22,
    });
    expect(applyCompletion("Ä = ", { from: 4, to: 4, quoted: false }, "x")).toEqual({ text: "Ä = x ", caret: 6 });
  });

  it("puts a caret under the character an error names", () => {
    expect(caretLine(1)).toBe("^");
    expect(caretLine(4)).toBe("   ^");
    expect(caretLine(undefined)).toBe("");
  });
});

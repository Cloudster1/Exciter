import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequirementRow } from "@/api/requirements";
import type { Suggestions } from "@/api/search";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { draft } from "@/test/workflows";

afterEach(() => vi.unstubAllGlobals());

const row: RequirementRow = { ...aRequirement(), outline: "1", trashed: false, attributes: {}, labels: [], suspectLinks: 0, quality: null };

/** Two statuses after status =, and otherwise one field and one requirement the text finds. */
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

function stubGrid() {
  const asked: URLSearchParams[] = [];
  stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/requirements": (request) => {
      asked.push(new URL(request.url).searchParams);
      return { status: 200, body: { rows: [row], total: 1, limit: 100, offset: 0 } };
    },
    "GET /projects/CHK/requirements/suggest": (request) => ({ status: 200, body: suggestions(new URL(request.url).searchParams.get("q") ?? "") }),
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [] } },
  });
  return asked;
}

const options = () => within(screen.getByRole("listbox", { name: "Suggestions" })).getAllByRole("option");
const selected = () => options().findIndex((each) => each.getAttribute("aria-selected") === "true");

describe("the query box's keys", () => {
  it("opens the list on Arrow Down and goes round it both ways", async () => {
    stubGrid();
    await renderAt("/p/CHK/grid?query=x");
    const box = await screen.findByRole("combobox", { name: "Query" });
    await userEvent.click(box);
    expect(screen.queryByRole("listbox")).toBeNull();
    await userEvent.keyboard("{ArrowDown}");
    expect(await screen.findByRole("listbox", { name: "Suggestions" })).toBeInTheDocument();
    expect(box).toHaveAttribute("aria-expanded", "true");
    expect(options()).toHaveLength(2);
    expect(selected()).toBe(-1);
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()).toBe(0);
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()).toBe(1);
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()).toBe(0);
    await userEvent.keyboard("{ArrowUp}");
    expect(selected()).toBe(1);
    expect(box).toHaveAttribute("aria-activedescendant", options()[1]!.id);

    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(box).toHaveFocus();
    expect(box).toHaveValue("x");
  });

  it("takes the first completion on Tab, or the one picked, and leaves the box on Shift Tab", async () => {
    stubGrid();
    await renderAt("/p/CHK/grid?query=x");
    const box = await screen.findByRole("combobox", { name: "Query" });
    await userEvent.clear(box);
    await userEvent.type(box, "sta");
    await within(await screen.findByRole("listbox", { name: "Suggestions" })).findByRole("option", { name: /status field/ });
    await userEvent.keyboard("{Tab}");
    expect(box).toHaveValue("status ");
    expect(box).toHaveFocus();

    await userEvent.type(box, "= ");
    await within(await screen.findByRole("listbox", { name: "Suggestions" })).findByRole("option", { name: /In review/ });
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{Tab}");
    expect(box).toHaveValue('status = "In review" ');

    await userEvent.type(box, "{Backspace}");
    await screen.findByRole("listbox", { name: "Suggestions" });
    await userEvent.keyboard("{Shift>}{Tab}{/Shift}");
    expect(box).not.toHaveFocus();
    expect(box).toHaveValue('status = "In review"');
  });

  it("runs what is typed when Run is clicked", async () => {
    const asked = stubGrid();
    const router = await renderAt("/p/CHK/grid?query=x");
    const box = await screen.findByRole("combobox", { name: "Query" });
    await userEvent.clear(box);
    await userEvent.type(box, "kind = section");
    await userEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ query: "kind = section" }));
    await waitFor(() => expect(asked.at(-1)?.get("query")).toBe("kind = section"));
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequirementRow } from "@/api/requirements";
import { GRID_PAGE_SIZE } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import { draft } from "@/test/workflows";

afterEach(() => vi.unstubAllGlobals());

const TOTAL = 250;
// Six windows of up to a hundred rows are drawn, which outlasts vitest's five seconds while the Go gate runs beside.
const WALKING_TAKES = 20_000;

/** One row of each window the grid asks for, numbered by where the window starts. */
const rowAt = (offset: number): RequirementRow => ({
  ...aRequirement({ id: `0195f000-0000-7000-8000-${String(offset).padStart(12, "0")}`, key: `CHK-R${offset + 1}`, title: `Row ${offset + 1}` }),
  outline: String(offset + 1),
  trashed: false,
  attributes: {},
  labels: [],
  suspectLinks: 0,
  quality: null,
});

function stubPages() {
  const asked: URLSearchParams[] = [];
  stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/requirements": (request) => {
      const params = new URL(request.url).searchParams;
      asked.push(params);
      const offset = Number(params.get("offset") ?? 0);
      const shown = Math.min(GRID_PAGE_SIZE, TOTAL - offset);
      const rows = Array.from({ length: shown }, (_, i) => rowAt(offset + i));
      return { status: 200, body: { rows, total: TOTAL, limit: GRID_PAGE_SIZE, offset } };
    },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
    "GET /projects/CHK/types": { status: 200, body: { types: [] } },
    "GET /projects/CHK/statuses": { status: 200, body: { statuses: [draft] } },
    "GET /projects/CHK/views": { status: 200, body: { views: [] } },
  });
  return asked;
}

describe("the grid's pages", () => {
  it(
    "turns a window at a time, and starts again at the first for a new narrowing or order",
    async () => {
      const asked = stubPages();
      await renderAt("/projects/CHK/grid");
      expect(await screen.findByText("1 to 100 of 250 requirements")).toBeInTheDocument();
      expect(asked.at(-1)?.get("limit")).toBe(String(GRID_PAGE_SIZE));
      expect(asked.at(-1)?.get("offset")).toBe("0");
      const previous = screen.getByRole("button", { name: "Previous" });
      const next = screen.getByRole("button", { name: "Next" });
      expect(previous).toBeDisabled();

      await userEvent.click(next);
      await waitFor(() => expect(asked.at(-1)?.get("offset")).toBe(String(GRID_PAGE_SIZE)));
      expect(await screen.findByText("101 to 200 of 250 requirements")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Row 101" })).toBeInTheDocument();
      expect(previous).toBeEnabled();
      await userEvent.click(next);
      await waitFor(() => expect(asked.at(-1)?.get("offset")).toBe(String(2 * GRID_PAGE_SIZE)));
      expect(await screen.findByText("201 to 250 of 250 requirements")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
      // The window before was read already, so it comes back from what the grid holds.
      await userEvent.click(screen.getByRole("button", { name: "Previous" }));
      expect(await screen.findByText("101 to 200 of 250 requirements")).toBeInTheDocument();

      await userEvent.selectOptions(screen.getByLabelText("Priority"), "must");
      await waitFor(() => expect(asked.at(-1)?.get("priority")).toBe("must"));
      expect(asked.at(-1)?.get("offset")).toBe("0");
      expect(await screen.findByRole("button", { name: "Previous" })).toBeDisabled();

      await userEvent.click(screen.getByRole("button", { name: "Next" }));
      await waitFor(() => expect(asked.at(-1)?.get("offset")).toBe(String(GRID_PAGE_SIZE)));
      await userEvent.click(screen.getByRole("button", { name: "Sort by Title" }));
      await waitFor(() => expect(asked.at(-1)?.get("sort")).toBe("title"));
      expect(asked.at(-1)?.get("offset")).toBe("0");
    },
    WALKING_TAKES,
  );
});

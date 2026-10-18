import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Dashboard, WidgetData } from "@/api/dashboards";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { bucketAddress, bucketLabel, joinQuery } from "./address";
import { arcPath } from "./charts";

// Dashboards (#212).

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const id = (n: number) => `0195f000-0000-7000-8000-0000000005${String(n).padStart(2, "0")}`;
const person = { id: id(90), name: "Bob Builder" };
const widget = (n: number, kind: Dashboard["widgets"][number]["kind"], title: string, width = 1) => ({
  id: id(n),
  kind,
  title,
  width,
  position: n,
  config: { source: { query: "type = Functional" }, groupBy: "status" as const, shape: "bars" as const },
});
const dashboard: Dashboard = {
  id: id(1),
  projectKey: "CHK",
  name: "Brakes",
  shared: true,
  owner: person,
  home: false,
  can: { edit: true, share: true },
  updatedAt: "2026-10-18T08:00:00Z",
  createdAt: "2026-10-18T08:00:00Z",
  widgets: [widget(11, "breakdown", "By status"), widget(12, "count", "All", 2)],
};
const breakdown: WidgetData = {
  kind: "breakdown",
  total: 5,
  groups: [
    {
      name: "status",
      total: 5,
      buckets: [
        { label: "Draft", count: 3, category: "draft", query: 'status = "Draft"' },
        { label: "", count: 2, query: "status IS EMPTY" },
      ],
    },
  ],
  lines: [],
  rows: [],
  average: null,
  query: "type = Functional",
  viewId: null,
};
const count: WidgetData = { ...breakdown, kind: "count", groups: [] };

function stubDashboard(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /projects/CHK/dashboards": { status: 200, body: { dashboards: [dashboard] } },
    [`GET /dashboards/${dashboard.id}`]: { status: 200, body: { dashboard } },
    [`GET /widgets/${id(11)}/data`]: { status: 200, body: { data: breakdown } },
    [`GET /widgets/${id(12)}/data`]: { status: 200, body: { data: count } },
    "GET /projects/CHK/views": { status: 200, body: { views: [] } },
    "GET /projects/CHK/attributes": { status: 200, body: { attributes: [], recompute: null } },
    ...more,
  });
}

describe("a dashboard", () => {
  it("opens a bucket in the grid with its source and its condition joined, a view's filter kept", () => {
    expect(joinQuery("type = Functional ORDER BY updated DESC", "suspect > 0")).toBe("(type = Functional) AND suspect > 0");
    expect(joinQuery("", "suspect > 0")).toBe("suspect > 0");
    expect(bucketAddress(breakdown, 'status = "Draft"')).toEqual({ query: '(type = Functional) AND status = "Draft"' });
    expect(bucketLabel({ label: "", count: 1, query: "" })).toBe("Other");
    expect(bucketLabel({ label: "", count: 1, query: "owner IS EMPTY" })).toBe("None");
    expect(arcPath(0, 0.25)).toMatch(/^M50 2 A48 48 0 0 1 98 50/);
  });

  it("draws its widgets, each value a link into the grid, and the numbers as a table too", async () => {
    stubDashboard();
    await renderAt(`/p/CHK/dashboards/${dashboard.id}`);
    const card = (await screen.findByText("By status")).closest("[data-widget-card]") as HTMLElement;
    const [draft] = await within(card).findAllByRole("link", { name: "Open the 3 with Draft in the grid" });
    expect(draft!.getAttribute("href")).toContain("/p/CHK/grid?query=");
    expect(decodeURIComponent(draft!.getAttribute("href")!.replace(/\+/g, " "))).toContain('(type = Functional) AND status = "Draft"');
    expect(within(card).getAllByText("None").length).toBeGreaterThan(0);
    const all = (await screen.findByText("All")).closest("[data-widget-card]") as HTMLElement;
    expect(await within(all).findByText("5 requirements")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Export as PDF" })).toHaveAttribute("href", `/api/v1/dashboards/${dashboard.id}/dashboard.pdf`);
    expect(await axeViolations()).toEqual([]);
  });

  it("is arranged by dragging or by buttons, and a widget's width is chosen", async () => {
    const sent = stubDashboard({
      [`PUT /dashboards/${dashboard.id}/order`]: { status: 200, body: { dashboard: { ...dashboard, widgets: [...dashboard.widgets].reverse() } } },
      [`PATCH /widgets/${id(11)}`]: { status: 200, body: { widget: { ...dashboard.widgets[0], width: 3 } } },
    });
    await renderAt(`/p/CHK/dashboards/${dashboard.id}`);
    await userEvent.click(await screen.findByRole("button", { name: "Arrange" }));
    await userEvent.click(screen.getByRole("button", { name: "Move By status later" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ widgets: [id(12), id(11)] }));
    const items = document.querySelectorAll("[data-dashboard-grid] > li");
    fireEvent.dragStart(items[1]!);
    fireEvent.dragOver(items[0]!);
    fireEvent.drop(items[0]!);
    await waitFor(() => expect(sent.filter((r) => r.method === "PUT")).toHaveLength(2));
    await userEvent.selectOptions(screen.getAllByLabelText("Width")[0]!, "3");
    await waitFor(() => expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ width: 3 }));
    expect(await axeViolations()).toEqual([]);
  });

  it("is listed among the project's, and a new one starts from a template", async () => {
    const sent = stubDashboard({
      "GET /dashboard-templates": {
        status: 200,
        body: { templates: [{ key: "overview", name: "Overview", description: "", widgets: [] }], kinds: [] },
      },
      "POST /projects/CHK/dashboards": { status: 201, body: { dashboard: { ...dashboard, id: id(2), name: "Mine", shared: false } } },
      [`GET /dashboards/${id(2)}`]: { status: 200, body: { dashboard: { ...dashboard, id: id(2), name: "Mine", shared: false, widgets: [] } } },
    });
    await renderAt("/p/CHK/dashboards");
    const shared = await screen.findByRole("region", { name: "Shared with the project" });
    expect(within(shared).getByRole("link", { name: "Brakes" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New dashboard" }));
    await userEvent.type(screen.getByLabelText("Name"), "Mine");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Make the dashboard" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Mine", shared: false, template: "overview" }));
    expect(await screen.findByRole("heading", { name: "Mine" })).toBeInTheDocument();
  });
});

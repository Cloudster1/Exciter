import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE, DASHBOARD_REFETCH_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// Dashboards over a project's requirements (#212).

type Wire = components["schemas"];

export type Dashboard = Wire["Dashboard"];
export type DashboardSummary = Wire["DashboardSummary"];
export type Widget = Wire["Widget"];
export type WidgetConfig = Wire["WidgetConfig"];
export type WidgetData = Wire["WidgetData"];
export type WidgetKind = Widget["kind"];
export type WidgetInput = Wire["WidgetInput"];
export type DashboardTemplate = Wire["DashboardTemplate"];
export type Bucket = Wire["WidgetBucket"];

const dashboardsKey = (projectKey: string) => ["dashboards", projectKey.toUpperCase()] as const;
const dashboardKey = (id: string) => ["dashboard", id] as const;
const widgetKey = (id: string) => ["widget", id] as const;

/** The project's dashboards the reader may read. */
export function useDashboards(projectKey: string) {
  return useQuery({
    queryKey: dashboardsKey(projectKey),
    queryFn: async (): Promise<DashboardSummary[]> =>
      (await api.GET("/projects/{projectKey}/dashboards", { params: { path: { projectKey } } })).data!.dashboards,
    enabled: Boolean(projectKey),
  });
}

/** One dashboard and its widgets in order. */
export function useDashboard(id: string | undefined) {
  return useQuery({
    queryKey: dashboardKey(id ?? ""),
    queryFn: async (): Promise<Dashboard> => (await api.GET("/dashboards/{dashboardID}", { params: { path: { dashboardID: id! } } })).data!.dashboard,
    enabled: Boolean(id),
  });
}

/** The templates a first dashboard is made from, and every kind of widget. */
export function useDashboardTemplates() {
  return useQuery({
    queryKey: ["dashboard-templates"],
    queryFn: async () => (await api.GET("/dashboard-templates")).data!,
    staleTime: Infinity,
  });
}

/** What a widget shows now, read as the reader and read again while the dashboard is open. */
export function useWidgetData(id: string) {
  return useQuery({
    queryKey: widgetKey(id),
    queryFn: async (): Promise<WidgetData> => (await api.GET("/widgets/{widgetID}/data", { params: { path: { widgetID: id } } })).data!.data,
    refetchInterval: DASHBOARD_REFETCH_MS,
  });
}

/** What a widget being set up would show. */
export function useWidgetPreview(projectKey: string, kind: WidgetKind, config: WidgetConfig, enabled: boolean) {
  return useQuery({
    queryKey: ["widget-preview", projectKey.toUpperCase(), kind, config],
    queryFn: async (): Promise<WidgetData> =>
      (await api.POST("/projects/{projectKey}/widget-preview", { params: { path: { projectKey } }, body: { kind, config } })).data!.data,
    enabled,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** Where a dashboard downloads as a PDF. */
export function dashboardPdfHref(id: string): string {
  return `${API_BASE}/dashboards/${encodeURIComponent(id)}/dashboard.pdf`;
}

/** Makes, changes and deletes dashboards and their widgets, asking again for what changed. */
export function useDashboardActions(projectKey: string) {
  const queryClient = useQueryClient();
  const settle = (id?: string) => {
    void queryClient.invalidateQueries({ queryKey: dashboardsKey(projectKey) });
    if (id) void queryClient.invalidateQueries({ queryKey: dashboardKey(id) });
  };
  return {
    create: useMutation({
      mutationFn: async (body: Wire["DashboardInput"]): Promise<Dashboard> =>
        (await api.POST("/projects/{projectKey}/dashboards", { params: { path: { projectKey } }, body })).data!.dashboard,
      onSuccess: (d) => settle(d.id),
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: Wire["DashboardPatch"] & { id: string }): Promise<Dashboard> =>
        (await api.PATCH("/dashboards/{dashboardID}", { params: { path: { dashboardID: id } }, body })).data!.dashboard,
      onSuccess: (d) => {
        settle(d.id);
        // The project's home goes with a dashboard no longer shared.
        void queryClient.invalidateQueries({ queryKey: ["projects"] });
      },
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/dashboards/{dashboardID}", { params: { path: { dashboardID: id } } });
        return id;
      },
      onSuccess: (id) => {
        settle();
        queryClient.removeQueries({ queryKey: dashboardKey(id) });
      },
    }),
    addWidget: useMutation({
      mutationFn: async ({ dashboardId, ...body }: WidgetInput & { dashboardId: string }) =>
        (await api.POST("/dashboards/{dashboardID}/widgets", { params: { path: { dashboardID: dashboardId } }, body })).data!.widget,
      onSuccess: (_, vars) => settle(vars.dashboardId),
    }),
    updateWidget: useMutation({
      mutationFn: async ({ id, dashboardId: _dashboard, ...body }: Wire["WidgetPatch"] & { id: string; dashboardId: string }) =>
        (await api.PATCH("/widgets/{widgetID}", { params: { path: { widgetID: id } }, body })).data!.widget,
      onSuccess: (widget, vars) => {
        settle(vars.dashboardId);
        void queryClient.invalidateQueries({ queryKey: widgetKey(widget.id) });
      },
    }),
    removeWidget: useMutation({
      mutationFn: async ({ id }: { id: string; dashboardId: string }) => {
        await api.DELETE("/widgets/{widgetID}", { params: { path: { widgetID: id } } });
      },
      onSuccess: (_, vars) => settle(vars.dashboardId),
    }),
    order: useMutation({
      mutationFn: async ({ dashboardId, widgets }: { dashboardId: string; widgets: string[] }): Promise<Dashboard> =>
        (await api.PUT("/dashboards/{dashboardID}/order", { params: { path: { dashboardID: dashboardId } }, body: { widgets } })).data!.dashboard,
      onSuccess: (d) => queryClient.setQueryData(dashboardKey(d.id), d),
    }),
  };
}

import { createRoute } from "@tanstack/react-router";
import { PrintBaselineDocument, PrintProjectDocument } from "@/features/print/PrintDocument";
import { PrintSessionReport } from "@/features/print/PrintSessionReport";
import { PrintDashboard } from "@/features/dashboard/DashboardPages";
import type { DocumentShape } from "@/api/documents";
import { rootRoute } from "./root";

/** The template a print goes through and the baseline its change marks count from, as the API passed them on. */
function shapeOf(search: Record<string, unknown>): DocumentShape {
  return {
    ...(typeof search.template === "string" ? { template: search.template } : {}),
    ...(typeof search.since === "string" ? { since: search.since } : {}),
  };
}

// What the render service opens to print a document as PDF: outside the shell and its sign-in guard, since the
// service signs in with a token on each API call rather than a session, as Stator's print views do.

export const printProjectDocumentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/p/$projectKey/document",
  validateSearch: (search: Record<string, unknown>): { section?: string } & DocumentShape => ({
    ...(typeof search.section === "string" ? { section: search.section } : {}),
    ...shapeOf(search),
  }),
  component: function PrintProjectDocumentPage() {
    const { projectKey } = printProjectDocumentRoute.useParams();
    const { section, template, since } = printProjectDocumentRoute.useSearch();
    return <PrintProjectDocument projectKey={projectKey} section={section} shape={{ template, since }} />;
  },
});

export const printSessionReportRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/sessions/$sessionId",
  component: function PrintSessionReportPage() {
    const { sessionId } = printSessionReportRoute.useParams();
    return <PrintSessionReport sessionId={sessionId} />;
  },
});

export const printBaselineDocumentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/baselines/$baselineId",
  validateSearch: (search: Record<string, unknown>): DocumentShape => shapeOf(search),
  component: function PrintBaselineDocumentPage() {
    const { baselineId } = printBaselineDocumentRoute.useParams();
    const shape = printBaselineDocumentRoute.useSearch();
    return <PrintBaselineDocument baselineId={baselineId} shape={shape} />;
  },
});

export const printDashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/dashboards/$dashboardId",
  component: function PrintDashboardPage() {
    const { dashboardId } = printDashboardRoute.useParams();
    return <PrintDashboard dashboardId={dashboardId} />;
  },
});

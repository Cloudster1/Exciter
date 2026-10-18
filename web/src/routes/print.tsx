import { createRoute } from "@tanstack/react-router";
import { PrintBaselineDocument, PrintProjectDocument } from "@/features/print/PrintDocument";
import { PrintSessionReport } from "@/features/print/PrintSessionReport";
import { PrintDashboard } from "@/features/dashboard/DashboardPages";
import { rootRoute } from "./root";

// What the render service opens to print a document as PDF: outside the shell and its sign-in guard, since the
// service signs in with a token on each API call rather than a session, as Stator's print views do.

export const printProjectDocumentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/p/$projectKey/document",
  validateSearch: (search: Record<string, unknown>): { section?: string } => (typeof search.section === "string" ? { section: search.section } : {}),
  component: function PrintProjectDocumentPage() {
    const { projectKey } = printProjectDocumentRoute.useParams();
    const { section } = printProjectDocumentRoute.useSearch();
    return <PrintProjectDocument projectKey={projectKey} section={section} />;
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
  component: function PrintBaselineDocumentPage() {
    const { baselineId } = printBaselineDocumentRoute.useParams();
    return <PrintBaselineDocument baselineId={baselineId} />;
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

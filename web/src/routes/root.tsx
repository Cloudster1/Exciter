import { Outlet, createRootRouteWithContext, useNavigate, type ErrorComponentProps } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Button, EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { AppShell } from "@/features/shell/AppShell";
import { t } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

// The shell is the signed-in layout's; the sign-in page stands on its own. A
// path that leads nowhere still gets the shell, so there is somewhere to go.
export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: () => (
    <AppShell>
      <NotFound />
    </AppShell>
  ),
});

// Drawn inside the shell, so the reader who followed a stale link still has
// the sidebar to go somewhere else.
export function NotFound() {
  const navigate = useNavigate();
  return (
    <div data-not-found>
      <PageHeader title={t.notFound.title} />
      <EmptyState
        icon={<Icon.Warning />}
        title={t.notFound.emptyTitle}
        description={t.notFound.body}
        action={
          <Button variant="secondary" onClick={() => navigate({ to: "/" })} data-action="home">
            {t.notFound.action}
          </Button>
        }
      />
    </div>
  );
}

// A page that throws loses only itself: the shell stays, and the reader can go
// somewhere else or try again.
export function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <div data-route-error>
      <PageHeader title={t.routeError.title} />
      <EmptyState
        icon={<Icon.Warning />}
        title={t.routeError.emptyTitle}
        description={error instanceof Error && error.message ? error.message : t.routeError.fallback}
        action={
          <Button variant="secondary" onClick={reset}>
            {t.routeError.retry}
          </Button>
        }
      />
    </div>
  );
}

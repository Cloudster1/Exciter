import { Outlet, createRoute, redirect } from "@tanstack/react-router";
import { meQuery } from "@/api/auth";
import { ApiError } from "@/api/client";
import { AppShell } from "@/features/shell/AppShell";
import { rootRoute } from "./root";

/** Everything behind sign-in: a visitor without a session goes to sign in before any page loads, and comes back afterwards. */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) {
        throw redirect({ to: "/login", search: { next: location.href } });
      }
      throw error;
    }
  },
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
});

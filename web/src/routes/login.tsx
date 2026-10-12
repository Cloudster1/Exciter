import { createRoute, redirect } from "@tanstack/react-router";
import { meQuery } from "@/api/auth";
import { LoginPage } from "@/features/auth/LoginPage";
import { safeNext } from "@/lib/session";
import { rootRoute } from "./root";

interface LoginSearch {
  next?: string;
  sso?: string;
  org?: string;
}

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    ...(typeof search.next === "string" ? { next: search.next } : {}),
    ...(typeof search.sso === "string" ? { sso: search.sso } : {}),
    ...(typeof search.org === "string" ? { org: search.org } : {}),
  }),
  // Somebody already signed in has nothing to do here. The answer is settled
  // before the redirect is thrown, so the catch cannot swallow the redirect.
  beforeLoad: async ({ context, search }) => {
    const signedIn = await context.queryClient.ensureQueryData(meQuery).then(
      () => true,
      () => false,
    );
    if (signedIn) {
      // href wins over to when the router resolves a redirect.
      throw redirect({ to: "/", href: safeNext(search.next) });
    }
  },
  component: function Login() {
    const { next, sso, org } = loginRoute.useSearch();
    return <LoginPage next={next} sso={sso} org={org} />;
  },
});

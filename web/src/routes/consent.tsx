import { createRoute } from "@tanstack/react-router";
import { OAUTH_CONSENT_PATH } from "@/config";
import { ConsentPage } from "@/features/oauth/ConsentPage";
import { appRoute } from "./app";

interface ConsentSearch {
  request?: string;
  error?: string;
}

/** The consent page needs a session, so somebody signed out signs in and comes back to it. */
export const consentRoute = createRoute({
  getParentRoute: () => appRoute,
  path: OAUTH_CONSENT_PATH,
  validateSearch: (search: Record<string, unknown>): ConsentSearch => ({
    ...(typeof search.request === "string" ? { request: search.request } : {}),
    ...(typeof search.error === "string" ? { error: search.error } : {}),
  }),
  component: function Consent() {
    const { request, error } = consentRoute.useSearch();
    return <ConsentPage request={request} error={error} />;
  },
});

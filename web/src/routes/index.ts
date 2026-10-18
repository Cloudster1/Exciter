import { createElement } from "react";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import { Skeleton } from "@/components/ui";
import { LOGIN_PATH } from "@/config";
import { appRoute } from "./app";
import { armatureAccountRoute, armatureSettingsRoute } from "./armature";
import { webhooksRoute } from "./webhooks";
import { statorAccountRoute, statorSettingsRoute } from "./stator";
import { auditRoute } from "./audit";
import { defaultThemeRoute } from "./default-theme";
import { homeRoute } from "./home";
import { installationRoute } from "./installation";
import { consentRoute } from "./consent";
import { labelRoute } from "./labels";
import { loginRoute } from "./login";
import { notificationSettingsRoute, notificationsRoute, watchingRoute } from "./notifications";
import {
  newProjectRoute,
  gridRoute,
  newRequirementRoute,
  projectPermissionsRoute,
  projectRoute,
  projectSettingsRoute,
  projectsRoute,
  specRoute,
  trashRoute,
  suspectsRoute,
  baselinesRoute,
  coverageRoute,
  codeTagsRoute,
  qualityRoute,
  matrixRoute,
  chainsRoute,
  baselineRoute,
  baselineCompareRoute,
  sessionsRoute,
  sessionRoute,
  workflowDesignRoute,
} from "./projects";
import { historyRoute, permalinkRoute, requirementRoute } from "./requirements";
import { reviewsRoute } from "./reviews";
import { searchRoute } from "./search";
import { linkTypesRoute, membersRoute, settingsRoute } from "./settings";
import { ssoRoute } from "./sso";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";
import { allTokensRoute, tokensRoute } from "./tokens";
import { allConnectedAppsRoute, connectedAppsRoute } from "./connected-apps";
import { printBaselineDocumentRoute, printProjectDocumentRoute, printSessionReportRoute } from "./print";
import { RouteError, rootRoute } from "./root";

const routeTree = rootRoute.addChildren([
  loginRoute,
  printProjectDocumentRoute,
  printBaselineDocumentRoute,
  printSessionReportRoute,
  appRoute.addChildren([
    homeRoute,
    projectsRoute,
    newProjectRoute,
    projectRoute,
    projectSettingsRoute,
    workflowDesignRoute,
    projectPermissionsRoute,
    newRequirementRoute,
    trashRoute,
    suspectsRoute,
    baselinesRoute,
    coverageRoute,
    codeTagsRoute,
    qualityRoute,
    matrixRoute,
    chainsRoute,
    baselineRoute,
    baselineCompareRoute,
    sessionsRoute,
    sessionRoute,
    specRoute,
    gridRoute,
    requirementRoute,
    permalinkRoute,
    historyRoute,
    labelRoute,
    searchRoute,
    settingsRoute,
    installationRoute,
    consentRoute,
    membersRoute,
    linkTypesRoute,
    ssoRoute,
    auditRoute,
    defaultThemeRoute,
    themesRoute,
    tokensRoute,
    connectedAppsRoute,
    allConnectedAppsRoute,
    allTokensRoute,
    armatureSettingsRoute,
    webhooksRoute,
    armatureAccountRoute,
    statorSettingsRoute,
    statorAccountRoute,
    notificationsRoute,
    notificationSettingsRoute,
    watchingRoute,
    reviewsRoute,
    themeNewRoute,
    themeEditRoute,
  ]),
]);

/** The application's router; tests pass a memory history to start anywhere. */
export function buildRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    history,
    context: { queryClient },
    defaultPreload: "intent",
    defaultErrorComponent: RouteError,
    defaultPendingComponent: () => createElement(Skeleton),
  });
}

export type AppRouter = ReturnType<typeof buildRouter>;

/** The session is gone: forget who it was, and sign in again to come back here. */
export function sendToLogin(router: AppRouter, client: QueryClient) {
  const here = router.state.location;
  if (here.pathname === LOGIN_PATH) return;
  client.removeQueries({ queryKey: meQueryKey });
  void router.navigate({ to: "/login", search: { next: here.href } });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}

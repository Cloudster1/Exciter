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
  projectRoute,
  projectsRoute,
  specRoute,
  suspectsRoute,
  baselinesRoute,
  coverageRoute,
  codeTagsRoute,
  qualityRoute,
  dashboardsRoute,
  dashboardRoute,
  glossaryRoute,
  duplicatesRoute,
  termRoute,
  matrixRoute,
  chainsRoute,
  baselineRoute,
  baselineCompareRoute,
  sessionsRoute,
  sessionRoute,
} from "./projects";
import { movedProjectRoute, projectSettingsRoute, projectSettingsSectionRoutes, workflowDesignRoute } from "./project-settings";
import { historyRoute, permalinkRoute, requirementRoute, traceGraphRoute } from "./requirements";
import { reviewsRoute } from "./reviews";
import { searchRoute } from "./search";
import { linkTypesRoute, profileRoute, settingsRoute } from "./settings";
import { accessRoute, accessTabRoute } from "./access";
import { adminIndexRoute, adminRoute, settingsAccessTabRoute, settingsMovedRoute } from "./admin";
import { usersRoute } from "./users";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";
import { tokensRoute } from "./tokens";
import { allConnectedAppsRoute, connectedAppsRoute } from "./connected-apps";
import {
  printBaselineDocumentRoute,
  printDashboardRoute,
  printExecutionReportRoute,
  printProjectDocumentRoute,
  printRiskReportRoute,
  printSessionReportRoute,
} from "./print";
import { testExecutionRoute, testPlanRoute, testRunRoute, testsRoute } from "./tests";
import { risksRoute } from "./risks";
import { RouteError, rootRoute } from "./root";

const routeTree = rootRoute.addChildren([
  loginRoute,
  movedProjectRoute,
  printProjectDocumentRoute,
  printBaselineDocumentRoute,
  printDashboardRoute,
  printSessionReportRoute,
  printExecutionReportRoute,
  printRiskReportRoute,
  appRoute.addChildren([
    homeRoute,
    projectsRoute,
    newProjectRoute,
    projectRoute,
    projectSettingsRoute,
    ...projectSettingsSectionRoutes,
    workflowDesignRoute,
    newRequirementRoute,
    suspectsRoute,
    baselinesRoute,
    coverageRoute,
    codeTagsRoute,
    qualityRoute,
    dashboardsRoute,
    dashboardRoute,
    glossaryRoute,
    duplicatesRoute,
    termRoute,
    matrixRoute,
    chainsRoute,
    baselineRoute,
    baselineCompareRoute,
    sessionsRoute,
    sessionRoute,
    testsRoute,
    risksRoute,
    testPlanRoute,
    testExecutionRoute,
    testRunRoute,
    specRoute,
    gridRoute,
    requirementRoute,
    permalinkRoute,
    historyRoute,
    traceGraphRoute,
    labelRoute,
    searchRoute,
    settingsRoute,
    profileRoute,
    consentRoute,
    adminRoute.addChildren([
      adminIndexRoute,
      installationRoute,
      accessRoute,
      accessTabRoute,
      usersRoute,
      linkTypesRoute,
      armatureSettingsRoute,
      statorSettingsRoute,
      allConnectedAppsRoute,
      webhooksRoute,
      auditRoute,
    ]),
    settingsMovedRoute,
    settingsAccessTabRoute,
    themesRoute,
    tokensRoute,
    connectedAppsRoute,
    armatureAccountRoute,
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

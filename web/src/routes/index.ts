import { createElement } from "react";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import { Skeleton } from "@/components/ui";
import { LOGIN_PATH } from "@/config";
import { appRoute } from "./app";
import { auditRoute } from "./audit";
import { defaultThemeRoute } from "./default-theme";
import { homeRoute } from "./home";
import { loginRoute } from "./login";
import {
  newProjectRoute,
  newRequirementRoute,
  projectPermissionsRoute,
  projectRoute,
  projectSettingsRoute,
  projectsRoute,
  specRoute,
  trashRoute,
  workflowDesignRoute,
} from "./projects";
import { historyRoute, permalinkRoute, requirementRoute } from "./requirements";
import { membersRoute, settingsRoute } from "./settings";
import { ssoRoute } from "./sso";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";
import { RouteError, rootRoute } from "./root";

const routeTree = rootRoute.addChildren([
  loginRoute,
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
    specRoute,
    requirementRoute,
    permalinkRoute,
    historyRoute,
    settingsRoute,
    membersRoute,
    ssoRoute,
    auditRoute,
    defaultThemeRoute,
    themesRoute,
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

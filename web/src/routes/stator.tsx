import { createRoute } from "@tanstack/react-router";
import { STATOR_ACCOUNT_PATH, STATOR_SETTINGS_PATH } from "@/config";
import { AccountSection } from "@/features/stator/AccountSection";
import { ConnectionSettings } from "@/features/stator/ConnectionSettings";
import { PageHeader, Page } from "@/components/ui";
import { t } from "@/i18n";
import { adminRoute } from "./admin";
import { appRoute } from "./app";

/** Where an administrator connects the installation's Stator and its webhook. */
export const statorSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: STATOR_SETTINGS_PATH,
  component: () => (
    <Page width="narrow">
      <PageHeader title={t.stator.title} />
      <ConnectionSettings />
    </Page>
  ),
});

/** Where a person connects their own Stator account, under their account. */
export const statorAccountRoute = createRoute({ getParentRoute: () => appRoute, path: STATOR_ACCOUNT_PATH, component: AccountSection });

import { createRoute } from "@tanstack/react-router";
import { ARMATURE_ACCOUNT_PATH, ARMATURE_SETTINGS_PATH } from "@/config";
import { AccountSection } from "@/features/armature/AccountSection";
import { ConnectionSettings } from "@/features/armature/ConnectionSettings";
import { PageHeader, Page } from "@/components/ui";
import { t } from "@/i18n";
import { adminRoute } from "./admin";
import { appRoute } from "./app";

/** Where an administrator connects the installation's Armature and its webhook. */
export const armatureSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: ARMATURE_SETTINGS_PATH,
  component: () => (
    <Page width="narrow">
      <PageHeader title={t.armature.title} />
      <ConnectionSettings />
    </Page>
  ),
});

/** Where a person connects their own Armature account, under their account. */
export const armatureAccountRoute = createRoute({ getParentRoute: () => appRoute, path: ARMATURE_ACCOUNT_PATH, component: AccountSection });

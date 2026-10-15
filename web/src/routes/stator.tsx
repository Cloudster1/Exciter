import { createRoute } from "@tanstack/react-router";
import { STATOR_ACCOUNT_PATH, STATOR_SETTINGS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { AccountSection } from "@/features/stator/AccountSection";
import { ConnectionSettings } from "@/features/stator/ConnectionSettings";
import { PageHeader } from "@/components/ui";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where an administrator connects the installation's Stator and its webhook. */
export const statorSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: STATOR_SETTINGS_PATH,
  component: () => (
    <AdminsOnly title={t.stator.title} refusal={t.stator.notAdmin}>
      <div className="mx-auto max-w-3xl">
        <PageHeader title={t.stator.title} />
        <ConnectionSettings />
      </div>
    </AdminsOnly>
  ),
});

/** Where a person connects their own Stator account, under their account. */
export const statorAccountRoute = createRoute({ getParentRoute: () => appRoute, path: STATOR_ACCOUNT_PATH, component: AccountSection });

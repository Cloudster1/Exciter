import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { INSTALLATION_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { InstallationSettings } from "@/features/installation/InstallationSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where an administrator renames the installation. */
export const installationRoute = createRoute({
  getParentRoute: () => appRoute,
  path: INSTALLATION_PATH,
  component: () => (
    <AdminsOnly title={t.installation.title} refusal={t.installation.notAdmin}>
      <div className="mx-auto max-w-3xl">
        <PageHeader title={t.installation.title} />
        <InstallationSettings />
      </div>
    </AdminsOnly>
  ),
});

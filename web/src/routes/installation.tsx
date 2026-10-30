import { createRoute } from "@tanstack/react-router";
import { PageHeader, Page } from "@/components/ui";
import { INSTALLATION_PATH } from "@/config";
import { InstallationSettings } from "@/features/installation/InstallationSettings";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** The installation itself: what it is called, the theme everybody sees until they choose, and the suite demo. */
export const installationRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: INSTALLATION_PATH,
  component: () => (
    <Page width="narrow" data-installation-page="">
      <PageHeader title={t.installation.title} />
      <InstallationSettings />
    </Page>
  ),
});

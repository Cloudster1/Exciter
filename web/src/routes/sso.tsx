import { createRoute } from "@tanstack/react-router";
import { PageHeader, Page } from "@/components/ui";
import { SSO_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { GroupRoles } from "@/features/auth/GroupRoles";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The installation's identity provider, the roles its groups grant, and who waits to be let in, for its administrators. */
export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SSO_PATH,
  component: () => (
    <AdminsOnly title={t.sso.title} refusal={t.sso.notAdmin}>
      <Page width="narrow" data-sso>
        <PageHeader title={t.sso.title} />
        <JoinRequests />
        <ProviderSettings />
        <GroupRoles />
      </Page>
    </AdminsOnly>
  ),
});

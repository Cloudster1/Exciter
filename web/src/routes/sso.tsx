import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { SSO_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { GroupRoles } from "@/features/auth/GroupRoles";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The organization's identity provider, the roles its groups grant, and who waits to be let in, for its administrators. */
export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SSO_PATH,
  component: () => (
    <AdminsOnly title={t.sso.title} refusal={t.sso.notAdmin}>
      <div className="mx-auto max-w-3xl" data-sso>
        <PageHeader title={t.sso.title} />
        <JoinRequests />
        <ProviderSettings />
        <GroupRoles />
      </div>
    </AdminsOnly>
  ),
});

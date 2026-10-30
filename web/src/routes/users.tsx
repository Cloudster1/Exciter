import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { USERS_PATH } from "@/config";
import { AddMember } from "@/features/auth/AddMember";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { UserList } from "@/features/users/UserList";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** The installation's accounts: who waits to be let in, adding somebody with a password, and switching an account off or back on. */
export const usersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: USERS_PATH,
  component: () => (
    <Page width="content" data-users-page="">
      <PageHeader title={t.admin.users.title} actions={<AddMember />} />
      <p className="mb-4 max-w-prose text-sm text-ink-muted">{t.admin.users.intro}</p>
      <JoinRequests />
      <UserList />
    </Page>
  ),
});

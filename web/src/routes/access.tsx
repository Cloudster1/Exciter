import { useId, useState } from "react";
import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Button, Page, PageHeader, TabPanel, Tabs } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ACCESS_PATH, ACCESS_TAB_PATH, MEMBERS_PATH, SSO_PATH } from "@/config";
import { GroupList } from "@/features/access/GroupList";
import { MemberList } from "@/features/access/MemberList";
import { NewRoleDialog, RoleMatrix } from "@/features/access/RoleMatrix";
import { RoleTable } from "@/features/access/RoleTable";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

const tabs = ["roles", "matrix", "members", "groups", "sso"] as const;

export type AccessTab = (typeof tabs)[number];

function isTab(value: string): value is AccessTab {
  return (tabs as readonly string[]).includes(value);
}

// The tests find a tab by these words, whatever language the page speaks.
const tabHooks: Record<AccessTab, string> = { roles: "Roles", matrix: "Matrix", members: "Members", groups: "Groups", sso: "Single sign-on" };

/** The Access page opens on its first tab. */
export const accessRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ACCESS_PATH,
  beforeLoad: () => {
    throw redirect({ to: ACCESS_TAB_PATH, params: { tab: "roles" }, replace: true });
  },
});

/** Each tab is a route of its own, so a tab can be linked, survives a reload and steps back with Back. */
export const accessTabRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ACCESS_TAB_PATH,
  beforeLoad: ({ params }) => {
    if (!isTab(params.tab)) throw redirect({ to: ACCESS_TAB_PATH, params: { tab: "roles" }, replace: true });
  },
  component: () => (
    <AdminsOnly title={t.accessAdmin.title} refusal={t.accessAdmin.notAdmin}>
      <AccessPage />
    </AdminsOnly>
  ),
});

/** The members' page of before, kept so its links and bookmarks still arrive. */
export const membersRedirectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: MEMBERS_PATH,
  beforeLoad: () => {
    throw redirect({ to: ACCESS_TAB_PATH, params: { tab: "members" }, replace: true });
  },
});

/** The single sign-on page of before, kept so its links and bookmarks still arrive. */
export const ssoRedirectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SSO_PATH,
  beforeLoad: () => {
    throw redirect({ to: ACCESS_TAB_PATH, params: { tab: "sso" }, replace: true });
  },
});

function AccessPage() {
  const { tab: param } = accessTabRoute.useParams();
  const tab: AccessTab = isTab(param) ? param : "roles";
  const navigate = useNavigate();
  const panelId = useId();
  return (
    <Page width="content" data-access-page={tab}>
      <PageHeader title={t.accessAdmin.title} />
      <p className="mb-3 text-sm text-ink-muted">{t.accessAdmin.intro}</p>
      <Tabs
        label={t.accessAdmin.title}
        value={tab}
        onChange={(next) => void navigate({ to: ACCESS_TAB_PATH, params: { tab: next } })}
        panelId={panelId}
        className="mb-4"
        tabs={tabs.map((each) => ({ value: each, label: t.accessAdmin.tabs[each], attrs: { "data-access-tab": tabHooks[each] } }))}
      />
      <TabPanel id={panelId} label={t.accessAdmin.tabs[tab]}>
        {tab === "roles" && <RolesTab />}
        {tab === "matrix" && <RoleMatrix />}
        {tab === "members" && <MemberList />}
        {tab === "groups" && <GroupList />}
        {tab === "sso" && (
          <div className="max-w-2xl" data-sso>
            <ProviderSettings />
          </div>
        )}
      </TabPanel>
    </Page>
  );
}

function RolesTab() {
  const [adding, setAdding] = useState(false);
  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button variant="secondary" icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-role">
          {t.accessAdmin.matrix.newRole}
        </Button>
      </div>
      <RoleTable />
      {adding && <NewRoleDialog onClose={() => setAdding(false)} />}
    </div>
  );
}

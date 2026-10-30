import { Link, Outlet, createRoute, notFound, redirect, useLocation, useNavigate } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Button, Card, EmptyState, Menu, Page, PageHeader, SectionTitle, type MenuEntry } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ACCESS_TAB_PATH, ADMIN_PATH, SETTINGS_ACCESS_TAB_PATH, SETTINGS_MOVED_PATH } from "@/config";
import { NavItem } from "@/features/shell/nav";
import { ADMIN_SECTIONS, adminPlaces, type AdminSection, type Place } from "@/features/shell/places";
import { JoinRequestsCount } from "@/features/shell/Sidebar";
import { useNarrow } from "@/features/shell/state";
import { t } from "@/i18n";
import { appRoute } from "./app";

// Adapted from Armature's and Stator's Admin pages. The layout has no path of its own, so every page below keeps its
// whole address as a plain string, which scripts/e2e-reach.mjs reads routes by.

/** Everything that is the installation's, for its administrators; anybody else is refused here once, and no page below mounts. */
export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "admin",
  component: AdminLayout,
});

/** The Admin page: every page of the installation with what it is for. */
export const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: ADMIN_PATH,
  component: AdminIndex,
});

/** The settings pages that moved to Admin, by the name they had under /settings and the address below /admin they have now. */
export const MOVED_SECTIONS: Record<string, string> = {
  installation: "installation",
  "default-theme": "installation",
  access: "access",
  members: "access/members",
  sso: "access/sso",
  "link-types": "link-types",
  "all-tokens": "connected-apps",
  "all-connected-apps": "connected-apps",
  armature: "armature",
  stator: "stator",
  webhooks: "webhooks",
  audit: "audit",
};

/** Where an old /settings/<section> address lives now, with its search and hash, or undefined for one that never moved. */
export function movedAddress(section: string, searchStr = "", hash = ""): string | undefined {
  const moved = Object.hasOwn(MOVED_SECTIONS, section) ? MOVED_SECTIONS[section] : undefined;
  if (!moved) return undefined;
  return `${ADMIN_PATH}/${moved}${searchStr}${hash ? `#${hash}` : ""}`;
}

/** An old /settings/<moved> address, sent on to Admin; any other unknown one is not found. */
export const settingsMovedRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SETTINGS_MOVED_PATH,
  beforeLoad: ({ params, location }) => {
    const to = movedAddress(params.section, location.searchStr, location.hash);
    if (!to) throw notFound();
    throw redirect({ href: to, replace: true });
  },
});

/** A tab of the Access page under its address in the settings area, sent on to the same tab in Admin. */
export const settingsAccessTabRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SETTINGS_ACCESS_TAB_PATH,
  beforeLoad: ({ params }) => {
    throw redirect({ to: ACCESS_TAB_PATH, params: { tab: params.tab }, replace: true });
  },
});

function bySection(): Array<{ section: AdminSection; places: Place[] }> {
  const all = adminPlaces();
  return ADMIN_SECTIONS.map((section) => ({ section, places: all.filter((place) => place.section === section) }));
}

function AdminLayout() {
  const { data: me } = useMe();
  const narrow = useNarrow();
  if (!me?.administers) {
    return (
      <Page width="narrow" data-admin-refused>
        <PageHeader title={t.admin.title} />
        <EmptyState title={t.admin.refusedTitle} description={t.admin.refusedBody} icon={<Icon.Lock />} />
      </Page>
    );
  }
  return (
    <div className={narrow ? "flex flex-col" : "flex items-start gap-6"} data-admin-area>
      {narrow ? <AdminMenu /> : <AdminNav />}
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

/** The sections and their pages as a list beside the page, for wide screens. */
function AdminNav() {
  return (
    <nav aria-label={t.admin.navLabel} className="sticky top-4 w-56 shrink-0 py-2" data-admin-nav>
      <div data-nav="admin">
        <NavItem to={ADMIN_PATH} exact icon="Settings" rail={false}>
          {t.admin.allSections}
        </NavItem>
      </div>
      {bySection().map(({ section, places }) => (
        <div key={section} className="mt-3" data-admin-nav-section={section}>
          <SectionTitle className="mb-1 px-2">{t.admin.sections[section]}</SectionTitle>
          {places.map((place) => (
            <div key={place.id} data-nav={place.id}>
              <NavItem to={place.path} icon={place.icon} rail={false} trailing={place.id === "users" ? <JoinRequestsCount /> : undefined}>
                {place.label}
              </NavItem>
            </div>
          ))}
        </div>
      ))}
    </nav>
  );
}

/** The same list behind one button, for narrow screens. */
function AdminMenu() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const current = adminPlaces().find((place) => pathname === place.path || pathname.startsWith(`${place.path}/`));
  const items: MenuEntry[] = [
    { label: t.admin.allSections, icon: <Icon.Settings />, onSelect: () => void navigate({ to: ADMIN_PATH }), attrs: { "data-nav": "admin" } },
    ...bySection().map(({ section, places }) => ({
      group: t.admin.sections[section],
      items: places.map((place) => {
        const Glyph = Icon[place.icon];
        return { label: place.label, icon: <Glyph />, onSelect: () => void navigate({ to: place.path }), attrs: { "data-nav": place.id } };
      }),
    })),
  ];
  return (
    <div className="mb-2" data-admin-menu>
      <Menu
        label={t.admin.menuLabel}
        items={items}
        trigger={(props) => (
          <Button
            variant="secondary"
            size="sm"
            iconRight={<Icon.ChevronDown />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            data-action="admin-sections"
          >
            {current ? current.label : t.admin.title}
          </Button>
        )}
      />
    </div>
  );
}

/** Every page of the Admin area with what it is for, grouped the way the navigation groups them. */
function AdminIndex() {
  const { data: me } = useMe();
  return (
    <Page width="narrow" data-admin-index>
      <PageHeader title={t.admin.title} meta={me?.installation.name} />
      <p className="mb-6 text-sm text-ink-muted">{t.admin.intro}</p>
      {bySection().map(({ section, places }) => (
        <section key={section} className="mb-8" aria-labelledby={`admin-section-${section}`} data-admin-section={section}>
          <SectionTitle id={`admin-section-${section}`} className="mb-2">
            {t.admin.sections[section]}
          </SectionTitle>
          <Card className="divide-y divide-border">
            {places.map((place) => {
              const Glyph = Icon[place.icon];
              return (
                <Link
                  key={place.id}
                  to={place.path}
                  className="flex items-center gap-3 px-4 py-3 text-ink no-underline hover:bg-surface-raised"
                  data-admin-card={place.id}
                >
                  <Glyph className="shrink-0 text-ink-muted" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-ink">{place.label}</span>
                    <span className="block text-sm text-ink-muted">{place.detail}</span>
                  </span>
                  <Icon.ChevronDown className="shrink-0 -rotate-90 text-ink-subtle" />
                </Link>
              );
            })}
          </Card>
        </section>
      ))}
    </Page>
  );
}

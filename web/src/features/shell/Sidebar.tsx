import { useRef, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useRouter, useRouterState } from "@tanstack/react-router";
import { useJoinRequests, useMe } from "@/api/auth";
import { useUnreadCount } from "@/api/notifications";
import { useWaitingReviews } from "@/api/reviews";
import { Count, IconButton } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { LogoMark } from "@/components/Logo";
import { APP_NAME, NOTIFICATIONS_PATH, REVIEWS_PATH, SEARCH_PATH, SIDEBAR_RAIL_WIDTH, SIDEBAR_WIDTH } from "@/config";
import { t } from "@/i18n";
import { ProjectNav } from "@/features/projects/ProjectNav";
import { AccountMenu } from "./AccountMenu";
import { BuildStamp } from "./BuildStamp";
import { NavItem } from "./nav";
import { adminEntry, inSettings, placesFor } from "./places";
import { Rail } from "./Rail";
import { switcherShortcutHint } from "./shortcut";
import { SidebarGroup } from "./SidebarGroup";
import { DRAWER_ID, SIDEBAR_ID, lastOutsideSettings, useSidebarGroups, useSidebarMode } from "./state";
import { ThemeButton } from "./ThemeButton";

// The sidebar is the map of the work: where you are is highlighted and where you can go is listed in groups that
// fold. The settings area swaps it for a sidebar of its own. On a narrow screen there is no room for a column, so
// the map becomes a drawer, with the theme and the person in its foot.
export function Sidebar({ narrow, drawerOpen, onCloseDrawer }: { narrow: boolean; drawerOpen: boolean; onCloseDrawer: () => void }) {
  const [mode, toggleMode] = useSidebarMode();
  const open = mode === "open";
  const settings = inSettings(useRouterState({ select: (s) => s.location.pathname }));

  if (narrow) {
    return drawerOpen ? (
      <SidebarDrawer onClose={onCloseDrawer} settings={settings}>
        {settings ? <SettingsGroups onNavigate={onCloseDrawer} /> : <SidebarGroups onNavigate={onCloseDrawer} />}
      </SidebarDrawer>
    ) : null;
  }

  return (
    <>
      <Rail open={open} onToggle={toggleMode} controls={SIDEBAR_ID} />
      {open && (
        <nav
          id={SIDEBAR_ID}
          className="flex shrink-0 flex-col border-r border-border bg-surface"
          style={{ width: SIDEBAR_WIDTH }}
          data-sidebar={mode}
          data-settings-sidebar={settings ? "" : undefined}
          data-print-hide
          aria-label={settings ? t.nav.settingsArea : t.nav.whereYouAre}
        >
          {settings ? <BackHead /> : <OrgHead />}
          <div className="flex-1 overflow-y-auto px-2 py-3">{settings ? <SettingsGroups /> : <SidebarGroups />}</div>
          <BuildStamp />
        </nav>
      )}
    </>
  );
}

/** The installation's name, beside the mark. */
function OrgHead({ children }: { children?: ReactNode }) {
  const { data: me } = useMe();
  return (
    <div className="flex h-12 items-center gap-2 border-b border-border px-3" data-sidebar-head>
      <LogoMark className="shrink-0 rounded-control" />
      <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink" data-sidebar-org>
        {me?.installation.name ?? APP_NAME}
      </span>
      {children}
    </div>
  );
}

/** The settings sidebar's head: the way back to the last page outside the settings area. */
function BackHead({ onNavigate, children }: { onNavigate?: () => void; children?: ReactNode }) {
  const router = useRouter();
  const back = lastOutsideSettings();
  // The address is a whole href, search included, so the router's history takes it as it was noted.
  function go(event: MouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onNavigate?.();
    router.history.push(back);
  }
  return (
    <div className="flex h-12 items-center gap-2 border-b border-border px-2">
      <a
        href={back}
        onClick={go}
        className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-control px-2 text-sm font-medium text-ink hover:bg-surface-raised"
        data-action="back-from-settings"
      >
        <Icon.ChevronDown className="shrink-0 rotate-90" />
        <span className="min-w-0 truncate">{t.nav.backTo(APP_NAME)}</span>
      </a>
      {children}
    </div>
  );
}

function SidebarGroups({ onNavigate }: { onNavigate?: () => void }) {
  const { data: me } = useMe();
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;
  const unread = useUnreadCount().data ?? 0;
  const waiting = useWaitingReviews().data?.total ?? 0;
  const counts: Record<string, ReactNode> = {
    [NOTIFICATIONS_PATH]: <Count count={unread} name="notifications" says={t.nav.unread(unread)} />,
    [REVIEWS_PATH]: <Count count={waiting} name="reviews" says={t.review.inboxButton(waiting)} />,
    // On the current item the hint takes the accent: subtle ink is too faint on the accent tint in the dark palette.
    [SEARCH_PATH]: (
      <kbd aria-hidden="true" className="font-mono text-2xs text-ink-subtle in-data-[status=active]:text-accent" data-shortcut>
        {switcherShortcutHint()}
      </kbd>
    ),
  };
  const places = placesFor(me);
  const pages = places.filter((place) => place.area === "work");
  const admin = adminEntry();
  return (
    <>
      <SidebarGroup id="requirements" title={t.nav.groupRequirements} open={isOpen("requirements", true)} onToggle={() => toggleGroup("requirements", true)}>
        {pages.map((place) => (
          <NavItem
            key={place.path}
            to={place.path}
            exact={place.path === "/"}
            icon={place.icon}
            rail={false}
            trailing={counts[place.path]}
            onNavigate={onNavigate}
          >
            {place.label}
          </NavItem>
        ))}
      </SidebarGroup>
      <ProjectNav open={isOpen("project", true)} onToggle={() => toggleGroup("project", true)} onNavigate={onNavigate} />
      {me?.administers && (
        <div className="mt-3" data-nav={admin.id}>
          <NavItem to={admin.path} icon={admin.icon} rail={false} trailing={<JoinRequestsCount />} onNavigate={onNavigate}>
            {admin.label}
          </NavItem>
        </div>
      )}
    </>
  );
}

/** How many people wait to be let in, beside Admin and Users; only an administrator's sidebar asks. */
export function JoinRequestsCount() {
  const waiting = useJoinRequests().data?.length ?? 0;
  return <Count count={waiting} name="join-requests" says={t.sso.waitingCount(waiting)} />;
}

/** The settings area's group: the person's own pages. The installation's are under Admin. */
function SettingsGroups({ onNavigate }: { onNavigate?: () => void }) {
  const { data: me } = useMe();
  const [groups, toggleGroup] = useSidebarGroups();
  const you = placesFor(me).filter((place) => place.area === "you");
  return (
    <SidebarGroup id="you" title={t.nav.groupYou} open={groups.you ?? true} onToggle={() => toggleGroup("you", true)}>
      {you.map((place) => (
        <NavItem key={place.path} to={place.path} icon={place.icon} rail={false} onNavigate={onNavigate}>
          {place.label}
        </NavItem>
      ))}
    </SidebarGroup>
  );
}

// A modal panel from the left: the page stays in view behind the scrim but out
// of reach, focus is held inside, and it hands focus back to the menu button.
function SidebarDrawer({ onClose, settings, children }: { onClose: () => void; settings: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);
  const close = <IconButton icon={<Icon.X />} label={t.nav.closeDrawer} size="sm" onClick={onClose} data-focus-last="" data-action="close-drawer" />;
  // Closing on the press would let the rest of the click land on the page and
  // take the focus that was just handed back to the menu button.
  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the drawer with Escape or its close button
    <div className="fixed inset-0 z-50 flex bg-ink/30" onClick={(e) => e.target === e.currentTarget && onClose()} data-drawer-backdrop>
      <div
        ref={ref}
        id={DRAWER_ID}
        role="dialog"
        aria-modal="true"
        aria-label={t.nav.drawer}
        // A strip of the page as wide as the rail stays uncovered, so there
        // is always somewhere to tap the drawer shut.
        style={{ width: `min(${SIDEBAR_WIDTH}px, calc(100vw - ${SIDEBAR_RAIL_WIDTH}px))` }}
        className="flex h-full flex-col border-r border-border bg-surface shadow-2 motion-safe:animate-drawer-in"
        data-sidebar="drawer"
        data-settings-sidebar={settings ? "" : undefined}
      >
        {settings ? <BackHead onNavigate={onClose}>{close}</BackHead> : <OrgHead>{close}</OrgHead>}
        <nav aria-label={settings ? t.nav.settingsArea : t.nav.whereYouAre} className="flex-1 overflow-y-auto px-2 py-3">
          {children}
        </nav>
        <div className="flex items-center gap-2 border-t border-border px-2 py-2">
          <ThemeButton side="top" />
          <AccountMenu side="top" align="start" />
          <div className="min-w-0 flex-1 text-right [&>[data-build]]:p-0">
            <BuildStamp />
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}

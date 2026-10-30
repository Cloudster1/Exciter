import { IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PROJECTS_PATH, SEARCH_PATH, SIDEBAR_RAIL_WIDTH } from "@/config";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { t } from "@/i18n";
import { AccountMenu } from "./AccountMenu";
import { NavItem } from "./nav";
import { ThemeButton } from "./ThemeButton";

// The rail is what is always there on a wide screen: the places every page reaches from, the bell, and at the
// bottom the theme and the person. When the sidebar is folded away, it is the whole chrome.
export function Rail({ open, onToggle, controls }: { open: boolean; onToggle: () => void; controls: string }) {
  return (
    <aside className="flex shrink-0 flex-col items-center border-r border-border bg-surface" style={{ width: SIDEBAR_RAIL_WIDTH }} data-rail data-print-hide>
      {/* The toggle opens and closes the navigation, so it sits inside it, not in a banner of its own. */}
      <nav aria-label={t.nav.everywhere} className="flex w-full flex-1 flex-col items-center">
        <div className="flex h-12 w-full items-center justify-center border-b border-border" data-rail-top>
          <IconButton
            icon={open ? <Icon.Collapse /> : <Icon.Expand />}
            label={open ? t.nav.collapseSidebar : t.nav.expandSidebar}
            size="sm"
            onClick={onToggle}
            aria-expanded={open}
            aria-controls={open ? controls : undefined}
            data-action="sidebar"
          />
        </div>
        <div className="flex flex-1 flex-col items-center gap-1 py-3">
          <NavItem to="/" exact icon="Home" rail>
            {t.nav.home}
          </NavItem>
          <NavItem to={SEARCH_PATH} icon="Search" rail>
            {t.nav.search}
          </NavItem>
          <NotificationBell side="right" />
          <NavItem to={PROJECTS_PATH} icon="Folder" rail>
            {t.nav.projects}
          </NavItem>
        </div>
      </nav>
      {/* As wide as the rail, unlike its centred controls, so the account menu's anchor reaches the rail's edge. */}
      <div className="flex w-full flex-col items-center gap-1 border-t border-border py-2" data-rail-foot>
        <ThemeButton />
        <AccountMenu side="right" />
      </div>
    </aside>
  );
}

import { useCallback, useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { useRouterState } from "@tanstack/react-router";
import { IconButton, ShellHeaderContext } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BackdropEffect } from "@/features/themes/BackdropEffect";
import { ThemeLoader } from "@/features/themes/ThemeLoader";
import { t } from "@/i18n";
import { PlaceCrumbs } from "./Crumbs";
import { inSettings } from "./places";
import { QuickSwitcher } from "./QuickSwitcher";
import { Sidebar } from "./Sidebar";
import { DRAWER_ID, rememberOutsideSettings, useNarrow } from "./state";

const MAIN_ID = "main";

// Ctrl or Cmd+K anywhere in the shell, fields included, as in Armature and
// Stator: it is the product's one shortcut and a field has no other use for it.
export function useSwitcherShortcut(onOpen: () => void) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && !event.repeat && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpen();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onOpen]);
}

/** The chrome every page sits in: rail, sidebar, and the content under its sticky strip of crumbs and head. No top
 *  bar: on a narrow screen a thin strip opens the drawer. */
export function AppShell({ children }: { children: ReactNode }) {
  const [switching, setSwitching] = useState(false);
  const openSwitcher = useCallback(() => setSwitching(true), []);
  const closeSwitcher = useCallback(() => setSwitching(false), []);
  useSwitcherShortcut(openSwitcher);
  // The page's head is drawn into the strip over the content, which floats
  // as the content scrolls under it; the ref lands before first paint.
  const [strip, setStrip] = useState<HTMLElement | null>(null);

  const narrow = useNarrow();
  const [drawer, setDrawer] = useState(false);
  const closeDrawer = useCallback(() => setDrawer(false), []);
  // A route change from anywhere, Ctrl K included, means the reader has gone where they meant to.
  // Adjusted during render rather than in an effect, so the drawer is never drawn over the new page.
  const location = useRouterState({ select: (s) => s.location });
  const pathname = location.pathname;
  const [drawerPath, setDrawerPath] = useState(pathname);
  if (drawerPath !== pathname) {
    setDrawerPath(pathname);
    setDrawer(false);
  }
  const drawerOpen = narrow && drawer;
  useEffect(() => {
    if (!inSettings(pathname)) rememberOutsideSettings(location.href);
  }, [pathname, location.href]);

  return (
    <div className="flex h-full" data-shell>
      <a
        href={`#${MAIN_ID}`}
        onClick={skipToMain}
        className="sr-only z-60 rounded-control bg-surface-overlay px-3 py-2 text-sm font-medium text-ink shadow-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
        data-skip-link
      >
        {t.shell.skipToContent}
      </a>
      <Sidebar narrow={narrow} drawerOpen={drawerOpen} onCloseDrawer={closeDrawer} />
      <div className="flex min-w-0 flex-1 flex-col">
        {narrow && (
          <div className="flex h-10 shrink-0 items-center border-b border-border bg-surface px-2" data-narrow-strip data-print-hide>
            <IconButton
              icon={<Icon.Menu />}
              label={t.nav.openDrawer}
              size="sm"
              onClick={() => setDrawer(true)}
              aria-haspopup="dialog"
              aria-expanded={drawerOpen}
              aria-controls={drawerOpen ? DRAWER_ID : undefined}
              data-action="drawer"
            />
          </div>
        )}
        {/* Focusable only from the skip link, and not ringed then: it is where reading starts, not a control. */}
        <ThemeLoader />
        <main id={MAIN_ID} tabIndex={-1} className="relative isolate min-h-0 flex-1 overflow-auto bg-backdrop focus:outline-none" data-backdrop-host>
          <BackdropEffect />
          <div className="sticky top-0 z-20 border-b border-border/60 bg-surface-glass px-4 pt-3 backdrop-blur-md shell:px-8" data-shell-strip>
            <PlaceCrumbs />
            <div ref={setStrip} className="pt-2 empty:hidden" data-shell-header />
          </div>
          <ShellHeaderContext.Provider value={strip}>
            <div className="px-4 py-6 shell:px-8">{children}</div>
          </ShellHeaderContext.Provider>
        </main>
      </div>
      {switching && <QuickSwitcher onClose={closeSwitcher} />}
    </div>
  );
}

// The router owns the address, so the skip link moves focus itself rather
// than letting a hash change reach the history.
function skipToMain(event: MouseEvent<HTMLAnchorElement>) {
  event.preventDefault();
  document.getElementById(MAIN_ID)?.focus();
}

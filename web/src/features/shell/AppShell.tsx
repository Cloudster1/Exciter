import { useCallback, useState, type MouseEvent, type ReactNode } from "react";
import { useRouterState } from "@tanstack/react-router";
import { ShellHeaderContext } from "@/components/ui";
import { t } from "@/i18n";
import { QuickSwitcher } from "./QuickSwitcher";
import { Sidebar } from "./Sidebar";
import { useNarrow } from "./state";
import { TopBar, useSwitcherShortcut } from "./TopBar";

const MAIN_ID = "main";

/** The chrome every page sits in: rail, sidebar, top bar, and the content with its floating head. */
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
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [drawerPath, setDrawerPath] = useState(pathname);
  if (drawerPath !== pathname) {
    setDrawerPath(pathname);
    setDrawer(false);
  }
  const drawerOpen = narrow && drawer;

  return (
    <div className="flex h-full">
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
        <TopBar narrow={narrow} drawerOpen={drawerOpen} onOpenDrawer={() => setDrawer(true)} switching={switching} onSwitch={openSwitcher} />
        {/* Focusable only from the skip link, and not ringed then: it is where reading starts, not a control. */}
        <main id={MAIN_ID} tabIndex={-1} className="relative isolate min-h-0 flex-1 overflow-auto bg-backdrop focus:outline-none">
          <div
            ref={setStrip}
            className="sticky top-0 z-20 border-b border-border/60 bg-surface-glass px-4 pt-5 backdrop-blur-md empty:hidden shell:px-8"
            data-shell-header
          />
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

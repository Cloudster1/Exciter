import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

/** Below this width the shell is a drawer, as web/src/config.ts SIDEBAR_DRAWER_BELOW_PX says. */
const DRAWER_BELOW_PX = 768;

/** Opens a project's folded Project admin group in the sidebar, where its trash, settings and permissions are. */
export async function openProjectAdmin(page: Page, projectKey: string): Promise<void> {
  const fold = page.locator(`[data-project-nav="${projectKey}"]`).getByRole("button", { name: "Project admin" });
  if ((await fold.getAttribute("aria-expanded")) !== "true") await fold.click();
}

/** The palettes the theme button steps through, as web/src/lib/theme.ts stores them. */
export type ColourScheme = "light" | "dark";
const THEME_STORAGE_KEY = "exciter.theme";

/** Makes every page this browser opens start in the given palette, the way a stored choice does. */
export async function startInScheme(page: Page, scheme: ColourScheme): Promise<void> {
  await page.addInitScript(
    ([key, value]) => {
      try {
        localStorage.setItem(key, value);
      } catch {
        // An opaque origin has no storage; the page it opens is not ours.
      }
    },
    [THEME_STORAGE_KEY, scheme] as const,
  );
}

/** Opens a path and waits until the shell has drawn the page. */
export async function openShell(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await expect(page.locator("main#main")).toBeVisible();
}

// color-contrast is what jsdom cannot judge, and region is the rule a portal drawn outside every landmark trips;
// both are named so a change of axe's defaults cannot quietly drop them.
const RULES_ON = { "color-contrast": { enabled: true }, region: { enabled: true } };

// Findings this suite surfaced that wait on a fix of their own, each matched as narrowly as it can be. Empty is
// the goal; add an entry only with its issue, and delete it once that is fixed.
const KNOWN_FINDINGS: Array<{ rule: string; matches: (page: Page, node: AxeNode) => Promise<boolean> }> = [];

type AxeNode = Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"][number]["nodes"][number];

// What one axe run may add to its test's time: traces of busy CI shards show a run taking 12 to 13 of the 20
// seconds a test has, which ran tests out of time with nothing wrong in them (#389).
const AXE_ALLOWANCE_MS = 15_000;

/** Runs axe over the whole page and fails with every violation it finds, adding its time to the test's own. */
export async function expectAccessible(page: Page): Promise<void> {
  const info = test.info();
  info.setTimeout(info.timeout + AXE_ALLOWANCE_MS);
  const results = await new AxeBuilder({ page }).options({ rules: RULES_ON }).analyze();
  const found: string[] = [];
  for (const violation of results.violations) {
    for (const node of violation.nodes) {
      const known = KNOWN_FINDINGS.find((each) => each.rule === violation.id);
      if (known && (await known.matches(page, node))) continue;
      found.push(`${violation.id} (${violation.impact}) at ${node.target.join(" ")}: ${node.failureSummary ?? violation.help}`);
    }
  }
  expect(found, "axe found accessibility violations").toEqual([]);
}

/** Whether the page scrolls sideways, which a narrow screen must never do. */
export async function scrollsSideways(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}

/** Opens the account menu from the avatar: at the foot of the rail on a wide screen, in the drawer's foot on a phone. */
export async function openAccountMenu(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? DRAWER_BELOW_PX) < DRAWER_BELOW_PX) await page.locator('[data-action="drawer"]').click();
  await page.locator('[data-action="account"]').click();
  await expect(page.getByRole("menu")).toBeVisible();
}

/** An item of the open menu, by its data-action. */
export const menuItem = (page: Page, action: string) => page.locator(`[role="menu"] [data-action="${action}"]`);

/** Answers the open ConfirmDialog with its danger button and waits for it to close. */
export async function confirm(page: Page): Promise<void> {
  const dialog = page.locator("[data-confirm]");
  await dialog.locator('[data-action="confirm"]').click();
  await expect(dialog).toHaveCount(0);
}

/** Closes every toast in the corner, which would otherwise sit over the controls beneath it. */
export async function dismissToasts(page: Page): Promise<void> {
  const close = page.locator('[data-toast] [data-action="dismiss-toast"]');
  // A toast may time out between counting and clicking.
  for (let left = await close.count(); left > 0; left--)
    await close
      .first()
      .click({ timeout: 1000 })
      .catch(() => {});
}

/** The sentence of the latest toast that says it, without the dismiss button's name. */
export function toast(page: Page, text: string | RegExp) {
  // Matched on the message alone, so a pattern anchored at its end is not thrown by the dismiss button's name.
  return page.locator("[data-toast-message]").filter({ hasText: text }).last();
}

import { expect, type Locator, type Page } from "@playwright/test";

// Read-your-writes holds a session to its own writes only, so a read of anybody else's write retries until it
// sees what only that write shows. Never raise a timeout to cover the lag, nor wait for what an older answer shows.

/** How long one look waits before the page is loaded again. */
export const ONE_LOOK = { timeout: 2_000 } as const;

/** Loads path until check passes; check's own waits take ONE_LOOK.
 *  Another session's write may reach this read a replica later. */
export async function openUntil(page: Page, path: string, check: () => Promise<unknown>): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await check();
  }).toPass();
}

/** Loads path until it shows `shown`: words within main, or a locator made visible.
 *  Another session's write may reach this read a replica later. */
export async function openShowing(page: Page, path: string, shown: string | Locator): Promise<void> {
  await openUntil(page, path, () =>
    typeof shown === "string" ? expect(page.locator("main")).toContainText(shown, ONE_LOOK) : expect(shown).toBeVisible(ONE_LOOK),
  );
}

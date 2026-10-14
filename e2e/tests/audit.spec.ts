import { randomUUID } from "node:crypto";
import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";

const AUDIT_PATH = "/settings/audit";
// web/src/config.ts AUDIT_PAGE_SIZE: the page holds this many, so more spill onto a second.
const PAGE_SIZE = 50;
const SPILL = 7;
// An action the facets list once the planted entries are there; they may come from a replica further behind.
const PLANTED_ACTION = "theme.default_set";
const FACET_PLANTED = `#audit-action option[value="${PLANTED_ACTION}"]`;
// audit.CSVHeader, the export's first line.
const CSV_HEADER = "time,action,actor_id,actor,target_type,target_id,ip,data";
const REFUSED = /^Only an administrator can do that\. Ask one/;
const NOT_ADMIN = "Only an administrator reads the audit log.";

// Written straight into the record, a second apart: making sixty through the API would only be slower at it.
/** A theme the log names, and changes to it beyond one page of the log, all by alice. */
async function busyTarget(api: ExciterApi, name: string): Promise<{ id: string; name: string }> {
  const id = randomUUID();
  const me = (await api.GET("/auth/me")).data!;
  await withDatabase(async (db) => {
    await db.query(
      `INSERT INTO audit_log (actor_user_id, action, target_type, target_id, data, created_at)
       SELECT $1, $2, 'theme', $3, jsonb_build_object('name', $4::text),
              now() - make_interval(secs => n)
       FROM generate_series(1, $5::int) AS n`,
      [me.user.id, PLANTED_ACTION, id, name, PAGE_SIZE + SPILL],
    );
  });
  return { id, name };
}

// A page of the log holds two buttons a row, so reaching the pager takes a hundred presses and more.
const MAX_TABS = 300;

/** Presses Tab until the control has focus, as a person working the page by keyboard would. */
async function tabTo(page: Page, control: Locator): Promise<void> {
  // Asked through a locator, each of the hundred checks queried the whole page again.
  const target = await control.elementHandle();
  try {
    for (let i = 0; i < MAX_TABS; i++) {
      if (await target.evaluate((el) => el === document.activeElement)) return;
      await page.keyboard.press("Tab");
    }
  } finally {
    await target.dispose();
  }
  await expect(control).toBeFocused();
}

// Other tests and specs planted the same action in the installation, so only a row naming this target
// shows that its changes have arrived.
const plantedRow = (page: Page, name: string) => page.locator(`[data-audit-row="${PLANTED_ACTION}"]`).filter({ hasText: name }).first();

/** Opens the log and waits for the changes planted on the target named. */
async function openLog(page: Page, name: string): Promise<void> {
  await openUntil(page, AUDIT_PATH, async () => {
    await expect(plantedRow(page, name)).toBeVisible(ONE_LOOK);
    await expect(page.locator(FACET_PLANTED)).toBeAttached(ONE_LOOK);
  });
}

test.describe("the audit log", () => {
  test("an administrator narrows it to one theme's changes and pages through them", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const target = await busyTarget(api, uniqueName(testInfo, "Busy"));
    await openLog(page, target.name);
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Audit log");

    await page.getByLabel("Action").selectOption(PLANTED_ACTION);
    await expect(page.locator("[data-audit-row]").first()).toHaveAttribute("data-audit-row", PLANTED_ACTION);
    await page
      .getByRole("button", { name: `Show only entries about ${target.name}` })
      .first()
      .click();
    await expect(page.locator("[data-audit-target-filter]")).toHaveText(new RegExp(`Only entries about ${target.name}`));
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 1");

    await page.locator('[data-action="audit-older"]').click();
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 2");
    await expect(page.locator("[data-audit-row]")).toHaveCount(SPILL);
    await expect(page.locator('[data-action="audit-older"]')).toBeDisabled();
    await page.locator('[data-action="audit-newer"]').click();
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 1");
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);

    const href = await page.locator('[data-action="export-audit"]').getAttribute("href");
    expect(href).toContain(`target=${target.id}`);
    const csv = await page.request.get(href!);
    expect(csv.status()).toBe(200);
    const lines = (await csv.text()).trim().split("\n");
    expect(lines).toHaveLength(PAGE_SIZE + SPILL + 1);
    expect(lines[0]).toBe(CSV_HEADER);

    await expect(async () => {
      const exported = (await api.GET("/audit", { params: { query: { action: "audit.exported" } } })).data!.entries;
      expect(exported.some((entry) => (entry.data as { targetId?: string }).targetId === target.id)).toBe(true);
    }).toPass();

    await page.locator('[data-action="clear-audit-filters"]').click();
    await expect(page.getByLabel("Action")).toHaveValue("");
    await expect(page.locator("[data-audit-target-filter]")).toHaveCount(0);
  });

  test("bob, a member, is refused it", async ({ apiAs, pageAs }) => {
    const bobApi = await apiAs("bob");
    const refused = await bobApi.GET("/audit");
    expect(refused.response.status).toBe(403);
    expect((refused.error as { error?: { message?: string } } | undefined)?.error?.message).toMatch(REFUSED);

    const bob = await pageAs("bob");
    await bob.goto(AUDIT_PATH);
    await expect(bob.locator("[data-admins-only]")).toContainText(NOT_ADMIN);
    await expect(bob.locator("[data-audit-row]")).toHaveCount(0);
  });

  test("it is narrowed and paged from the keyboard", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const target = await busyTarget(api, uniqueName(testInfo, "Keys"));
    await openLog(page, target.name);

    await page.getByLabel("Action").focus();
    await tabTo(page, page.getByRole("button", { name: `Show only entries about ${target.name}` }).first());
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-audit-target-filter]")).toBeVisible();
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);

    await tabTo(page, page.locator('[data-action="audit-older"]'));
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 2");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`it passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const target = await busyTarget(api, uniqueName(testInfo, `Axe ${scheme}`));
      await startInScheme(page, scheme);
      await openLog(page, target.name);
      await expectAccessible(page);
    });
  }

  test("on a phone the filters stack and the page never scrolls sideways", { tag: "@mobile" }, async ({ page, api }, testInfo) => {
    const target = await busyTarget(api, uniqueName(testInfo, "Phone"));
    await openLog(page, target.name);
    await expect(page.getByLabel("Action")).toBeVisible();
    await expect(page.getByLabel("From")).toBeVisible();
    await expect(page.locator('[data-action="export-audit"]')).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
    await page.getByLabel("Action").selectOption(PLANTED_ACTION);
    await expect(page.locator("[data-audit-row]").first()).toHaveAttribute("data-audit-row", PLANTED_ACTION);
    expect(await scrollsSideways(page)).toBe(false);
  });
});

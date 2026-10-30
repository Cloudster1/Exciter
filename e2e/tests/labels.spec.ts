import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("TAGS");
// Labels are the installation's, so this one carries a word no other worker or run has.
const WORD = uniqueKey("W").toLowerCase();
const TYPED = `Safety ${WORD}`;
const LABEL = `safety-${WORD}`;
// Enough of the label typed to suggest it among any others that begin alike.
const SUGGEST_FROM = LABEL.length - 2;
const PAGED = `paged-${WORD}`;
// web/src/config.ts LABEL_PAGE_SIZE: one requirement past it makes a second page.
const LABEL_PAGE_SIZE = 20;

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

// The project's key and the label's word are this worker's own, so they meet no other spec's.
test.describe("labels", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice coins a label, puts it on a second requirement from the suggestions, and finds both by it", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Tagged" } }));
    for (const title of ["Pay the basket", "Refund the basket", "Print the receipt"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    const labels = page.locator("[data-requirement-labels]");
    await expect(labels).toContainText("No labels yet.");
    const box = labels.getByRole("combobox", { name: "Add a label" });
    await box.fill(TYPED);
    await box.press("Enter");
    await expect(labels.locator(`[data-label="${LABEL}"]`)).toBeVisible();
    await expect(page.getByText(`Added the label ${LABEL}.`)).toBeAttached();
    await expect(page.locator("[data-revision]")).toHaveAttribute("data-revision", "1");
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/r/${KEY}-R2`);
    const second = page.locator("[data-requirement-labels]").getByRole("combobox", { name: "Add a label" });
    await second.fill(LABEL.slice(0, SUGGEST_FROM));
    await page.locator(`[data-label-option="${LABEL}"]`).click();
    await expect(page.locator(`[data-requirement-labels] [data-label="${LABEL}"]`)).toBeVisible();

    await page.locator(`[data-requirement-labels] [data-label="${LABEL}"]`).getByRole("link").click();
    await expect(page).toHaveURL(`/projects/${KEY}?label=${LABEL}`);
    await expect(page.locator("[data-label-filter]")).toContainText(`Showing requirements labelled ${LABEL}`);
    await expect(page.locator("[data-requirement-row]")).toHaveCount(2);
    await expect(page.locator(`[data-requirement-row="${KEY}-R3"]`)).toHaveCount(0);
    await expectAccessible(page);

    await page.locator('[data-label-filter] [data-action="clear-label"]').click();
    await expect(page).toHaveURL(`/projects/${KEY}`);
    await expect(page.locator("[data-label-filter]")).toHaveCount(0);
    await expect(page.locator("[data-requirement-row]")).toHaveCount(3);
    await page.goBack();
    await expect(page.locator("[data-requirement-row]")).toHaveCount(2);

    await page.locator('[data-label-filter] [data-action="label-everywhere"]').click();
    await expect(page).toHaveURL(`/labels/${LABEL}`);
    await expect(page.locator("[data-label-count]")).toHaveText("2 requirements");
    await expect(page.locator(`[data-labelled-requirement="${KEY}-R1"]`)).toContainText("Pay the basket");
    await expectAccessible(page);

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await page.getByRole("button", { name: `Remove label ${LABEL}` }).click();
    await expect(page.locator("[data-requirement-labels]")).toContainText("No labels yet.");
    await page.goto(`/projects/${KEY}/r/${KEY}-R1/history?tab=activity`);
    await expect(page.locator('[data-activity-entry="change"]').first()).toContainText(`changed the labels from ${LABEL} to nothing`);
  });

  test("alice steps through the requirements carrying a label a page at a time, and back", async ({ page, api }) => {
    test.setTimeout(60_000);
    const path = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Paged" } }));
    const last = LABEL_PAGE_SIZE + 1;
    for (let n = 1; n <= last; n++) must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: { title: `Step ${n}`, labels: [PAGED] } }));

    await page.goto(`/labels/${PAGED}?project=${KEY}`);
    const count = page.locator("[data-label-count]");
    const paging = page.locator("[data-label-paging]");
    const listed = page.locator("[data-labelled-requirement]");
    const previous = page.locator('[data-action="previous-labelled"]');
    const next = page.locator('[data-action="next-labelled"]');
    await expect(count).toHaveText(`1 to ${LABEL_PAGE_SIZE} of ${last} requirements`);
    await expect(paging).toContainText("Page 1 of 2");
    await expect(listed).toHaveCount(LABEL_PAGE_SIZE);
    await expect(previous).toBeDisabled();

    await next.click();
    await expect(page).toHaveURL(/[?&]page=2(&|$)/);
    await expect(count).toHaveText(`${last} to ${last} of ${last} requirements`);
    await expect(paging).toContainText("Page 2 of 2");
    await expect(listed).toHaveCount(1);
    await expect(page.locator(`[data-labelled-requirement="${KEY}-R${last}"]`)).toContainText(`Step ${last}`);
    await expect(next).toBeDisabled();
    await expectAccessible(page);

    await previous.click();
    await expect(page).not.toHaveURL(/[?&]page=/);
    await expect(count).toHaveText(`1 to ${LABEL_PAGE_SIZE} of ${last} requirements`);
    await expect(page.locator(`[data-labelled-requirement="${KEY}-R1"]`)).toContainText("Step 1");
  });
});

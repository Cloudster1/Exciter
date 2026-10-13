import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible } from "../fixtures/shell";

const KEY = "TRACE";
const MADE = "mitigates";

/** Removes the project and the link type the test made, so each test starts without them. */
async function cleanUp(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (found.data) {
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
  const types = await api.GET("/link-types");
  const made = types.data?.linkTypes.find((each) => each.key === MADE);
  if (made) await api.DELETE("/link-types/{linkTypeID}", { params: { path: { linkTypeID: made.id } } });
}

async function makeProject(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Traced" } }));
  for (const title of ["Pay the basket", "Take the payment", "Charge the card"]) {
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
  }
}

/** Links the requirement open now to another found by its key, read the way the type option says. */
async function link(page: import("@playwright/test").Page, way: string, other: string) {
  const form = page.getByRole("form", { name: "Add a link" });
  await form.getByRole("combobox", { name: "Link type" }).selectOption({ label: way });
  await form.getByRole("combobox", { name: "Requirement" }).fill(other);
  await page.locator(`[data-picker-option="${other}"]`).click();
  await form.getByRole("button", { name: "Link" }).click();
}

// The organization is this file's alone, so its link types meet no other spec's.
test.describe("trace links", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice links requirements, reads a link from both ends, is refused a circle, and removes a link", async ({ page, api }) => {
    await makeProject(api);
    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const panel = page.locator("[data-requirement-links]");
    await expect(panel).toContainText("Nothing is linked to this requirement yet.");
    await link(page, "This requirement derives from", `${KEY}-R2`);
    await expect(panel.locator(`[data-link-group="derives:outgoing"] [data-link="${KEY}-R2"]`)).toContainText("Take the payment");
    await expect(page.getByText(`Linked: derives from ${KEY}-R2.`)).toBeAttached();
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/r/${KEY}-R2`);
    await expect(panel.locator(`[data-link-group="derives:incoming"] [data-link="${KEY}-R1"]`)).toContainText("Pay the basket");
    await link(page, "This requirement derives from", `${KEY}-R3`);
    await expect(panel.locator(`[data-link="${KEY}-R3"]`)).toBeVisible();

    await page.goto(`/p/${KEY}/r/${KEY}-R3`);
    await link(page, "This requirement derives from", `${KEY}-R1`);
    await expect(panel.getByRole("alert")).toContainText(`${KEY}-R1 already derives from ${KEY}-R3 through ${KEY}-R2`);

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await panel.getByRole("button", { name: `Remove the link derives from ${KEY}-R2` }).click();
    await expect(panel).toContainText("Nothing is linked to this requirement yet.");
    await page.goto(`/p/${KEY}/r/${KEY}-R2/history?tab=activity`);
    await expect(page.locator('[data-activity-entry="change"]').first()).toContainText(`${KEY}-R1`);
  });

  test("alice makes a link type for the organization, and a project turns it off", async ({ page, api }) => {
    await makeProject(api);
    await page.goto("/settings/link-types");
    await expect(page.locator('[data-link-type="derives"]')).toBeVisible();
    await page.locator('[data-action="new-link-type"]').click();
    await page.getByRole("textbox", { name: "Name" }).fill("Mitigates");
    await page.getByRole("textbox", { name: "Reads from the source" }).fill("mitigates");
    await page.getByRole("textbox", { name: "Reads from the target" }).fill("is mitigated by");
    await expectAccessible(page);
    await page.locator('[data-action="save-link-type"]').click();
    await expect(page.locator(`[data-link-type="${MADE}"]`)).toContainText("mitigates / is mitigated by");

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    const types = page.getByRole("form", { name: "Add a link" }).getByRole("combobox", { name: "Link type" });
    await expect(types.locator("option", { hasText: "This requirement mitigates" })).toHaveCount(1);

    await page.goto(`/p/${KEY}/settings?tab=links`);
    // The box shows what the server holds, so it clears once the list is read again.
    await page.getByRole("checkbox", { name: "Use Mitigates in this project" }).click();
    await expect(page.getByRole("checkbox", { name: "Use Mitigates in this project" })).not.toBeChecked();
    await expectAccessible(page);

    await page.goto(`/p/${KEY}/r/${KEY}-R1`);
    await expect(types.locator("option", { hasText: "This requirement derives from" })).toHaveCount(1);
    await expect(types.locator("option", { hasText: "This requirement mitigates" })).toHaveCount(0);
  });
});

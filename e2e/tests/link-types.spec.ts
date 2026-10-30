import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Link types are shaped where the installation's and a project's administrators keep them, and what they change
// shows where a requirement is linked (#313).

const KEY = uniqueKey("LTY");
const OTHER = uniqueKey("LTZ");
// Types for all projects are the installation's, so theirs carry a word of this worker's own.
const WORD = uniqueKey("W").toLowerCase();
const GUARDS = { key: `guards_${WORD}`, name: `Guards ${WORD}`, forward: `guards ${WORD}`, backward: "is guarded by" };
const CHECKS = { key: `checks_${WORD}`, name: `Checks ${WORD}`, forward: `checks ${WORD}`, backward: "is checked by" };
const SHIELDS = `shields ${WORD}`;

async function cleanUp(api: ExciterApi) {
  for (const projectKey of [KEY, OTHER]) {
    const path = { params: { path: { projectKey } } };
    const found = await api.GET("/projects/{projectKey}", path);
    if (!found.data) continue;
    if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
    await api.DELETE("/projects/{projectKey}", path);
  }
  // A type keeps its links until their requirements go with the project, so the types go last.
  const types = await api.GET("/link-types");
  for (const made of types.data?.linkTypes.filter((each) => each.key === GUARDS.key || each.key === CHECKS.key) ?? []) {
    await api.DELETE("/link-types/{linkTypeID}", { params: { path: { linkTypeID: made.id } } });
  }
}

async function makeProject(api: ExciterApi, projectKey: string, titles: string[]) {
  must(await api.POST("/projects", { body: { key: projectKey, name: projectKey } }));
  for (const title of titles) must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey } }, body: { title } }));
}

/** The ways of linking the requirement open now offers, as the type select reads them. */
function linkTypeOptions(page: Page) {
  return page.getByRole("form", { name: "Add a link" }).getByRole("combobox", { name: "Link type" }).locator("option");
}

test.describe("link types", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => cleanUp(api));

  test("alice edits, retires, uses again and deletes types for all projects, and the requirement page follows", async ({ page, api }) => {
    await makeProject(api, KEY, ["Brakes engage", "Brakes hold", "Brakes release"]);
    for (const type of [GUARDS, CHECKS]) must(await api.POST("/link-types", { body: type }));
    must(await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey: `${KEY}-R1` } }, body: { typeKey: GUARDS.key, otherKey: `${KEY}-R2` } }));

    await page.goto("/admin/link-types");
    const guards = page.locator(`[data-link-type="${GUARDS.key}"]`);
    const checks = page.locator(`[data-link-type="${CHECKS.key}"]`);
    await expect(guards.locator('[data-action="delete-link-type"]')).toHaveCount(0);
    await expect(checks.locator('[data-action="delete-link-type"]')).toBeVisible();

    await guards.locator('[data-action="edit-link-type"]').click();
    await guards.getByRole("textbox", { name: "Reads from the source" }).fill(SHIELDS);
    await expectAccessible(page);
    await guards.locator('[data-action="save-link-type"]').click();
    await expect(guards).toContainText(`${SHIELDS} / is guarded by`);
    await guards.locator('[data-action="edit-link-type"]').click();
    await guards.getByRole("textbox", { name: "Name" }).fill("Never saved");
    await guards.locator('[data-action="cancel-link-type"]').click();
    await expect(guards).toContainText(GUARDS.name);
    await expect(guards).not.toContainText("Never saved");

    await page.goto(`/projects/${KEY}/r/${KEY}-R3`);
    await expect(linkTypeOptions(page).filter({ hasText: `This requirement ${SHIELDS}` })).toHaveCount(1);
    await expect(linkTypeOptions(page).filter({ hasText: `This requirement ${GUARDS.forward}` })).toHaveCount(0);

    await page.goto("/admin/link-types");
    await guards.locator('[data-action="retire-link-type"]').click();
    await expect(guards).toHaveAttribute("data-retired", "");
    await expect(guards.locator('[data-action="restore-link-type"]')).toHaveText("Use again");
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(page.locator(`[data-link-group="${GUARDS.key}:outgoing"] [data-link="${KEY}-R2"]`)).toContainText("Brakes hold");
    // The options load apart from the links, so a type still in use shows first that they are there.
    await expect(linkTypeOptions(page).filter({ hasText: `This requirement ${CHECKS.forward}` })).toHaveCount(1);
    await expect(linkTypeOptions(page).filter({ hasText: SHIELDS })).toHaveCount(0);

    await page.goto("/admin/link-types");
    await guards.locator('[data-action="restore-link-type"]').click();
    await expect(guards).not.toHaveAttribute("data-retired", "");
    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(linkTypeOptions(page).filter({ hasText: `This requirement ${SHIELDS}` })).toHaveCount(1);

    await page.goto("/admin/link-types");
    await checks.locator('[data-action="delete-link-type"]').click();
    await expect(checks).toHaveCount(0);
    await expect(guards.locator('[data-action="delete-link-type"]')).toHaveCount(0);
  });

  test("a project makes a type of its own and limits one to a pair of requirement types", async ({ page, api }) => {
    await makeProject(api, KEY, ["Brakes engage", "Brakes hold"]);
    await makeProject(api, OTHER, ["Lights dim"]);
    for (const name of ["Feature", "Test"]) {
      must(await api.POST("/projects/{projectKey}/types", { params: { path: { projectKey: KEY } }, body: { name, icon: "requirement", colour: "#4f6bed" } }));
    }

    await page.goto(`/projects/${KEY}/settings/links`);
    await page.locator('[data-action="new-link-type"]').click();
    await page.getByRole("textbox", { name: "Name" }).fill("Tests");
    await page.getByRole("textbox", { name: "Reads from the source" }).fill("tests");
    await page.getByRole("textbox", { name: "Reads from the target" }).fill("is tested by");
    await page.locator('[data-action="save-link-type"]').click();
    const tests = page.locator('[data-link-type="tests"]');
    await expect(tests).toContainText("This project");
    await expect(tests).toContainText("tests / is tested by");

    const derives = page.locator('[data-link-type="derives"]');
    await derives.locator('[data-action="edit-pairs"]').click();
    const dialog = page.getByRole("dialog", { name: "Requirement types Derives joins" });
    await dialog.locator('[data-action="add-pair"]').click();
    await dialog.getByRole("combobox", { name: "From 1" }).selectOption({ label: "Test" });
    await dialog.getByRole("combobox", { name: "To 1" }).selectOption({ label: "Feature" });
    await expectAccessible(page);
    await dialog.locator('[data-action="save-pairs"]').click();
    await expect(dialog).toBeHidden();
    await expect(derives.locator("[data-link-type-pairs]")).toHaveText("1 pair of types");

    await page.goto(`/projects/${KEY}/r/${KEY}-R1`);
    await expect(linkTypeOptions(page).filter({ hasText: "This requirement tests" })).toHaveCount(1);
    const form = page.getByRole("form", { name: "Add a link" });
    await form.getByRole("combobox", { name: "Link type" }).selectOption({ label: "This requirement derives from" });
    await form.getByRole("combobox", { name: "Requirement" }).fill(`${KEY}-R2`);
    await page.locator(`[data-picker-option="${KEY}-R2"]`).click();
    await form.locator('[data-action="create-link"]').click();
    await expect(form.getByRole("alert")).toContainText(`In ${KEY}, Derives links do not join a Requirement to a Requirement.`);
    await expect(page.locator(`[data-link="${KEY}-R2"]`)).toHaveCount(0);

    await page.goto(`/projects/${OTHER}/r/${OTHER}-R1`);
    await expect(linkTypeOptions(page).filter({ hasText: "This requirement derives from" })).toHaveCount(1);
    await expect(linkTypeOptions(page).filter({ hasText: "This requirement tests" })).toHaveCount(0);
  });
});

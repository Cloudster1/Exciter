import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { findGrants, grantInForm, grantRow } from "../fixtures/grants";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey, uniqueName } from "../fixtures/seed";
import { confirm, dismissToasts, expectAccessible, menuItem, toast } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const KEY = uniqueKey("ACC");
const ACCESS = "/admin/access";

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** Deletes the groups and roles a walk made under these names, whichever step it stopped at. */
async function forget(api: ExciterApi, groups: string[], roles: string[]) {
  const { groups: all } = must(await api.GET("/groups", { params: { query: { limit: 200 } } }));
  for (const group of all.filter((each) => groups.includes(each.name))) {
    await api.DELETE("/groups/{groupID}", { params: { path: { groupID: group.id } } });
  }
  for (const role of must(await api.GET("/roles")).roles.filter((each) => !each.builtin && roles.includes(each.name))) {
    await api.DELETE("/roles/{roleKey}", { params: { path: { roleKey: role.role } } });
  }
}

// The project, the group and the role are this worker's own, so the walk meets no other spec.
test.describe("the Access page", { tag: "@desktop" }, () => {
  test("an administrator makes a group, grants roles in a project, and adds, ticks, renames and deletes a role", async ({ page, api, people }, testInfo) => {
    const group = uniqueName(testInfo, "Checkers");
    const renamedGroup = uniqueName(testInfo, "Inspectors");
    const role = uniqueName(testInfo, "Auditor");
    const renamedRole = uniqueName(testInfo, "Assessor");
    const bob = people.bob.name;
    must(await api.POST("/projects", { body: { key: KEY, name: "Accessed" } }));
    try {
      await page.goto(ACCESS);
      await expect(page).toHaveURL(`${WEB_URL}${ACCESS}/roles`);
      await page.goto("/settings/sso");
      await expect(page).toHaveURL(`${WEB_URL}${ACCESS}/sso`);
      await expect(page.getByRole("tab", { name: "Single sign-on" })).toHaveAttribute("aria-selected", "true");

      await page.getByRole("tab", { name: "Groups" }).click();
      await expect(page).toHaveURL(`${WEB_URL}${ACCESS}/groups`);
      await expect(page.locator('[data-group-builtin="everyone"]')).toBeVisible();
      await page.getByRole("textbox", { name: "New group" }).fill(group);
      await page.locator('[data-action="create-group"]').click();
      await expect(toast(page, `${group} was made.`)).toBeVisible();
      await dismissToasts(page);
      const card = page.locator(`[data-group="${group}"]`);
      await card.locator("[data-subject-picker] input").fill(bob);
      await card.locator(`[data-subject-option="${bob}"]`).click();
      await expect(toast(page, `${bob} is in ${group} now.`)).toBeVisible();
      await dismissToasts(page);
      await expect(card.locator(`[data-group-member="${bob}"]`)).toBeVisible();
      await card.locator('[data-action="edit-group"]').click();
      const editing = page.locator("[data-edit-group]");
      await editing.getByLabel("Name", { exact: true }).fill(renamedGroup);
      await editing.locator('[data-action="save-group"]').click();
      await expect(toast(page, `${renamedGroup} was saved.`)).toBeVisible();
      await dismissToasts(page);
      const renamedCard = page.locator(`[data-group="${renamedGroup}"]`);
      await expect(renamedCard.locator(`[data-group-member="${bob}"]`)).toBeVisible();
      await expectAccessible(page);

      await page.getByRole("tab", { name: "Roles" }).click();
      await expect(page).toHaveURL(`${WEB_URL}${ACCESS}/roles`);
      const form = page.locator("[data-grant-form]");
      await form.locator("[data-subject-picker] input").fill(people.alice.name);
      await form.locator(`[data-subject-option="${people.alice.name}"]`).click();
      await form.locator('[data-action="change-subject"]').click();
      await expect(form.locator("[data-grant-to]")).toHaveCount(0);
      await grantInForm(page, bob, "reviewer", KEY);
      await expect(toast(page, `${bob} holds Reviewer now.`)).toBeVisible();
      await dismissToasts(page);
      await grantInForm(page, renamedGroup, "editor", KEY);
      await expect(toast(page, `${renamedGroup} holds Editor now.`)).toBeVisible();
      await dismissToasts(page);
      await findGrants(page, KEY);
      await expect(grantRow(page, bob, "reviewer")).toContainText(`Accessed (${KEY})`);
      await expect(grantRow(page, renamedGroup, "editor")).toContainText(`Accessed (${KEY})`);
      await expectAccessible(page);

      await page.goto(`/p/${KEY}/permissions`);
      await expect(grantRow(page, bob, "reviewer")).toBeVisible();
      await expect(grantRow(page, renamedGroup, "editor")).toBeVisible();

      await page.goto(`${ACCESS}/matrix`);
      await expect(page.locator('[data-permission-cell="administrator:administerInstallation"]')).toBeDisabled();
      await page.locator('[data-action="new-role"]').click();
      const adding = page.locator("[data-new-role]");
      await adding.getByLabel("Name", { exact: true }).fill(role);
      await adding.locator('[data-action="add-role"]').click();
      await expect(toast(page, `${role} was added.`)).toBeVisible();
      await dismissToasts(page);
      const key = must(await api.GET("/roles")).roles.find((each) => each.name === role)?.role;
      expect(key, "the new role is listed").toBeDefined();
      const column = page.locator(`[data-role-column="${key}"]`);
      await expect(column).toContainText(role);
      const comment = page.locator(`[data-permission-cell="${key}:comment"]`);
      await expect(page.locator(`[data-permission-cell="${key}:view"]`)).toBeChecked();
      await expect(comment).not.toBeChecked();
      await comment.check();
      await expect(toast(page, `${role} may now comment on requirements.`)).toBeVisible();
      await dismissToasts(page);
      await page.reload();
      await expect(comment).toBeChecked();
      await expectAccessible(page);

      await column.locator('[data-action="role-menu"]').click();
      await menuItem(page, "rename-role").click();
      const renaming = page.locator(`[data-rename-role="${key}"]`);
      await renaming.getByLabel("Name", { exact: true }).fill(renamedRole);
      await renaming.locator('[data-action="save-role"]').click();
      await expect(toast(page, `${renamedRole} was saved.`)).toBeVisible();
      await dismissToasts(page);
      await expect(column).toContainText(renamedRole);

      await page.goto(`/p/${KEY}/permissions`);
      await expect(page.locator(`[data-project-role="${key}"]`)).toContainText(renamedRole);
      await expect(page.locator(`[data-project-role="${key}"]`)).toContainText("Comment on requirements");

      await page.goto(`${ACCESS}/matrix`);
      await column.locator('[data-action="role-menu"]').click();
      await menuItem(page, "delete-role").click();
      await confirm(page);
      await expect(toast(page, `${renamedRole} was deleted.`)).toBeVisible();
      await dismissToasts(page);
      await expect(column).toHaveCount(0);

      await page.getByRole("tab", { name: "Groups" }).click();
      await renamedCard.locator('[data-action="remove-group-member"]').click();
      await confirm(page);
      await expect(toast(page, `${bob} left ${renamedGroup}.`)).toBeVisible();
      await dismissToasts(page);
      await expect(renamedCard.locator(`[data-group-member="${bob}"]`)).toHaveCount(0);
      await renamedCard.locator('[data-action="delete-group"]').click();
      await confirm(page);
      await expect(toast(page, `${renamedGroup} was deleted.`)).toBeVisible();
      await expect(renamedCard).toHaveCount(0);
    } finally {
      await forget(api, [group, renamedGroup], [role, renamedRole]);
      await removeProject(api);
    }
  });
});

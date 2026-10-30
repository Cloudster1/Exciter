import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm } from "../fixtures/shell";

// Comments and mentions (#33): alice asks on a requirement and names bob, bob answers and resolves the thread.

const KEY = uniqueKey("TALK");
const REQUIREMENT = `/p/${KEY}/r/${KEY}-R1`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("comments", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice asks bob on a requirement, bob answers and resolves, and alice withdraws her question", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Talked over" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(REQUIREMENT);
    const discussion = page.getByRole("region", { name: "Discussion" });
    await expect(discussion.getByText("Nobody has said anything about this requirement yet.")).toBeVisible();
    await discussion.getByRole("button", { name: "Comment" }).click();
    const box = page.locator("#new-comment");
    await box.click();
    await box.pressSequentially("Why 100 ms, ");
    // Bob's address is his alone; his first name is every spec's bob's.
    await box.pressSequentially(`@${people.bob.email.split("@")[0]}`);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await discussion.getByRole("button", { name: "Post comment" }).click();
    const thread = discussion.getByRole("article");
    await expect(thread).toContainText(`@${people.bob.name}`);
    await expect(thread).toContainText("on revision 1");
    await expectAccessible(page);

    const bob = await pageAs("bob");
    const bobs = bob.getByRole("region", { name: "Discussion" });
    await openUntil(bob, REQUIREMENT, () => expect(bobs.getByRole("article")).toBeVisible(ONE_LOOK));
    await bobs.getByRole("button", { name: "Reply" }).click();
    await bob.locator("[id^=reply-]").click();
    await bob.locator("[id^=reply-]").pressSequentially("Because of the new sensor.");
    await bobs.getByRole("button", { name: "Post reply" }).click();
    await expect(bobs.getByRole("article")).toContainText("Because of the new sensor.");
    await bobs.getByRole("button", { name: "Resolve" }).click();
    await expect(bobs.getByText(new RegExp(`Resolved by ${people.bob.name}`))).toBeVisible();

    await openUntil(page, REQUIREMENT, () => expect(discussion.getByText(new RegExp(`Resolved by ${people.bob.name}`))).toBeVisible(ONE_LOOK));
    await discussion.getByRole("button", { name: "Delete" }).first().click();
    await confirm(page);
    await expect(discussion.getByText("This comment was deleted.")).toBeVisible();
    await expect(discussion.getByRole("article")).toContainText("Because of the new sensor.");
  });
});

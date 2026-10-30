import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

// The edges of the discussion (#312): a comment cancelled, edited to mention bob, resolved and reopened; another's
// comment moderated; and a reader, and a member without a grant, kept out of it.

const TALK = uniqueKey("EDGE");
const RULE = uniqueKey("MODR");

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function projectWithOne(api: ExciterApi, key: string) {
  must(await api.POST("/projects", { body: { key, name: `Discussed ${key}` } }));
  must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: key } }, body: { title: "Brakes engage within 100 ms" } }));
}

test.describe("the discussion's edges", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, TALK);
    await removeProject(api, RULE);
  });

  test("alice cancels, then edits her comment to mention bob, who hears of it, resolves and reopens", async ({ page, api, pageAs, people }) => {
    await projectWithOne(api, TALK);
    const requirement = `/p/${TALK}/r/${TALK}-R1`;
    await page.goto(requirement);
    const discussion = page.getByRole("region", { name: "Discussion" });
    await discussion.getByRole("button", { name: "Comment" }).click();
    await page.locator("#new-comment").click();
    await page.locator("#new-comment").pressSequentially("Not yet");
    await discussion.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator("#new-comment")).toHaveCount(0);
    await expect(discussion.getByText("Nobody has said anything about this requirement yet.")).toBeVisible();
    await expect(discussion.getByRole("article")).toHaveCount(0);

    await discussion.getByRole("button", { name: "Comment" }).click();
    await page.locator("#new-comment").click();
    await page.locator("#new-comment").pressSequentially("Why 100 ms?");
    await discussion.getByRole("button", { name: "Post comment" }).click();
    const thread = discussion.getByRole("article");
    await expect(thread).toContainText("Why 100 ms?");
    await thread.getByRole("button", { name: "Edit" }).click();
    const box = page.locator("[id^=edit-]");
    await box.click();
    await box.press("End");
    await box.pressSequentially(" Ask ");
    await box.pressSequentially(`@${people.bob.email.split("@")[0]}`);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await thread.getByRole("button", { name: "Save" }).click();
    await expect(thread).toContainText(`@${people.bob.name}`);
    await expect(thread.getByText("edited")).toBeVisible();
    await expectAccessible(page);

    const bob = await pageAs("bob");
    const bell = bob.getByRole("button", { name: /^Notifications, \d+ unread$/ });
    await openUntil(bob, "/projects", () => expect(bell).toBeVisible(ONE_LOOK));
    await bell.click();
    await expect(
      bob
        .getByRole("dialog", { name: "Notifications" })
        .getByRole("link", { name: new RegExp(`${people.alice.name} mentioned you in a comment on ${TALK}-R1`) }),
    ).toContainText("Why 100 ms? Ask");

    const bobs = bob.getByRole("region", { name: "Discussion" });
    await openUntil(bob, requirement, () => expect(bobs.getByRole("article")).toContainText("edited", ONE_LOOK));
    await bobs.getByRole("button", { name: "Resolve" }).click();
    await expect(bobs.getByText(`Resolved by ${people.bob.name}`)).toBeVisible();
    await bobs.getByRole("button", { name: "Reopen" }).click();
    await expect(bobs.getByText(`Resolved by ${people.bob.name}`)).toHaveCount(0);
    await expect(bobs.getByRole("button", { name: "Resolve" })).toBeVisible();
    await openUntil(page, requirement, () => expect(discussion.getByRole("button", { name: "Resolve" })).toBeVisible(ONE_LOOK));
    await expect(discussion.getByRole("article")).not.toHaveAttribute("data-resolved");
  });

  test("alice deletes bob's comment, and a reader and a member without a grant stay out", async ({ page, api, apiAs, pageAs, people }) => {
    await projectWithOne(api, RULE);
    const reqKey = `${RULE}-R1`;
    const requirement = `/p/${RULE}/r/${reqKey}`;
    const reqPath = { params: { path: { reqKey } } };
    const bobsThread = must(
      await (await apiAs("bob")).POST("/requirements/{reqKey}/comments", { ...reqPath, body: { bodyMarkdown: "This is off topic." } }),
    ).thread;
    must(
      await api.POST("/comments/{commentID}/replies", { params: { path: { commentID: bobsThread.id } }, body: { bodyMarkdown: "Let us keep to the brakes." } }),
    );

    const discussion = page.getByRole("region", { name: "Discussion" });
    await openUntil(page, requirement, () => expect(discussion.getByRole("article")).toContainText("This is off topic.", ONE_LOOK));
    await discussion.locator(`[data-comment="${bobsThread.id}"]`).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("[data-confirm]")).toContainText(`Delete ${people.bob.name}'s comment?`);
    await confirm(page);
    await expect(discussion.getByText("This comment was deleted.")).toBeVisible();
    const bob = await pageAs("bob");
    const bobs = bob.getByRole("region", { name: "Discussion" });
    await openUntil(bob, requirement, () => expect(bobs.getByText("This comment was deleted.")).toBeVisible(ONE_LOOK));
    await expect(bobs.getByRole("article")).not.toContainText("This is off topic.");
    await expect(bobs.getByRole("article")).toContainText("Let us keep to the brakes.");

    const me = must(await api.GET("/auth/me")).user;
    const grants = (bobRole?: "viewer") =>
      setProjectGrants(api, RULE, [
        { subject: { type: "user", id: me.id }, role: "admin" },
        ...(bobRole ? [{ subject: { type: "user" as const, id: people.bob.id }, role: bobRole }] : []),
      ]);
    await grants();
    await page.goto(requirement);
    await discussion.getByRole("button", { name: "Comment" }).click();
    await page.locator("#new-comment").click();
    const prefix = people.bob.email.split("@")[0]!;
    const picked = page.waitForResponse((answer) => answer.url().includes("/mentionable") && answer.url().includes(`q=${encodeURIComponent(prefix)}`));
    await page.locator("#new-comment").pressSequentially(`@${prefix}`);
    const offered = (await (await picked).json()) as { people: { id: string; canView: boolean }[] };
    expect(offered.people.find((each) => each.id === people.bob.id)?.canView).toBe(false);
    await expect(page.getByRole("option", { name: new RegExp(people.bob.name) })).toHaveCount(0);
    await page.keyboard.press("Escape");
    await discussion.getByRole("button", { name: "Cancel" }).click();

    await grants("viewer");
    await openUntil(bob, requirement, () => expect(bobs.getByText(/You may read this discussion but not take part/)).toBeVisible(ONE_LOOK));
    await expect(bobs.getByRole("article")).toContainText("Let us keep to the brakes.");
    await expect(bobs.getByRole("button", { name: "Comment" })).toHaveCount(0);
    await expect(bobs.getByRole("button", { name: "Reply" })).toHaveCount(0);
    await expectAccessible(bob);
  });
});

import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { mailsTo, mailText } from "../fixtures/mail";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Watching and notifications (#34): bob watches a requirement, alice mentions him and revises it, and the stack's
// worker tells him in the app and by mail.

const KEY = uniqueKey("HEAR");
const REQ_KEY = `${KEY}-R1`;
const REQUIREMENT = `/projects/${KEY}/r/${REQ_KEY}`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("notifications", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("bob watches a requirement and hears of alice's mention and revision, in the bell and by mail", async ({ api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Heard about" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));

    const bob = await pageAs("bob");
    await openUntil(bob, REQUIREMENT, () => expect(bob.getByRole("button", { name: "Watch", exact: true })).toBeVisible(ONE_LOOK));
    await bob.getByRole("button", { name: "Watch", exact: true }).click();
    await bob.getByRole("menuitem", { name: "Watch this requirement" }).click();
    await expect(bob.getByRole("button", { name: "Watching", exact: true })).toBeVisible();

    const reqPath = { params: { path: { reqKey: REQ_KEY } } };
    const thread = must(
      await api.POST("/requirements/{reqKey}/comments", {
        ...reqPath,
        body: {
          body: {
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [
                  { type: "text", text: "Can you check this, " },
                  { type: "mention", attrs: { id: people.bob.id, label: people.bob.name } },
                ],
              },
            ],
          },
        },
      }),
    ).thread;
    must(await api.PATCH("/requirements/{reqKey}", { ...reqPath, body: { title: "Brakes engage within 80 ms", baseRevision: 1, comment: "Tighter." } }));

    const bell = bob.getByRole("button", { name: "Notifications, 2 unread" });
    await openUntil(bob, "/projects", () => expect(bell).toBeVisible(ONE_LOOK));
    await bell.click();
    const panel = bob.getByRole("dialog", { name: "Notifications" });
    await expect(panel.getByRole("link", { name: new RegExp(`${people.alice.name} saved a new revision of ${REQ_KEY}`) })).toContainText("Tighter.");
    const mention = panel.getByRole("link", { name: new RegExp(`${people.alice.name} mentioned you in a comment on ${REQ_KEY}`) });
    await expect(mention).toContainText("Can you check this");
    await expectAccessible(bob);

    await mention.click();
    await expect(bob).toHaveURL(new RegExp(`${REQUIREMENT}#thread-${thread.id}$`));
    await expect(bob.locator(`[data-thread="${thread.id}"]`)).toBeFocused();
    await expect(bob.getByRole("button", { name: "Notifications, 1 unread" })).toBeVisible();

    await expect.poll(async () => (await mailsTo(people.bob.email, `mentioned you in a comment on ${REQ_KEY}`)).length).toBeGreaterThan(0);
    const [mailed] = await mailsTo(people.bob.email, `mentioned you in a comment on ${REQ_KEY}`);
    expect(await mailText(mailed!.ID)).toContain(`/r/${thread.requirementId}#thread-${thread.id}`);

    await bob.getByRole("button", { name: "Notifications, 1 unread" }).click();
    await bob.getByRole("dialog", { name: "Notifications" }).getByRole("link", { name: "Notification settings" }).click();
    const revised = bob.getByRole("checkbox", { name: "In the app: A requirement you watch gets a new revision" });
    await expect(revised).toBeChecked();
    await revised.click();
    await bob.getByRole("button", { name: "Save" }).click();
    await expect(bob.getByText("Your notification settings are saved.")).toBeVisible();
    await expectAccessible(bob);
  });
});

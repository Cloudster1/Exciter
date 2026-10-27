import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { mailsTo } from "../fixtures/mail";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// The notifications page and the settings (#312): bob walks more than a page of what he was told, narrows it to the
// unread and reads it all; and a mail switched off is not sent while the bell still says it.

const PAGED = uniqueKey("NPGS");
const QUIET = uniqueKey("NSET");
// Two past a page of the notifications page: one is read beforehand, and the unread still spill onto a second page.
const REVISIONS = 52;

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

async function watchedBy(api: ExciterApi, bob: ExciterApi, key: string) {
  must(await api.POST("/projects", { body: { key, name: `Told ${key}` } }));
  must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: key } }, body: { title: "Brakes engage within 100 ms" } }));
  must(await bob.PUT("/requirements/{reqKey}/watch", { params: { path: { reqKey: `${key}-R1` } } }));
}

test.describe("the notifications page and settings", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await removeProject(api, PAGED);
    await removeProject(api, QUIET);
  });

  test("bob opens everything from the bell, walks the pages, narrows to the unread and reads it all", async ({ api, apiAs, pageAs }) => {
    const bobApi = await apiAs("bob");
    await watchedBy(api, bobApi, PAGED);
    const reqPath = { params: { path: { reqKey: `${PAGED}-R1` } } };
    for (let base = 1; base <= REVISIONS; base++) {
      must(await api.PATCH("/requirements/{reqKey}", { ...reqPath, body: { title: `Brakes engage within ${100 - base} ms`, baseRevision: base } }));
    }
    const listed = async () => must(await bobApi.GET("/notifications", { params: { query: { limit: 1 } } }));
    await expect.poll(async () => (await listed()).total).toBe(REVISIONS);
    const total = (await listed()).total;
    const read = await bobApi.POST("/notifications/read", { body: { ids: [(await listed()).notifications[0]!.id] } });
    expect(read.response.status).toBe(204);

    const bob = await pageAs("bob");
    await bob.goto("/projects");
    await bob.getByRole("button", { name: /^Notifications, \d+ unread$/ }).click();
    await bob.getByRole("dialog", { name: "Notifications" }).getByRole("link", { name: "See all notifications" }).click();
    await expect(bob).toHaveURL(/\/notifications$/);
    await expect(bob.getByText(`1 to 50 of ${total}`)).toBeVisible();
    await expectAccessible(bob);
    await bob.getByRole("button", { name: "Next" }).click();
    await expect(bob.getByText(`51 to ${total} of ${total}`)).toBeVisible();
    await expect(bob.getByRole("button", { name: "Next" })).toBeDisabled();
    await bob.getByRole("button", { name: "Previous" }).click();
    await expect(bob.getByText(`1 to 50 of ${total}`)).toBeVisible();

    await bob.getByRole("checkbox", { name: "Unread only" }).check();
    await expect(bob.getByText(`1 to 50 of ${total - 1}`)).toBeVisible();
    await bob.getByRole("button", { name: "Mark all as read" }).click();
    await expect(bob.getByText("You have read everything.")).toBeVisible();
    await expect(bob.getByRole("button", { name: "Notifications", exact: true })).toBeVisible();
  });

  test("bob switches mention mails off: the bell says it, Mailpit gets nothing, and a daily digest is kept", async ({ api, apiAs, pageAs, people }) => {
    const bobApi = await apiAs("bob");
    await watchedBy(api, bobApi, QUIET);
    const reqKey = `${QUIET}-R1`;
    const bob = await pageAs("bob");
    await bob.goto("/settings/notifications");
    const mentions = bob.getByRole("checkbox", { name: "By email: Somebody mentions you in a comment" });
    await mentions.uncheck();
    await bob.getByRole("button", { name: "Save" }).click();
    await expect(bob.getByText("Your notification settings are saved.")).toBeVisible();

    const reqPath = { params: { path: { reqKey } } };
    must(
      await api.POST("/requirements/{reqKey}/comments", {
        ...reqPath,
        body: {
          body: {
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [
                  { type: "text", text: "Quiet, " },
                  { type: "mention", attrs: { id: people.bob.id, label: people.bob.name } },
                ],
              },
            ],
          },
        },
      }),
    );
    const bell = bob.getByRole("button", { name: /^Notifications, \d+ unread$/ });
    await openUntil(bob, "/projects", () => expect(bell).toBeVisible(ONE_LOOK));
    await bell.click();
    await expect(
      bob
        .getByRole("dialog", { name: "Notifications" })
        .getByRole("link", { name: new RegExp(`${people.alice.name} mentioned you in a comment on ${reqKey}`) }),
    ).toBeVisible();
    // A revision bob still takes by mail goes after the mention, so once its mail is in the mention's would be too.
    must(await api.PATCH("/requirements/{reqKey}", { ...reqPath, body: { title: "Brakes engage within 90 ms", baseRevision: 1 } }));
    await expect.poll(async () => (await mailsTo(people.bob.email, `saved a new revision of ${reqKey}`)).length).toBeGreaterThan(0);
    expect(await mailsTo(people.bob.email, `mentioned you in a comment on ${reqKey}`)).toEqual([]);

    await bob.goto("/settings/notifications");
    await expect(mentions).not.toBeChecked();
    await bob.getByLabel("When emails go out").selectOption("daily");
    await bob.getByRole("button", { name: "Save" }).click();
    await expect(bob.getByText("Your notification settings are saved.")).toBeVisible();
    await bob.reload();
    await expect(bob.getByLabel("When emails go out")).toHaveValue("daily");
    await expect(mentions).not.toBeChecked();
    // The spec's people are the file's, so bob's mail goes out at once again for the tests after.
    await bob.getByLabel("When emails go out").selectOption("off");
    await mentions.check();
    await bob.getByRole("button", { name: "Save" }).click();
    await expect(bob.getByText("Your notification settings are saved.")).toBeVisible();
  });
});

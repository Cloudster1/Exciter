import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

const KEY = uniqueKey("DOC");

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

// The project's key is this worker's own, so it meets no other spec.
test.describe("the document", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("writes a requirement where it is read, adds one after it, and saves over a revision made meanwhile", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    const pay = await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
    await write({ title: "Ship", place: { parentId: scope.id, afterId: pay.id } });

    await page.goto(`/p/${KEY}/spec`);
    const entry = (key: string) => page.locator(`[data-spec-entry="${key}"]`);
    await expect(entry(pay.key).getByRole("heading")).toHaveText("1.1 Pay");

    // From the keyboard: down to Pay, edit, save, and back on the entry.
    await entry(scope.key).focus();
    await page.keyboard.press("ArrowDown");
    await expect(entry(pay.key)).toBeFocused();
    await page.keyboard.press("Enter");
    const title = entry(pay.key).getByLabel("Title", { exact: true });
    await expect(title).toBeFocused();
    await title.fill("Pay by card");
    await expectAccessible(page);
    await page.keyboard.press("ControlOrMeta+s");
    await expect(entry(pay.key).getByRole("heading")).toHaveText("1.1 Pay by card");
    await expect(page.locator("[data-spec-announced]")).toHaveText(`${pay.key}: Saved as revision 2.`);
    await expect(entry(pay.key)).toBeFocused();

    // Added after Pay, it stands between Pay and Ship.
    await entry(pay.key).locator('[data-action="add-entry"]').click();
    await page.locator('[data-action="add-after"]').click();
    const adding = page.locator("[data-spec-adding]");
    await adding.getByLabel("Title", { exact: true }).fill("Refund");
    await adding.locator('[data-action="create-requirement"]').click();
    await expect(page.getByRole("heading", { name: "1.2 Refund" })).toBeVisible();
    await expect(entry(`${KEY}-R3`).getByRole("heading")).toHaveText("1.3 Ship");

    // Somebody else saves Ship in a second tab while this one edits it.
    await entry(`${KEY}-R3`).locator('[data-action="edit-entry"]').click();
    await entry(`${KEY}-R3`).getByLabel("Title", { exact: true }).fill("Ship in a day");
    const other = await page.context().newPage();
    await other.goto(`/p/${KEY}/spec`);
    await other.locator(`[data-spec-entry="${KEY}-R3"] [data-action="edit-entry"]`).click();
    await other.locator(`[data-spec-entry="${KEY}-R3"]`).getByLabel("Title", { exact: true }).fill("Ship in two days");
    await other.locator(`[data-spec-entry="${KEY}-R3"] [data-action="save-requirement"]`).click();
    await expect(other.locator(`[data-spec-entry="${KEY}-R3"]`).getByRole("heading")).toHaveText("1.3 Ship in two days");
    await other.close();

    await entry(`${KEY}-R3`).locator('[data-action="save-requirement"]').click();
    await expect(entry(`${KEY}-R3`).locator("[data-newer-revision]")).toContainText("Ship in two days");
    await expect(entry(`${KEY}-R3`).getByLabel("Title", { exact: true })).toHaveValue("Ship in a day");
    await entry(`${KEY}-R3`).locator('[data-action="save-over-latest"]').click();
    await expect(entry(`${KEY}-R3`).getByRole("heading")).toHaveText("1.3 Ship in a day");
  });

  test("asks before an unsaved draft is left behind", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    for (const title of ["Pay", "Ship"]) {
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title } }));
    }
    await page.goto(`/p/${KEY}/spec`);
    await page.locator(`[data-spec-entry="${KEY}-R1"] [data-action="edit-entry"]`).click();
    await page.locator(`[data-spec-entry="${KEY}-R1"]`).getByLabel("Title", { exact: true }).fill("Pay later");
    await page.getByRole("link", { name: `${KEY}-R2` }).click();
    const dialog = page.locator("[data-discard-dialog]");
    await expect(dialog).toBeVisible();
    await expectAccessible(page);
    await dialog.locator('[data-action="keep-editing"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/spec`);
    await expect(page.locator(`[data-spec-entry="${KEY}-R1"]`).getByLabel("Title", { exact: true })).toHaveValue("Pay later");

    await page.getByRole("link", { name: `${KEY}-R2` }).click();
    await dialog.locator('[data-action="discard-changes"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${KEY}-R2`);
  });

  test("submits a section for review at once, and a reviewer approves from the document", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    must(
      await api.PUT("/projects/{projectKey}/grants", {
        params: { path: { projectKey: KEY } },
        body: {
          grants: [
            { subject: { type: "user", id: people.alice.id }, role: "admin" },
            { subject: { type: "everyone" }, role: "editor" },
            { subject: { type: "user", id: people.bob.id }, role: "reviewer" },
          ],
        },
      }),
    );
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    const pay = await write({ title: "Pay", place: { parentId: scope.id, afterId: null } });
    const ship = await write({ title: "Ship", place: { parentId: scope.id, afterId: pay.id } });
    const other = await write({ title: "Other" });

    await page.goto(`/p/${KEY}/spec`);
    await page.locator(`[data-pick-below="${scope.key}"]`).check();
    const bar = page.locator("[data-selection-bar]");
    await expect(bar.getByRole("status")).toHaveText("2 requirements selected");
    await bar.locator('[data-bulk-transition="Submit for review"]').click();
    await expect(page.locator("[data-spec-announced]")).toHaveText("2 requirements moved to In review.");
    for (const key of [pay.key, ship.key]) {
      await expect(page.locator(`[data-spec-entry="${key}"] [data-status="In review"]`)).toBeVisible();
    }
    await expect(page.locator(`[data-spec-entry="${other.key}"] [data-status="Draft"]`)).toBeVisible();

    await page.locator('[data-filter-category="review"]').click();
    await expect(page.locator(`[data-spec-entry="${other.key}"]`)).toHaveCount(0);
    await expect(page.locator(`[data-spec-entry="${scope.key}"]`)).toBeVisible();
    await expectAccessible(page);

    // Alice moved them, so bob's read may come from a replica a moment behind.
    const bob = await pageAs("bob");
    const payStatus = bob.locator(`[data-spec-entry="${pay.key}"] [data-action="status-menu"]`);
    await openUntil(bob, `/p/${KEY}/spec`, () => expect(payStatus.locator('[data-status="In review"]')).toBeVisible(ONE_LOOK));
    await payStatus.click();
    await bob.getByRole("menuitem", { name: "Approve: Approved" }).click();
    await expect(bob.locator(`[data-spec-entry="${pay.key}"] [data-status="Approved"]`)).toBeVisible();
    await expect(bob.locator("[data-spec-announced]")).toHaveText(`${pay.key} moved to Approved.`);
  });

  test("marks what two revised requirements changed, against their previous revisions and against a baseline", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    const pay = await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
    const ship = await write({ title: "Ship", statement: paragraph("The basket ships."), place: { parentId: scope.id, afterId: pay.id } });
    must(await api.POST("/projects/{projectKey}/baselines", { params: { path: { projectKey: KEY } }, body: { name: "Before" } }));
    const revise = async (reqKey: string, title: string, text: string) =>
      must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey } }, body: { title, statement: paragraph(text), baseRevision: 1 } }));
    await revise(pay.key, "Pay by card", "The shopper pays by card.");
    await revise(ship.key, "Ship in a day", "The basket ships in a day.");

    await page.goto(`/p/${KEY}/spec`);
    const entry = (key: string) => page.locator(`[data-spec-entry="${key}"]`);
    await expect(entry(pay.key).getByRole("heading")).toHaveText("1.1 Pay by card");
    await page.locator('[data-action="show-changes"]').click();
    await expect(entry(pay.key).locator('[data-spec-change="changed"]')).toBeVisible();
    await expect(entry(pay.key).locator("[data-diff-title] ins")).toHaveText(/by card/);
    // The words are marked one by one: "at" and "once" go, "by" and "card" come.
    const payDiff = entry(pay.key).locator('[data-diff-of="statement"]');
    await expect(payDiff.locator("del")).toHaveText([/at/, /once/]);
    await expect(payDiff.locator("ins")).toHaveText([/by/, /card/]);
    await expect(entry(ship.key).locator('[data-diff-of="statement"] ins').first()).toBeVisible();
    await expect(page.locator("[data-changes-count]")).toHaveText("2 requirements differ");
    await expectAccessible(page);

    await page.locator("[data-changes-base]").selectOption("baseline");
    await page.locator("[data-changes-baseline]").selectOption({ label: "Before" });
    await expect(page).toHaveURL(/changes=baseline/);
    await expect(entry(pay.key).locator("[data-full-comparison]")).toHaveText("Compare revision 1 with 2");
    await expect(entry(ship.key).locator('[data-spec-change="changed"]')).toBeVisible();

    await entry(scope.key).focus();
    await page.locator('[data-action="next-change"]').click();
    await expect(entry(pay.key)).toBeFocused();
    await entry(pay.key).locator("[data-full-comparison]").click();
    await expect(page).toHaveURL(`/p/${KEY}/r/${pay.key}/history?from=1&to=2`);
    await expect(page.locator('[data-diff-of="title"] ins')).toContainText("by card");
  });

  test("an author asks for review of a section, and a reviewer approves one and rejects another in the document", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Document" } }));
    const path = { params: { path: { projectKey: KEY } } };
    must(
      await api.PUT("/projects/{projectKey}/grants", {
        ...path,
        body: {
          grants: [
            { subject: { type: "user", id: people.alice.id }, role: "admin" },
            { subject: { type: "everyone" }, role: "editor" },
            { subject: { type: "user", id: people.bob.id }, role: "reviewer" },
          ],
        },
      }),
    );
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...path,
        body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: people.bob.id }] },
      }),
    );
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { ...path, body: body as never })).requirement;
    const scope = await write({ kind: "section", title: "Scope" });
    const pay = await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
    const ship = await write({ title: "Ship", statement: paragraph("The basket ships."), place: { parentId: scope.id, afterId: pay.id } });

    // Nothing is approved yet, so against the last approval both are new, and the section asks for both at once.
    await page.goto(`/p/${KEY}/spec?changes=approved`);
    await page.locator(`[data-spec-entry="${scope.key}"] [data-action="ask-reviews"]`).click();
    await expect(page.locator("[data-spec-announced]")).toHaveText("2 reviews asked for.");

    const bob = await pageAs("bob");
    await openUntil(bob, "/reviews", () => expect(bob.locator(`[data-review-in-document="${KEY}"]`).first()).toBeVisible(ONE_LOOK));
    await bob.locator(`[data-review-in-document="${KEY}"]`).first().click();
    await expect(bob).toHaveURL(new RegExp(`/p/${KEY}/spec\\?`));
    const entry = (key: string) => bob.locator(`[data-spec-entry="${key}"]`);
    await expect(bob.locator("[data-review-progress]")).toHaveText("0 of 2 decided");
    await expect(entry(pay.key).locator('[data-spec-change="added"]')).toBeVisible();
    await expectAccessible(bob);

    await entry(pay.key).locator('[data-action="approve-entry"]').click();
    await expect(entry(pay.key).locator('[data-your-decision="approve"]')).toBeVisible();
    await entry(ship.key).getByLabel("Your comment").fill("Say how fast it ships.");
    await entry(ship.key).locator('[data-action="reject-entry"]').click();
    await expect(entry(ship.key).locator('[data-your-decision="reject"]')).toBeVisible();
    await expect(bob.locator("[data-review-progress]")).toHaveText("2 of 2 decided");

    await bob.locator('[data-action="leave-review"]').click();
    await expect(entry(pay.key).locator("[data-entry-review]")).toHaveCount(0);
  });
});

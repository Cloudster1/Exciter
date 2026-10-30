import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test, type Person } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { unzipText } from "../fixtures/unzip";
import { setProjectGrants } from "../fixtures/grants";

// Every control of the specification page, each used as its reader would and checked for what it changed (#310).

const KEY = uniqueKey("SPP");
// One more page of the document than its first, which holds SPEC_PAGE entries.
const SPEC_PAGE = 50;
const PAGED = 55;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const project = { params: { path: { projectKey: KEY } } };

/** A project of a section, Scope, holding Pay and Ship, and Other at the top after it. */
async function scoped(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Specified" } }));
  const write = async (body: Record<string, unknown>) =>
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: body as never })).requirement;
  const scope = await write({ kind: "section", title: "Scope" });
  const pay = await write({ title: "Pay", statement: paragraph("The shopper pays at once."), place: { parentId: scope.id, afterId: null } });
  const ship = await write({ title: "Ship", statement: paragraph("The basket ships."), place: { parentId: scope.id, afterId: pay.id } });
  const other = await write({ title: "Other", statement: paragraph("Something else entirely.") });
  return { scope, pay, ship, other };
}

/** Bob reviews and approves in the project, which everybody else edits. */
async function bobApproves(api: ExciterApi, people: Record<"alice" | "bob", Person>) {
  await setProjectGrants(api, KEY, [
    { subject: { type: "user", id: people.alice.id }, role: "admin" },
    { subject: { type: "everyone" }, role: "editor" },
    { subject: { type: "user", id: people.bob.id }, role: "reviewer" },
  ]);
  must(
    await api.PUT("/projects/{projectKey}/approval-rules", {
      ...project,
      body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: people.bob.id }] },
    }),
  );
}

async function askReview(api: ExciterApi, reqKey: string) {
  return must(await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey } }, body: { revision: 1 } })).review;
}

const entryOf = (page: Page, key: string) => page.locator(`[data-spec-entry="${key}"]`);

test.describe("the specification page", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice folds and opens the tree and moves a requirement with the keyboard alone", async ({ page, api }) => {
    const { pay, other } = await scoped(api);
    await page.goto(`/projects/${KEY}/spec`);
    const tree = page.getByRole("tree");
    const item = (name: string) => tree.getByRole("treeitem", { name });

    await item("1 Scope").focus();
    await page.keyboard.press("ArrowLeft");
    await expect(item("1 Scope")).toHaveAttribute("aria-expanded", "false");
    await expect(item("1.1 Pay")).toHaveCount(0);
    await page.keyboard.press("ArrowRight");
    await expect(item("1.1 Pay")).toBeVisible();

    await page.keyboard.press("End");
    await expect(item("2 Other")).toBeFocused();
    await page.keyboard.press("m");
    const dialog = page.locator('[data-place-dialog="move"]');
    await expect(page.getByRole("dialog", { name: `Move ${other.key}` })).toBeVisible();
    await dialog.getByLabel("Below").selectOption(pay.id);
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-move"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(item("1.1.1 Other")).toBeVisible();
    await expect(entryOf(page, other.key).locator("[data-spec-outline]")).toHaveText("1.1.1");
  });

  test("alice turns to the next page of a long specification, which goes on where the first ended", async ({ page, api }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Long" } }));
    for (let n = 1; n <= PAGED; n++) {
      must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: `Need ${n}` } }));
    }
    await page.goto(`/projects/${KEY}/spec`);
    const entries = page.locator("[data-spec-entry]");
    await expect(entries).toHaveCount(SPEC_PAGE);
    await page.locator('[data-action="spec-more"]').click();
    await expect(entries).toHaveCount(PAGED);
    await expect(page.locator('[data-action="spec-more"]')).toHaveCount(0);
    await expect(entryOf(page, `${KEY}-R${SPEC_PAGE + 1}`).locator("[data-spec-outline]")).toHaveText(String(SPEC_PAGE + 1));
    await expect(entryOf(page, `${KEY}-R${PAGED}`).locator("[data-spec-outline]")).toHaveText(String(PAGED));
    const keys = await entries.evaluateAll((all) => all.map((each) => each.getAttribute("data-spec-entry")));
    expect(new Set(keys).size).toBe(PAGED);
  });

  test("alice narrows the specification to one status, and back to every status", async ({ page, api }) => {
    const { scope, pay, ship, other } = await scoped(api);
    const submit = must(await api.GET("/requirements/{reqKey}/transitions", { params: { path: { reqKey: pay.key } } })).transitions.find(
      (each) => each.name === "Submit for review",
    )!;
    must(await api.POST("/requirements/{reqKey}/transitions", { params: { path: { reqKey: pay.key } }, body: { transitionId: submit.id } }));

    await page.goto(`/projects/${KEY}/spec`);
    await expect(entryOf(page, pay.key).locator('[data-status="In review"]')).toBeVisible();
    const filter = page.locator("[data-filter-status]");
    await filter.selectOption({ label: "In review" });
    await expect(page).toHaveURL(/status=/);
    await expect(entryOf(page, pay.key)).toBeVisible();
    await expect(entryOf(page, scope.key)).toBeVisible();
    for (const gone of [ship.key, other.key]) await expect(entryOf(page, gone)).toHaveCount(0);
    await expectAccessible(page);

    await page.reload();
    await expect(filter.locator("option:checked")).toHaveText("In review");
    await expect(entryOf(page, ship.key)).toHaveCount(0);
    await page.locator('[data-action="clear-status-filter"]').click();
    await expect(page).not.toHaveURL(/status=/);
    for (const back of [scope.key, pay.key, ship.key, other.key]) await expect(entryOf(page, back)).toBeVisible();

    await filter.selectOption({ label: "Approved" });
    await expect(page.locator("[data-spec-empty-filter]")).toHaveText("No requirement is in those statuses. Show every status, or pick others.");
    await expect(page.locator("[data-spec-entry]")).toHaveCount(0);
  });

  test("alice reads only what changed since a baseline, steps back through it, and sees what left the document", async ({ page, api }) => {
    const { pay, ship, other } = await scoped(api);
    must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Before" } }));
    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: pay.key } },
        body: { title: "Pay by card", statement: paragraph("The shopper pays by card."), baseRevision: 1 },
      }),
    );
    expect((await api.DELETE("/requirements/{reqKey}", { params: { path: { reqKey: other.key } } })).response.status).toBe(204);

    await page.goto(`/projects/${KEY}/spec`);
    await page.locator('[data-action="show-changes"]').click();
    await page.locator("[data-changes-base]").selectOption("baseline");
    await page.locator("[data-changes-baseline]").selectOption({ label: "Before" });
    await expect(entryOf(page, pay.key).locator('[data-spec-change="changed"]')).toBeVisible();
    const removed = page.locator("[data-spec-removed]");
    await expect(removed).toContainText("No longer in the document");
    await expect(removed.locator(`[data-removed="${other.key}"]`)).toContainText("Other");

    await expect(entryOf(page, ship.key).getByText("The basket ships.")).toBeVisible();
    await page.locator('[data-filter="only-changed"]').click();
    await expect(page).toHaveURL(/onlyChanged=true/);
    await expect(entryOf(page, ship.key).getByRole("heading")).toHaveText("1.2 Ship");
    await expect(entryOf(page, ship.key).getByText("The basket ships.")).toHaveCount(0);
    await expect(entryOf(page, pay.key).locator('[data-diff-of="statement"] ins')).toHaveText([/by/, /card/]);
    await expectAccessible(page);

    await entryOf(page, ship.key).focus();
    await page.locator('[data-action="previous-change"]').click();
    await expect(entryOf(page, pay.key)).toBeFocused();
    await page.locator('[data-action="previous-change"]').click();
    await expect(page.locator("[data-spec-announced]")).toHaveText("No further change among the entries shown.");
  });

  test("bob enters review mode from the page and sees only what waits for him", async ({ page, api, pageAs, people }) => {
    const { pay, ship, other } = await scoped(api);
    await bobApproves(api, people);
    const bob = await pageAs("bob");
    await openUntil(bob, `/projects/${KEY}/spec`, () => expect(entryOf(bob, pay.key)).toBeVisible(ONE_LOOK));
    await bob.locator('[data-action="review-mode"]').click();
    await expect(bob.locator("[data-nothing-waiting]")).toHaveText("Nothing in this project waits for your decision.");
    await expect(bob.locator("[data-spec-entry]")).toHaveCount(0);

    for (const each of [pay, ship]) await askReview(api, each.key);
    await expect(async () => {
      await bob.goto(`/projects/${KEY}/spec`);
      await bob.locator('[data-action="review-mode"]').click();
      await expect(bob.locator("[data-review-progress]")).toHaveText("0 of 2 decided", ONE_LOOK);
    }).toPass();
    await expect(bob).toHaveURL(/waiting=true/);
    for (const waiting of [pay.key, ship.key]) await expect(entryOf(bob, waiting).locator("[data-entry-review]")).toBeVisible();
    await expect(entryOf(bob, other.key)).toHaveCount(0);
    await expectAccessible(bob);
    await page.goto(`/projects/${KEY}/spec`);
    await expect(entryOf(page, other.key)).toBeVisible();
  });

  test("alice answers a comment inside its entry, and adds a requirement first below a section", async ({ page, api }) => {
    const { scope, pay, ship } = await scoped(api);
    must(await api.POST("/requirements/{reqKey}/comments", { params: { path: { reqKey: pay.key } }, body: { body: paragraph("Which cards?") } as never }));

    await page.goto(`/projects/${KEY}/spec`);
    const comments = entryOf(page, pay.key).locator('[data-action="entry-comments"]');
    await expect(comments).toHaveText("Comments (1 open)");
    await comments.click();
    await expect(comments).toHaveAttribute("aria-expanded", "true");
    const discussion = entryOf(page, pay.key).getByRole("region", { name: "Discussion" });
    await expect(discussion.getByRole("article")).toContainText("Which cards?");
    await discussion.getByRole("button", { name: "Reply" }).click();
    await page.locator("[id^=reply-]").click();
    await page.locator("[id^=reply-]").pressSequentially("Every card the shop takes.");
    await discussion.getByRole("button", { name: "Post reply" }).click();
    await expect(discussion.getByRole("article")).toContainText("Every card the shop takes.");
    await expectAccessible(page);

    await entryOf(page, scope.key).locator('[data-action="add-entry"]').click();
    await page.locator('[data-action="add-below"]').click();
    const adding = page.locator("[data-spec-adding]");
    await expect(adding).toContainText(`New, first below ${scope.key}`);
    await adding.getByLabel("Title", { exact: true }).fill("Refund");
    await adding.locator('[data-action="create-requirement"]').click();
    await expect(page.getByRole("heading", { name: "1.1 Refund" })).toBeVisible();
    await expect(entryOf(page, pay.key).locator("[data-spec-outline]")).toHaveText("1.2");
    await expect(entryOf(page, ship.key).locator("[data-spec-outline]")).toHaveText("1.3");

    await page.goto(`/projects/${KEY}/r/${pay.key}`);
    await expect(page.getByRole("region", { name: "Discussion" }).getByRole("article")).toContainText("Every card the shop takes.");
  });

  test("alice exports a section as Word from its own menu, and its print view says it is ready", async ({ page, api }) => {
    test.setTimeout(60_000);
    const { scope } = await scoped(api);
    await page.goto(`/projects/${KEY}/spec`);
    await expect(entryOf(page, scope.key).getByRole("heading")).toHaveText("1 Scope");
    const download = page.waitForEvent("download");
    await entryOf(page, scope.key).getByRole("button", { name: "Export Scope" }).click();
    await page.getByRole("menuitem", { name: "Word" }).click();
    const saved = await download;
    const body = unzipText(readFileSync(await saved.path()))["word/document.xml"] ?? "";
    for (const held of ["Scope", "The shopper pays at once.", "The basket ships."]) expect(body).toContain(held);
    expect(body).not.toContain("Something else entirely.");

    await page.goto(`/print/p/${KEY}/document?section=${scope.key}`);
    await expect(page.locator("html[data-print-ready]")).toBeAttached();
    await expect(page.getByText(`Section ${scope.key} of Specified (${KEY})`)).toBeVisible();
    await expect(page.getByText("The basket ships.")).toBeVisible();
    await expect(page.getByText("Something else entirely.")).toHaveCount(0);
  });

  test("bob signs his approval in the document with his password", async ({ api, pageAs, people }) => {
    const { pay } = await scoped(api);
    await bobApproves(api, people);
    must(await api.PATCH("/projects/{projectKey}", { ...project, body: { settings: { signedApprovals: true } } }));
    const asked = await askReview(api, pay.key);

    const bob = await pageAs("bob");
    const approve = entryOf(bob, pay.key).locator('[data-action="approve-entry"]');
    await openUntil(bob, `/projects/${KEY}/spec?waiting=true&changes=approved`, () => expect(approve).toBeVisible(ONE_LOOK));
    await approve.click();
    const dialog = bob.getByRole("dialog", { name: "Sign: approve" });
    await expect(dialog).toContainText(`I approve revision 1 of ${pay.key}, Pay, as one of its approvers.`);
    await expectAccessible(bob);
    await dialog.getByLabel("Your password").fill(people.bob.password);
    await dialog.getByRole("button", { name: "Sign and approve" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(entryOf(bob, pay.key).locator('[data-your-decision="approve"]')).toBeVisible();
    await expect(bob.locator("[data-review-progress]")).toHaveText("1 of 1 decided");

    const decisions = async () => must(await api.GET("/reviews/{reviewID}", { params: { path: { reviewID: asked.id } } })).review.decisions;
    await expect
      .poll(decisions)
      .toEqual([expect.objectContaining({ decision: "approve", signature: expect.objectContaining({ method: "password", revision: 1 }) })]);
  });
});

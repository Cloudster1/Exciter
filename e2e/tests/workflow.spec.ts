import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect, USERS } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible } from "../fixtures/shell";

const KEY = "FLOW";
const REQUIREMENT = `/p/${KEY}/r/${KEY}-R1`;
/** How many moves each leg of a drag is made of. */
const DRAG_STEPS = 4;

/** Archives and deletes the project the test made, so each test starts without it. */
async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project with one requirement in Draft, alice its administrator and author, and bob a reviewer too. */
async function makeProject(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Flowing" } }));
  must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Pay the basket" } }));
  const members = must(await api.GET("/users")).members;
  const id = (email: string) => members.find((each) => each.email === email)!.userId;
  must(
    await api.PUT("/projects/{projectKey}/grants", {
      params: { path: { projectKey: KEY } },
      body: {
        grants: [
          { subject: { type: "user", id: id(USERS.alice.email) }, role: "admin" },
          { subject: { type: "everyone" }, role: "editor" },
          { subject: { type: "user", id: id(USERS.bob.email) }, role: "reviewer" },
        ],
      },
    }),
  );
}

/** Draws a transition the way a person does: from a status's ring onto another, in small steps, since React
 * Flow measures a drag from where it begins. */
async function dragConnect(page: Page, from: string, to: string) {
  const start = await centreOf(page.locator(`[data-connect-handle="${from}"]`));
  const end = await centreOf(page.locator(`[data-workflow-node="${to}"]`));
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(start.x + 2, start.y, { steps: 2 });
  await page.mouse.move((start.x + end.x) / 2, (start.y + end.y) / 2, { steps: DRAG_STEPS });
  await page.mouse.move(end.x, end.y, { steps: DRAG_STEPS });
  await page.mouse.up();
}

async function centreOf(locator: Locator) {
  await locator.scrollIntoViewIfNeeded();
  const box = (await locator.boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

// The organization is this file's alone, so its project meets no other spec.
test.describe("workflow", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice submits a requirement, bob approves it, and a new revision sends it back to Draft", async ({ page, api, pageAs }) => {
    await makeProject(api);
    await page.goto(REQUIREMENT);
    const bar = page.locator("[data-transition-bar]");
    await expect(bar.locator('[data-status="Draft"]')).toBeVisible();
    await bar.getByRole("button", { name: "Submit for review" }).click();
    await expect(page.getByText("Moved to In review.")).toBeVisible();
    // Alice wrote the latest revision, so she is not offered its approval.
    await expect(bar.getByRole("button", { name: "Send back" })).toBeVisible();
    await expect(bar.getByRole("button", { name: "Approve" })).toHaveCount(0);
    await expectAccessible(page);

    // Alice moved it, so bob's read may come from a replica a moment behind.
    const bob = await pageAs("bob");
    await openUntil(bob, REQUIREMENT, () => expect(bob.locator("[data-transition-bar]").getByRole("button", { name: "Approve" })).toBeVisible(ONE_LOOK));
    await bob.locator("[data-transition-bar]").getByRole("button", { name: "Approve" }).click();
    await expect(bob.getByText("Moved to Approved.")).toBeVisible();

    await openUntil(page, REQUIREMENT, () => expect(bar.locator('[data-status="Approved"]')).toBeVisible(ONE_LOOK));
    await page.locator('[data-action="edit-requirement"]').click();
    await page.getByLabel("Title", { exact: true }).fill("Pay the whole basket");
    await page.locator('[data-action="save-requirement"]').click();
    await expect(page.getByText("Saved as revision 2.")).toBeVisible();
    await expect(bar.locator('[data-status="Draft"]')).toBeVisible();

    await bar.getByRole("button", { name: "Reject" }).click();
    const dialog = page.getByRole("dialog", { name: "Reject" });
    await dialog.getByLabel("Why").fill("Out of scope for this release.");
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm-transition"]').click();
    await expect(page.getByText("Moved to Rejected.")).toBeVisible();
    await expect(bar.locator('[data-status="Rejected"]')).toBeVisible();
  });

  test("an administrator draws a workflow and a type follows it", async ({ page, api }) => {
    await makeProject(api);
    await page.goto(`/p/${KEY}/settings?tab=workflow`);
    await expect(page.getByRole("figure", { name: "Workflow Default" })).toBeVisible();
    await expect(page.locator('[data-status-row="Obsolete"]')).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="new-workflow"]').click();
    await page.getByLabel("Name", { exact: true }).fill("Lean");
    for (const status of ["Draft", "In review"]) {
      await page.getByLabel("Add a status").selectOption({ label: status });
      await page.locator('[data-action="add-status"]').click();
    }
    await dragConnect(page, "Draft", "In review");
    await page.getByLabel("Transition name").fill("Ask");
    await expect(page.locator('[data-workflow-edge-label="Ask"]')).toBeVisible();
    await expectAccessible(page);
    await page.locator('[data-action="save-workflow"]').click();
    await expect(page).toHaveURL(`/p/${KEY}/settings?tab=workflow`);
    await expect(page.locator('[data-workflow-card="Lean"]')).toBeVisible();

    await page.getByLabel("Workflow of Requirement").selectOption({ label: "Lean" });
    const dialog = page.getByRole("dialog", { name: "Follow Lean" });
    await expect(dialog.getByText("Every requirement's status is one this workflow has, so nothing moves.")).toBeVisible();
    await dialog.locator('[data-action="confirm-assign"]').click();
    await expect(page.locator('[data-workflow-card="Lean"]')).toContainText("Followed by Requirement.");

    await page.goto(REQUIREMENT);
    const bar = page.locator("[data-transition-bar]");
    await expect(bar.getByRole("button", { name: "Ask" })).toBeVisible();
    await expect(bar.getByRole("button", { name: "Submit for review" })).toHaveCount(0);
  });
});

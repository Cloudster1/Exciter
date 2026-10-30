import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm, toast, dismissToasts } from "../fixtures/shell";

// Every control of a review and of the approval rules (#317): bob rejects with a comment that alice then reads, alice
// asks again and withdraws, and a rule of one type is added, changed and removed.

const KEY = uniqueKey("RVC");
const REQ_KEY = `${KEY}-R1`;
const REQUIREMENT = `/p/${KEY}/r/${REQ_KEY}`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("review controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("bob rejects with a comment alice reads, and alice asks again and withdraws", async ({ page, api, pageAs, people }) => {
    // Two people take turns over the requirement page and the inbox: about 20 s on CI.
    test.setTimeout(60_000);
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Rejected" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...project,
        body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: people.bob.id }] },
      }),
    );
    must(await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey: REQ_KEY } }, body: { revision: 1 } }));

    const bob = await pageAs("bob");
    const bobs = bob.getByRole("region", { name: "Approval" });
    await openUntil(bob, REQUIREMENT, () => expect(bobs.locator('[data-action="reject-review"]')).toBeVisible(ONE_LOOK));
    await bobs.getByLabel("Your comment").fill("Say how fast it engages when wet.");
    await bobs.locator('[data-action="reject-review"]').click();
    await expect(bobs.locator('[data-past-review="rejected"]')).toBeVisible();
    await expect(bobs.locator('[data-action="reject-review"]')).toHaveCount(0);

    const approval = page.getByRole("region", { name: "Approval" });
    const rejected = approval.locator('[data-past-review="rejected"]');
    await openUntil(page, REQUIREMENT, () => expect(rejected).toBeVisible(ONE_LOOK));
    await expect(rejected).toContainText(`${people.bob.name} rejected: Say how fast it engages when wet.`);
    await expect(approval).toContainText("No revision is approved yet.");
    await expectAccessible(page);

    await approval.getByRole("button", { name: "Ask for review" }).click();
    await approval.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(approval).toContainText(`Revision 1 is under review, asked by ${people.alice.name}.`);
    const waiting = bob.locator('[data-count="reviews"]');
    await openUntil(bob, "/reviews", () => expect(waiting).toBeVisible(ONE_LOOK));

    await approval.locator('[data-action="withdraw-review"]').click();
    await confirm(page);
    await expect(approval.locator('[data-past-review="withdrawn"]')).toBeVisible();
    await expect(approval.locator("[data-pending-review]")).toHaveCount(0);
    await expect(approval.getByRole("button", { name: "Ask for review" })).toBeVisible();
    await openUntil(bob, "/reviews", () => expect(bob.getByText("Nothing waits for your decision.")).toBeVisible(ONE_LOOK));
    await expect(waiting).toHaveCount(0);
  });

  test("alice adds a rule for one type, changes it and removes it", async ({ page, api, people }) => {
    test.setTimeout(60_000);
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Typed rules" } }));
    const safety = must(await api.POST("/projects/{projectKey}/types", { ...project, body: { name: "Safety", icon: "requirement", colour: "#dc2626" } })).type;
    const safe = must(
      await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes hold on ice", typeId: safety.id } }),
    ).requirement;
    const other = must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Lights dim at night" } })).requirement;

    await page.goto(`/p/${KEY}/settings?tab=approval`);
    const rules = page.locator("[data-approval-rules]");
    await rules.getByRole("button", { name: "Add a rule" }).click();
    await rules.getByLabel("Applies to").selectOption({ label: "Safety" });
    await rules.getByRole("combobox", { name: "Add a person or a group" }).pressSequentially(people.bob.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await rules.getByRole("button", { name: "Save rule" }).click();
    const rule = rules.locator('[data-approval-rule="Safety"]');
    await expect(rule).toContainText(`1 of ${people.bob.name}`);

    await page.goto(`/p/${KEY}/r/${safe.key}`);
    const approval = page.getByRole("region", { name: "Approval" });
    await approval.getByRole("button", { name: "Ask for review" }).click();
    await approval.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(approval.locator("[data-review-approvers]")).toHaveText(people.bob.name);
    await page.goto(`/p/${KEY}/r/${other.key}`);
    await approval.getByRole("button", { name: "Ask for review" }).click();
    await approval.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(approval).toContainText("Nobody approves requirements of this type yet.");

    await page.goto(`/p/${KEY}/settings?tab=approval`);
    await rule.locator('[data-action="edit-rule"]').click();
    const form = rules.locator("[data-rule-form]");
    await expect(form.getByLabel("Applies to")).toBeDisabled();
    await form.getByRole("combobox", { name: "Add a person or a group" }).pressSequentially(people.alice.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.alice.name) }).click();
    await form.getByLabel("Approvals needed").fill("2");
    await form.getByLabel("The author of a revision may approve it too").check();
    await expectAccessible(page);
    await form.getByRole("button", { name: "Save rule" }).click();
    await expect(toast(page, "The rule is saved.")).toHaveText("The rule is saved. Reviews already asked for keep the approvers they had.");
    await dismissToasts(page);
    await expect(rule).toContainText("2 of");
    await expect(rule).toContainText(people.alice.name);
    await expect(rule).toContainText("The author may approve");
    const saved = must(await api.GET("/projects/{projectKey}/approval-rules", project)).rules;
    expect(saved).toEqual([expect.objectContaining({ typeId: safety.id, required: 2, authorMayApprove: true })]);

    await rule.locator('[data-action="remove-rule"]').click();
    await confirm(page);
    await expect(toast(page, "The rule is removed.")).toBeVisible();
    await dismissToasts(page);
    await expect(rules.locator("[data-approval-rules-empty]")).toBeVisible();
    await expect.poll(async () => must(await api.GET("/projects/{projectKey}/approval-rules", project)).rules).toEqual([]);
  });
});

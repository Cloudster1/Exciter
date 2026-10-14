import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";

// Review and approval (#35): alice names bob the approver, asks for her revision to be approved, bob approves it
// from his inbox, and alice's next revision is no longer approved.

const KEY = uniqueKey("SIGN");
const REQ_KEY = `${KEY}-R1`;
const REQUIREMENT = `/p/${KEY}/r/${REQ_KEY}`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("approval", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice asks bob to approve her revision, bob approves it, and her next revision is not approved", async ({ page, api, pageAs, people }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Signed off" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));

    await page.goto(`/p/${KEY}/settings?tab=approval`);
    const rules = page.locator("[data-approval-rules]");
    await rules.getByRole("button", { name: "Add a rule" }).click();
    await rules.getByRole("combobox", { name: "Add a person or a group" }).pressSequentially(people.bob.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await expectAccessible(page);
    await rules.getByRole("button", { name: "Save rule" }).click();
    await expect(rules.getByRole("list", { name: "Approval rules" })).toContainText(`1 of ${people.bob.name}`);

    await page.goto(REQUIREMENT);
    const approval = page.getByRole("region", { name: "Approval" });
    await expect(approval).toContainText("No revision is approved yet.");
    await approval.getByRole("button", { name: "Ask for review" }).click();
    await approval.getByLabel("Note to the approvers").fill("Ready to sign off.");
    await approval.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(approval).toContainText(`Revision 1 is under review, asked by ${people.alice.name}.`);
    await expect(approval.getByRole("button", { name: "Approve" })).toHaveCount(0);

    const bob = await pageAs("bob");
    const inbox = bob.getByRole("link", { name: "1 review waits for you" });
    await openUntil(bob, "/projects", () => expect(inbox).toBeVisible(ONE_LOOK));
    await inbox.click();
    await bob
      .getByRole("list", { name: "Reviews" })
      .getByRole("link", { name: new RegExp(REQ_KEY) })
      .click();
    const bobs = bob.getByRole("region", { name: "Approval" });
    await bobs.getByLabel("Your comment").fill("Agreed.");
    await expectAccessible(bob);
    await bobs.getByRole("button", { name: "Approve" }).click();
    await expect(bobs).toContainText("Revision 1 is approved.");

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: REQ_KEY } },
        body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
      }),
    );
    await openUntil(page, REQUIREMENT, () => expect(approval).toContainText("Revision 1 was approved; revision 2 is not yet.", ONE_LOOK));
    await expect(approval.getByRole("button", { name: "Ask for review" })).toBeVisible();
  });
});

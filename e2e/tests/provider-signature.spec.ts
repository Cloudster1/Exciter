import type { Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect, test, USERS } from "../fixtures/auth";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { KEYCLOAK_URL, WEB_URL } from "../fixtures/stack";
import { expectAccessible } from "../fixtures/shell";

// A signature given at the identity provider (#46, #317): bob, who signs in through Keycloak and has no password
// here, cancels a signature, then signs in again at Keycloak and comes back to sign with his comment kept.

const KEY = uniqueKey("PSIG");
const REQ_KEY = `${KEY}-R1`;
const REQUIREMENT = `/projects/${KEY}/r/${REQ_KEY}`;

/** Types the password again on Keycloak's re-authentication form, which keeps the username it knows, and waits until
 *  the browser is back in Exciter. */
async function reauthenticateAtKeycloak(page: Page, password: string): Promise<void> {
  await page.waitForURL(`${KEYCLOAK_URL}/**`);
  await expect(page.getByText("Please re-authenticate to continue")).toBeVisible();
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click({ noWaitAfter: true });
  await page.waitForURL((url) => url.origin === new URL(WEB_URL).origin && !url.pathname.startsWith("/api/"));
}

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("a signature at the identity provider", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("bob cancels a signature, then signs in again at Keycloak and signs his approval", async ({ api, apiAs, pageAs }) => {
    // A round trip through Keycloak beside the requirement page: about 15 s on CI.
    test.setTimeout(60_000);
    const project = { params: { path: { projectKey: KEY } } };
    const bobsMe = must(await (await apiAs("bob")).GET("/auth/me"));
    expect(bobsMe.signsWith, "the seed's bob signs in through Keycloak alone").toBe("provider");
    const bobId = bobsMe.user.id;
    must(await api.POST("/projects", { body: { key: KEY, name: "Signed at the provider" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: { title: "Brakes engage within 100 ms" } }));
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...project,
        body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: bobId }] },
      }),
    );
    must(await api.PATCH("/projects/{projectKey}", { ...project, body: { settings: { signedApprovals: true } } }));
    const asked = must(await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey: REQ_KEY } }, body: { revision: 1 } })).review;

    const bob = await pageAs("bob");
    const approval = bob.getByRole("region", { name: "Approval" });
    await openUntil(bob, REQUIREMENT, () => expect(approval.getByRole("button", { name: "Approve" })).toBeVisible(ONE_LOOK));
    await approval.getByLabel("Your comment").fill("Signed at the provider.");
    await approval.getByRole("button", { name: "Approve" }).click();
    const dialog = bob.getByRole("dialog", { name: "Sign: approve" });
    await expect(dialog.locator('[data-action="sign-with-provider"]')).toHaveText("Sign in again to sign");
    await dialog.locator('[data-action="cancel-sign"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(approval).toContainText("Revision 1 is under review");
    expect(must(await api.GET("/reviews/{reviewID}", { params: { path: { reviewID: asked.id } } })).review).toMatchObject({ state: "pending", decisions: [] });

    await approval.getByRole("button", { name: "Approve" }).click();
    await dialog.locator('[data-action="sign-with-provider"]').click();
    await reauthenticateAtKeycloak(bob, USERS.bob.password);
    await expect(dialog).toContainText("You signed in again. Sign now; the sign-in counts for this one signature.");
    await expect(approval.getByLabel("Your comment")).toHaveValue("Signed at the provider.");
    await expectAccessible(bob);
    await dialog.locator('[data-action="sign-after-provider"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(approval).toContainText("Revision 1 is approved.");

    // Alice reads from a replica that may not have caught up with bob's signature yet.
    const decisions = async () => must(await api.GET("/reviews/{reviewID}", { params: { path: { reviewID: asked.id } } })).review.decisions;
    await expect.poll(decisions).toEqual([
      expect.objectContaining({
        decision: "approve",
        comment: "Signed at the provider.",
        signature: expect.objectContaining({ method: "oidc", revision: 1 }),
      }),
    ]);
  });
});

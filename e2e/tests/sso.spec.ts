import { must } from "../fixtures/api";
import { ANONYMOUS, expect, ME_PATH, ORG_SLUG, ownerApi, signIn, standing, startSSO, submitKeycloak, test, USERS } from "../fixtures/auth";
import { deadProvider } from "../fixtures/db";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { WEB_URL } from "../fixtures/stack";

// alice's group at the stack's Keycloak, from deploy/keycloak/realm.json.
const ADMINS_GROUP = "exciter-administrators";
const MEMBERS_PATH = "/settings/members";
const NOT_CONFIGURED = "That organization does not sign in through a provider. Check its name, or ask one of its administrators.";
const UNREACHABLE = "Exciter could not reach your organization's identity provider. Try again in a moment, and tell an administrator if it keeps failing.";

// The sign-in a browser alone can walk: the login page, Keycloak's own form, and back. It changes the installation's provider and
// alice's role there, which nobody else's spec signs in through, and runs alone.
test.describe("single sign-on", () => {
  test.use({ storageState: ANONYMOUS });

  test("alice signs in through Keycloak and lands in the installation as the administrator she is", async ({ page }) => {
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
    await signIn(page, "alice");
    await expect.poll(() => standing(page)).toEqual({ slug: ORG_SLUG, role: "admin" });
    await page.goto(MEMBERS_PATH);
    await expect(page.locator("[data-members]")).toBeVisible();
    await expect(page.locator("[data-account-name]")).toHaveText(USERS.alice.name);
  });

  test("a visitor opening the themes page signs in through Keycloak and comes back to it", async ({ page }) => {
    await page.goto("/settings/themes");
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fthemes$/);
    await startSSO(page);
    await submitKeycloak(page, USERS.bob.username, USERS.bob.password);
    await expect(page).toHaveURL(/\/settings\/themes$/);
    await expect(page.getByRole("heading", { level: 1, name: "Themes" })).toBeVisible();
  });

  test("an organization without single sign-on sends the visitor back with a message", async ({ page }) => {
    await startSSO(page, "no-such-org");
    await expect(page).toHaveURL(/\/login\?sso=not_configured/);
    await expect(page.locator("[data-login]").getByRole("alert")).toHaveText(NOT_CONFIGURED);
  });

  test("a provider that cannot be reached says so", async ({ page }) => {
    const provider = await deadProvider();
    try {
      await startSSO(page);
      await expect(page).toHaveURL(/\/login\?sso=unreachable/);
      await expect(page.locator("[data-login]").getByRole("alert")).toHaveText(UNREACHABLE);
      expect((await page.request.get(ME_PATH)).status()).toBe(401);
    } finally {
      await provider.restore();
    }
  });
});

test.describe("roles from the provider's groups", { tag: "@desktop" }, () => {
  test("alice, made a member by hand, signs in and is the administrator her group makes her", async ({ browser }) => {
    const owner = await ownerApi();
    const alice = must(await owner.GET("/users")).members.find((each) => each.email === USERS.alice.email);
    expect(alice, "the seed let alice in").toBeDefined();
    const aliceId = alice!.userId;
    let mappingId: string | undefined;
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      must(await owner.PATCH("/users/{userID}", { params: { path: { userID: aliceId } }, body: { role: "member" } }));
      mappingId = must(await owner.POST("/oidc-provider/group-roles", { body: { group: ADMINS_GROUP, role: "admin" } })).groupRole.id;

      const page = await context.newPage();
      await signIn(page, "alice");
      await expect.poll(() => standing(page)).toEqual({ slug: ORG_SLUG, role: "admin" });
      await openUntil(page, MEMBERS_PATH, async () => {
        const row = page.locator(`[data-member="${USERS.alice.email}"]`);
        await expect(row.locator('[data-role-source="oidc"]')).toBeVisible(ONE_LOOK);
        await expect(row.locator("[data-member-role]")).toHaveAttribute("data-member-role", "admin", ONE_LOOK);
      });
    } finally {
      await context.close();
      if (mappingId) await owner.DELETE("/oidc-provider/group-roles/{groupRoleID}", { params: { path: { groupRoleID: mappingId } } });
      await owner.PATCH("/users/{userID}", { params: { path: { userID: aliceId } }, body: { role: "admin" } });
      await owner.dispose();
    }
  });
});

import type { Browser, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import {
  ANONYMOUS,
  anonymousContext,
  expect,
  LOGIN_PATH,
  ME_PATH,
  ownerApi,
  signIn,
  signInWithPassword,
  standing,
  startSSO,
  submitKeycloak,
  test,
  USERS,
} from "../fixtures/auth";
import { deadProvider, withDatabase } from "../fixtures/db";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { openAccountMenu, toast, dismissToasts } from "../fixtures/shell";

// alice's group at the stack's Keycloak, from deploy/keycloak/realm.json.
const ADMINS_GROUP = "exciter-administrators";
const MEMBERS_PATH = "/settings/members";
const SSO_PATH = "/settings/sso";
const SETTINGS_PATH = "/settings";
// A claim the realm's tokens do not carry, so a sign-in reads no groups through it and still signs in.
const OTHER_CLAIM = "e2e_groups";
const WAITING = "Your request is waiting";
// carol and dave are in the realm, and nobody lets them in: the seed names alice and bob alone.
const CAROL = { username: "carol", password: "carol password", email: "carol@suite.test" };
const DAVE = { username: "dave", password: "dave password", email: "dave@suite.test" };

/** A browser of its own, so Keycloak's session from an earlier sign-in never skips the form. */
async function freshPage(browser: Browser): Promise<{ page: Page; close: () => Promise<void> }> {
  const context = await anonymousContext(browser);
  return { page: await context.newPage(), close: () => context.close() };
}

/** Signs somebody in through Keycloak in a browser of their own, and answers where they landed. */
async function signInFresh(browser: Browser, username: string, password: string): Promise<{ page: Page; close: () => Promise<void> }> {
  const fresh = await freshPage(browser);
  await startSSO(fresh.page);
  await submitKeycloak(fresh.page, username, password);
  return fresh;
}

/** Forgets the realm's people nobody lets in, so each run meets them as strangers again. */
async function forgetStrangers(): Promise<void> {
  await withDatabase((db) => db.query("DELETE FROM app_user WHERE email = ANY($1)", [[CAROL.email, DAVE.email]]));
}
const UNREACHABLE = "Exciter could not reach the identity provider. Try again in a moment, and tell an administrator if it keeps failing.";

// The sign-in a browser alone can walk: the login page, Keycloak's own form, and back. It changes the installation's provider and
// alice's role there, which nobody else's spec signs in through, and runs alone.
test.describe("single sign-on", () => {
  test.use({ storageState: ANONYMOUS });

  test("alice signs in through Keycloak and lands in the installation as the administrator she is", async ({ page }) => {
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
    await signIn(page, "alice");
    await expect.poll(() => standing(page)).toEqual({ role: "admin" });
    await page.goto(MEMBERS_PATH);
    await expect(page.locator("[data-members]")).toBeVisible();
    await openAccountMenu(page);
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
    const context = await anonymousContext(browser);
    try {
      must(await owner.PATCH("/users/{userID}", { params: { path: { userID: aliceId } }, body: { role: "member" } }));
      mappingId = must(await owner.POST("/oidc-provider/group-roles", { body: { group: ADMINS_GROUP, role: "admin" } })).groupRole.id;

      const page = await context.newPage();
      await signIn(page, "alice");
      await expect.poll(() => standing(page)).toEqual({ role: "admin" });
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

test.describe("the provider and its groups from the settings page", { tag: "@desktop" }, () => {
  test("alice changes the groups claim, and the secret kept still signs bob in", async ({ page, browser }) => {
    const owner = await ownerApi();
    const before = must(await owner.GET("/oidc-provider")).provider;
    expect(before, "the seed configured the provider").toBeTruthy();
    const { issuer, clientId, groupsClaim, scopes, createGroups, enabled } = before!;
    try {
      await page.goto(SSO_PATH);
      const claim = page.getByLabel("Groups claim", { exact: true });
      await expect(claim).toHaveValue(groupsClaim);
      await claim.fill(OTHER_CLAIM);
      await page.locator('[data-action="save-sso"]').click();
      await expect(page.locator('[data-action="save-sso"] + [role="status"]')).toHaveText("Saved.");

      await page.reload();
      await expect(claim).toHaveValue(OTHER_CLAIM);
      const secret = page.getByLabel("Client secret", { exact: true });
      await expect(secret).toHaveValue("");
      await expect(secret).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
      expect(must(await owner.GET("/oidc-provider")).provider).toMatchObject({ groupsClaim: OTHER_CLAIM, hasSecret: true });

      const bob = await signInFresh(browser, USERS.bob.username, USERS.bob.password);
      try {
        await expect.poll(() => standing(bob.page)).toEqual({ role: "member" });
      } finally {
        await bob.close();
      }
    } finally {
      must(await owner.PUT("/oidc-provider", { body: { issuer, clientId, groupsClaim, scopes, createGroups, enabled } }));
      // bob's sign-in read no groups through the other claim; one through the real claim gives them back.
      const bob = await signInFresh(browser, USERS.bob.username, USERS.bob.password);
      await bob.close();
      await owner.dispose();
    }
  });

  test("the owner maps alice's group to Administrator and unmaps it, and her next sign-ins follow", async ({ browser }) => {
    const owner = await ownerApi();
    const alice = must(await owner.GET("/users")).members.find((each) => each.email === USERS.alice.email);
    expect(alice, "the seed let alice in").toBeDefined();
    const aliceId = alice!.userId;
    const settings = await freshPage(browser);
    try {
      must(await owner.PATCH("/users/{userID}", { params: { path: { userID: aliceId } }, body: { role: "member" } }));
      await signInWithPassword(settings.page);
      await settings.page.goto(SSO_PATH);
      const card = settings.page.locator("[data-group-roles]");
      await card.getByLabel("Group", { exact: true }).fill(ADMINS_GROUP);
      await card.getByLabel("Role", { exact: true }).selectOption("admin");
      await card.locator('[data-action="map-group"]').click();
      await expect(toast(settings.page, `${ADMINS_GROUP} now grants Administrator.`)).toBeVisible();
      await dismissToasts(settings.page);
      await expect(card.locator(`[data-group-role="${ADMINS_GROUP}"]`)).toContainText("Administrator");

      const promoted = await signInFresh(browser, USERS.alice.username, USERS.alice.password);
      try {
        await expect.poll(() => standing(promoted.page)).toEqual({ role: "admin" });
        await openUntil(promoted.page, SETTINGS_PATH, () => expect(promoted.page.locator('[data-hub-card="settings"]')).toBeVisible(ONE_LOOK));
      } finally {
        await promoted.close();
      }

      await card.getByRole("button", { name: `Unmap ${ADMINS_GROUP}` }).click();
      await expect(toast(settings.page, `${ADMINS_GROUP} no longer grants a role.`)).toBeVisible();
      await dismissToasts(settings.page);
      await expect(card.locator(`[data-group-role="${ADMINS_GROUP}"]`)).toHaveCount(0);

      const demoted = await signInFresh(browser, USERS.alice.username, USERS.alice.password);
      try {
        await expect.poll(() => standing(demoted.page)).toEqual({ role: "member" });
        await openUntil(demoted.page, SETTINGS_PATH, async () => {
          await expect(demoted.page.locator('[data-hub-card="you"]')).toBeVisible(ONE_LOOK);
          await expect(demoted.page.locator('[data-hub-card="settings"]')).toHaveCount(0, ONE_LOOK);
        });
      } finally {
        await demoted.close();
      }
    } finally {
      await settings.close();
      const left = must(await owner.GET("/oidc-provider/group-roles")).groupRoles.find((each) => each.group === ADMINS_GROUP);
      if (left) await owner.DELETE("/oidc-provider/group-roles/{groupRoleID}", { params: { path: { groupRoleID: left.id } } });
      await owner.PATCH("/users/{userID}", { params: { path: { userID: aliceId } }, body: { role: "admin" } });
      await owner.dispose();
    }
  });
});

test.describe("people waiting to be let in", { tag: "@desktop" }, () => {
  test.beforeEach(forgetStrangers);
  test.afterEach(forgetStrangers);

  test("carol waits, is let in as a member and signs in; dave is turned away and still waits", async ({ browser }) => {
    for (const stranger of [CAROL, DAVE]) {
      const first = await signInFresh(browser, stranger.username, stranger.password);
      try {
        await expect(first.page).toHaveURL(new RegExp(`${LOGIN_PATH}\\?sso=not_a_member`));
        await expect(first.page.getByRole("heading", { name: WAITING })).toBeVisible();
        expect((await first.page.request.get(ME_PATH)).status()).toBe(401);
      } finally {
        await first.close();
      }
    }

    const admin = await freshPage(browser);
    try {
      await signInWithPassword(admin.page);
      const requests = admin.page.locator("[data-join-requests]");
      await openUntil(admin.page, MEMBERS_PATH, () => expect(requests.locator(`[data-join-request="${DAVE.email}"]`)).toBeVisible(ONE_LOOK));
      await requests.locator(`[data-join-request="${CAROL.email}"] [data-action="admit-request"]`).click();
      await expect(toast(admin.page, /may sign in now\.$/)).toBeVisible();
      await dismissToasts(admin.page);
      await expect(requests.locator(`[data-join-request="${CAROL.email}"]`)).toHaveCount(0);
      await requests.locator(`[data-join-request="${DAVE.email}"] [data-action="decline-request"]`).click();
      await expect(toast(admin.page, /was turned away\.$/)).toBeVisible();
      await dismissToasts(admin.page);
      await expect(requests.locator(`[data-join-request="${DAVE.email}"]`)).toHaveCount(0);
    } finally {
      await admin.close();
    }

    const carol = await signInFresh(browser, CAROL.username, CAROL.password);
    try {
      await expect.poll(() => standing(carol.page)).toEqual({ role: "member" });
      await carol.page.goto("/");
      await expect(carol.page.locator("[data-home]")).toBeVisible();
    } finally {
      await carol.close();
    }
    const dave = await signInFresh(browser, DAVE.username, DAVE.password);
    try {
      await expect(dave.page).toHaveURL(new RegExp(`${LOGIN_PATH}\\?sso=not_a_member`));
      await expect(dave.page.getByRole("heading", { name: WAITING })).toBeVisible();
      expect((await dave.page.request.get(ME_PATH)).status()).toBe(401);
    } finally {
      await dave.close();
    }
  });
});

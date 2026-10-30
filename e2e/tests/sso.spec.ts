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
import type { ExciterApi } from "../fixtures/api";
import { findGrants, grantEverywhere, grantInForm, grantRow, revokeEverywhere, revokeInTable } from "../fixtures/grants";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { openAccountMenu, toast, dismissToasts } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// alice's group at the stack's Keycloak, from deploy/keycloak/realm.json.
const ADMINS_GROUP = "exciter-administrators";
const ADMINISTRATOR = "administrator";
const MEMBERS_PATH = "/admin/access/members";
const SSO_PATH = "/admin/access/sso";
const ROLES_PATH = "/admin/access/roles";
const GROUPS_PATH = "/admin/access/groups";
const USERS_PATH = "/admin/users";
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
/** The group the provider fills from alice's group, made here when no sign-in has made it yet. */
async function adminsGroup(owner: ExciterApi): Promise<{ id: string; name: string }> {
  const { groups } = must(await owner.GET("/groups", { params: { query: { q: ADMINS_GROUP, limit: 50 } } }));
  const found = groups.find((each) => each.externalRef === ADMINS_GROUP);
  if (found) return { id: found.id, name: found.name };
  const made = must(await owner.POST("/groups", { body: { name: ADMINS_GROUP, externalRef: ADMINS_GROUP } })).group;
  return { id: made.id, name: made.name };
}

/** Deletes every group named for alice's provider group or carrying its claim, with their grants. */
async function dropAdminsGroup(owner: ExciterApi): Promise<void> {
  const { groups } = must(await owner.GET("/groups", { params: { query: { q: ADMINS_GROUP, limit: 50 } } }));
  for (const group of groups.filter((each) => each.externalRef === ADMINS_GROUP || each.name === ADMINS_GROUP)) {
    await owner.DELETE("/groups/{groupID}", { params: { path: { groupID: group.id } } });
  }
}

const UNREACHABLE = "Exciter could not reach the identity provider. Try again in a moment, and tell an administrator if it keeps failing.";

// The sign-in a browser alone can walk: the login page, Keycloak's own form, and back. It changes the installation's provider and
// alice's role there, which nobody else's spec signs in through, and runs alone.
test.describe("single sign-on", () => {
  test.use({ storageState: ANONYMOUS });

  test("alice signs in through Keycloak and lands in the installation as the administrator she is", async ({ page }) => {
    expect((await page.request.get(ME_PATH)).status()).toBe(401);
    await signIn(page, "alice");
    await expect.poll(() => standing(page)).toEqual({ role: "member", administers: true });
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

test.describe("grants through the provider's groups", { tag: "@desktop" }, () => {
  test("alice, her own administrator grant taken, signs in and administers through her group's", async ({ browser }) => {
    const owner = await ownerApi();
    const alice = must(await owner.GET("/users")).members.find((each) => each.email === USERS.alice.email);
    expect(alice, "the seed let alice in").toBeDefined();
    const aliceId = alice!.userId;
    const context = await anonymousContext(browser);
    let groupId: string | undefined;
    try {
      const group = await adminsGroup(owner);
      groupId = group.id;
      await grantEverywhere(owner, ADMINISTRATOR, { groupId });
      await revokeEverywhere(owner, ADMINISTRATOR, { userId: aliceId });

      const page = await context.newPage();
      await signIn(page, "alice");
      await expect.poll(() => standing(page)).toEqual({ role: "member", administers: true });
      await openUntil(page, MEMBERS_PATH, async () => {
        const row = page.locator(`[data-member="${USERS.alice.email}"]`);
        await expect(row.locator(`[data-member-holds="${ADMINISTRATOR}"]`).filter({ hasText: `through ${group.name}` })).toBeVisible(ONE_LOOK);
        await expect(row.locator("[data-member-role]")).toHaveAttribute("data-member-role", "member", ONE_LOOK);
      });
    } finally {
      await context.close();
      await grantEverywhere(owner, ADMINISTRATOR, { userId: aliceId });
      if (groupId) await revokeEverywhere(owner, ADMINISTRATOR, { groupId });
      await owner.dispose();
    }
  });
});

test.describe("the provider and its groups from the settings page", { tag: "@desktop" }, () => {
  test("alice changes the groups claim, and the secret kept still signs bob in", async ({ page, browser }) => {
    const owner = await ownerApi();
    const before = must(await owner.GET("/oidc-provider")).provider;
    expect(before, "the seed configured the provider").toBeTruthy();
    const { issuer, clientId, groupsClaim, scopes, createGroups, admitByGroup, enabled } = before!;
    try {
      await page.goto(SSO_PATH);
      const claim = page.getByLabel("Groups claim", { exact: true });
      await expect(claim).toHaveValue(groupsClaim);
      await claim.fill(OTHER_CLAIM);
      await page.locator('[data-action="save-sso"]').click();
      await expect(toast(page, "Single sign-on is saved.")).toBeVisible();
      await dismissToasts(page);
      await expect(page.getByLabel("Scopes")).toHaveCount(0);
      await expect(page.locator('[data-sso-state="on"] [data-sso-start]')).toHaveText(`${WEB_URL}/api/v1/auth/oidc/start`);
      expect(must(await owner.GET("/oidc-provider")).provider).toMatchObject({ scopes, createGroups });

      await page.reload();
      await expect(claim).toHaveValue(OTHER_CLAIM);
      const secret = page.getByLabel("Client secret", { exact: true });
      await expect(secret).toHaveValue("");
      await expect(secret).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
      expect(must(await owner.GET("/oidc-provider")).provider).toMatchObject({ groupsClaim: OTHER_CLAIM, hasSecret: true });

      const bob = await signInFresh(browser, USERS.bob.username, USERS.bob.password);
      try {
        await expect.poll(() => standing(bob.page)).toEqual({ role: "member", administers: false });
      } finally {
        await bob.close();
      }
    } finally {
      must(await owner.PUT("/oidc-provider", { body: { issuer, clientId, groupsClaim, scopes, createGroups, admitByGroup, enabled } }));
      // bob's sign-in read no groups through the other claim; one through the real claim gives them back.
      const bob = await signInFresh(browser, USERS.bob.username, USERS.bob.password);
      await bob.close();
      await owner.dispose();
    }
  });

  test("the owner makes alice's provider group, grants it Administrator and revokes it, and her next sign-ins follow", async ({ browser }) => {
    const owner = await ownerApi();
    const alice = must(await owner.GET("/users")).members.find((each) => each.email === USERS.alice.email);
    expect(alice, "the seed let alice in").toBeDefined();
    const aliceId = alice!.userId;
    const settings = await freshPage(browser);
    try {
      await dropAdminsGroup(owner);
      await revokeEverywhere(owner, ADMINISTRATOR, { userId: aliceId });
      await signInWithPassword(settings.page);
      await settings.page.goto(GROUPS_PATH);
      await settings.page.getByRole("textbox", { name: "New group" }).fill(ADMINS_GROUP);
      await settings.page.getByRole("textbox", { name: "Provider claim" }).fill(ADMINS_GROUP);
      await settings.page.locator('[data-action="create-group"]').click();
      await expect(toast(settings.page, `${ADMINS_GROUP} was made.`)).toBeVisible();
      await dismissToasts(settings.page);
      await expect(settings.page.locator(`[data-group="${ADMINS_GROUP}"]`)).toContainText("From the identity provider");

      await settings.page.goto(ROLES_PATH);
      await grantInForm(settings.page, ADMINS_GROUP, ADMINISTRATOR);
      await expect(toast(settings.page, `${ADMINS_GROUP} holds Administrator now.`)).toBeVisible();
      await dismissToasts(settings.page);
      await findGrants(settings.page, ADMINS_GROUP);
      await expect(grantRow(settings.page, ADMINS_GROUP, ADMINISTRATOR)).toBeVisible();

      const promoted = await signInFresh(browser, USERS.alice.username, USERS.alice.password);
      try {
        await expect.poll(() => standing(promoted.page)).toEqual({ role: "member", administers: true });
        await openUntil(promoted.page, "/", () => expect(promoted.page.locator('[data-sidebar] [data-nav="admin"]')).toBeVisible(ONE_LOOK));
      } finally {
        await promoted.close();
      }

      await revokeInTable(settings.page, ADMINS_GROUP, ADMINISTRATOR);
      await expect(toast(settings.page, `${ADMINS_GROUP} no longer holds Administrator.`)).toBeVisible();
      await dismissToasts(settings.page);
      await expect(grantRow(settings.page, ADMINS_GROUP, ADMINISTRATOR)).toHaveCount(0);

      const demoted = await signInFresh(browser, USERS.alice.username, USERS.alice.password);
      try {
        await expect.poll(() => standing(demoted.page)).toEqual({ role: "member", administers: false });
        await openUntil(demoted.page, "/", async () => {
          await expect(demoted.page.locator("[data-sidebar-org]")).toBeVisible(ONE_LOOK);
          await expect(demoted.page.locator('[data-sidebar] [data-nav="admin"]')).toHaveCount(0, ONE_LOOK);
        });
      } finally {
        await demoted.close();
      }
    } finally {
      await settings.close();
      await grantEverywhere(owner, ADMINISTRATOR, { userId: aliceId });
      const group = await adminsGroup(owner);
      await revokeEverywhere(owner, ADMINISTRATOR, { groupId: group.id });
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
      await openUntil(admin.page, USERS_PATH, () => expect(requests.locator(`[data-join-request="${DAVE.email}"]`)).toBeVisible(ONE_LOOK));
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
      await expect.poll(() => standing(carol.page)).toEqual({ role: "member", administers: false });
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

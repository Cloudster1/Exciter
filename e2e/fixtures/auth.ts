import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect, request, type Browser, type Page } from "@playwright/test";
import { createExciterApi, type ExciterApi, type Session } from "./api";
import { KEYCLOAK_URL, WEB_URL } from "./stack";

// Everything about signing in lives in this file, so when the login page
// changes this is the one place to follow it.

export type User = "alice" | "bob";

/** The realm's test users, from deploy/keycloak/realm.json. */
export const USERS: Record<User, { username: string; password: string; email: string; name: string }> = {
  alice: { username: "alice", password: "alice password", email: "alice@suite.test", name: "Alice Admin" },
  bob: { username: "bob", password: "bob password", email: "bob@suite.test", name: "Bob Builder" },
};

/** The owner the seed bootstraps, who signs in with a password and makes each spec's people. */
export const BOOTSTRAP_OWNER = { email: "admin@exciter.test", password: "exciter admin password" };

/** What the login page offers, and what answers once someone is signed in. */
export const LOGIN_PATH = "/login";
export const ME_PATH = "/api/v1/auth/me";
const SSO_START = '[data-action="sso"]';
const PASSWORD_SUBMIT = '[data-action="password-sign-in"]';

/** A browser nobody has signed in to. */
export const ANONYMOUS = { cookies: [], origins: [] };

// A click that starts a navigation waits for it within the action timeout.
// The sign-in hops outlast that on a busy machine, so the next step waits instead.
const LEAVES_THE_PAGE = { noWaitAfter: true } as const;

const AUTH_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "../.auth");

/** Where a user's signed-in browser state is kept between the setup and the specs. */
export function stateFile(user: User): string {
  return resolve(AUTH_DIR, `${user}.json`);
}

/** Starts SSO from the login page, staying on it when a redirect brought the browser there so its `next` is
 *  kept; the caller waits for the page the provider leads to. */
export async function startSSO(page: Page): Promise<void> {
  if (!page.url().startsWith(`${WEB_URL}${LOGIN_PATH}`)) await page.goto(LOGIN_PATH);
  await page.locator(SSO_START).click(LEAVES_THE_PAGE);
}

/** Fills Keycloak's own form, then waits until the browser is back in Exciter. */
export async function submitKeycloak(page: Page, username: string, password: string): Promise<void> {
  await page.waitForURL(`${KEYCLOAK_URL}/**`);
  await page.locator("#username").fill(username);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click(LEAVES_THE_PAGE);
  await page.waitForURL((url) => url.origin === new URL(WEB_URL).origin && !url.pathname.startsWith("/api/"));
}

/** Signs in through the login page and Keycloak's form, and waits for the session. */
export async function signIn(page: Page, user: User): Promise<void> {
  await startSSO(page);
  await submitKeycloak(page, USERS[user].username, USERS[user].password);
  await expect.poll(async () => (await page.request.get(ME_PATH)).status()).toBe(200);
}

/** Signs the bootstrap owner in with the password form on the login page. */
export async function signInWithPassword(page: Page, email = BOOTSTRAP_OWNER.email, password = BOOTSTRAP_OWNER.password): Promise<void> {
  await page.goto(LOGIN_PATH);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.locator(PASSWORD_SUBMIT).click();
  await expect.poll(async () => (await page.request.get(ME_PATH)).status()).toBe(200);
}

/** Signs the user in to the installation and stores the session for the specs. */
export async function saveSession(browser: Browser, user: User): Promise<void> {
  mkdirSync(AUTH_DIR, { recursive: true });
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
  try {
    await signIn(await context.newPage(), user);
    await context.storageState({ path: stateFile(user) });
  } finally {
    await context.close();
  }
}

/** The API as the bootstrap owner, signed in with the password. */
export async function ownerApi(): Promise<ExciterApi> {
  const context = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Origin: WEB_URL } });
  try {
    const login = await context.post("/api/v1/auth/login", { data: BOOTSTRAP_OWNER });
    if (!login.ok()) throw new Error(`The bootstrap owner could not sign in: ${login.status()} ${await login.text()}`);
    return await createExciterApi(await context.storageState());
  } finally {
    await context.dispose();
  }
}

/** Where the page's session acts now, and with which role. */
export async function standing(page: Page): Promise<{ role?: string }> {
  const me = await page.request.get(ME_PATH);
  if (!me.ok()) return {};
  return { role: ((await me.json()) as { role?: string }).role };
}

interface SessionFixtures {
  /** Where each user's session comes from: the setup's, or the people a spec made for itself. */
  sessionOf: (user: User) => Session;
  /** A page of its own, signed in as the user, beside the default page as alice. */
  pageAs: (user: User) => Promise<Page>;
  /** The API as alice. */
  api: ExciterApi;
  /** The API as the user. */
  apiAs: (user: User) => Promise<ExciterApi>;
}

/** The test for the shell: alice's page in the installation, with bob's and the API beside it. */
export const test = base.extend<SessionFixtures>({
  sessionOf: async ({}, use) => {
    await use(stateFile);
  },
  storageState: async ({ sessionOf }, use) => {
    await use(sessionOf("alice"));
  },
  pageAs: async ({ browser, viewport, hasTouch, isMobile, sessionOf }, use) => {
    const opened: Array<{ close: () => Promise<void> }> = [];
    await use(async (user) => {
      const context = await browser.newContext({ baseURL: WEB_URL, storageState: sessionOf(user), viewport, hasTouch, isMobile });
      opened.push(context);
      return context.newPage();
    });
    for (const context of opened) await context.close();
  },
  api: async ({ sessionOf }, use) => {
    const api = await createExciterApi(sessionOf("alice"));
    await use(api);
    await api.dispose();
  },
  apiAs: async ({ sessionOf }, use) => {
    const opened: ExciterApi[] = [];
    await use(async (user) => {
      const api = await createExciterApi(sessionOf(user));
      opened.push(api);
      return api;
    });
    for (const api of opened) await api.dispose();
  },
});

export { expect };

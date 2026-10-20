import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";

// The suite end to end (#31): alice signs in to all three products at the one Keycloak, traces a requirement to an
// Armature issue, moves the issue on in Armature, and Exciter's chip follows through Armature's webhook.

const SUITE_ENV_FILE = resolve(dirname(fileURLToPath(import.meta.url)), "../../.cache/suite.env");

function suiteEnv(): Record<string, string> {
  if (!existsSync(SUITE_ENV_FILE)) throw new Error(`${SUITE_ENV_FILE} is missing. Start the suite with make suite-up first.`);
  const out: Record<string, string> = {};
  for (const line of readFileSync(SUITE_ENV_FILE, "utf8").split("\n")) {
    const match = /^([A-Z0-9_]+)=(.*)$/.exec(line.trim());
    if (match) out[match[1]!] = match[2]!;
  }
  return out;
}

const env = suiteEnv();
const EXCITER = env.SUITE_EXCITER_URL!;
const ARMATURE = env.SUITE_ARMATURE_URL!;
const STATOR = env.SUITE_STATOR_URL!;
const TOKEN = env.SUITE_ALICE_ARMATURE_TOKEN!;
const ALICE = { username: "alice", password: "alice password" };
// How long a chip may take to follow Armature: the webhook, then the replica replaying what it wrote.
const FOLLOW_TIMEOUT_MS = 30_000;

/** Armature's API as alice, with the token the suite made her. */
async function armature<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${ARMATURE}/api/v1${path}`, {
    method,
    headers: { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`Armature answered ${method} ${path} with ${res.status}: ${await res.text()}`);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

/** Signs in through the suite's Keycloak, whose form shows only the first time: after that it is single sign-on. */
async function signIn(page: Page, origin: string) {
  await page.goto(`${origin}/api/v1/auth/oidc/start`);
  const form = page.locator("#kc-form-login");
  if (await form.isVisible()) {
    await page.locator("#username").fill(ALICE.username);
    await page.locator("#password").fill(ALICE.password);
    await page.locator("#kc-login").click();
  }
  await page.waitForURL((url) => url.origin === origin);
  const me = await page.request.get(`${origin}/api/v1/auth/me`);
  expect(me.ok(), `alice is signed in to ${origin}`).toBe(true);
}

test("alice signs in to all three, and a requirement follows its issue in Armature", async ({ page }) => {
  await signIn(page, EXCITER);
  await signIn(page, ARMATURE);
  await signIn(page, STATOR);

  const key = `SMK${Date.now().toString(36).toUpperCase().slice(-5)}`;
  const api = (path: string) => `${EXCITER}/api/v1${path}`;
  expect((await page.request.put(api("/armature/account/token"), { data: { token: TOKEN } })).ok()).toBe(true);
  expect((await page.request.post(api("/projects"), { data: { key, name: "Suite smoke" } })).ok()).toBe(true);
  expect((await page.request.post(api(`/projects/${key}/requirements`), { data: { title: "Brakes engage within 100 ms" } })).ok()).toBe(true);

  const { issues } = await armature<{ issues: { key: string }[] }>("GET", "/issues?limit=1&text=");
  const issue = issues[0]!.key;
  expect((await page.request.post(api(`/requirements/${key}-R1/armature-links`), { data: { issueKey: issue, relation: "implements" } })).ok()).toBe(true);

  await page.goto(`${EXCITER}/p/${key}/r/${key}-R1`);
  const chip = page.locator(`[data-armature-issue="${issue}"]`);
  await expect(chip).toBeVisible();

  const before = await armature<{ issue: { status: { name: string } } }>("GET", `/issues/${issue}`);
  await expect(chip).toContainText(before.issue.status.name);

  // Any transition Armature lets alice take without asking for more moves the issue on.
  const { transitions } = await armature<{ transitions: { id: string }[] }>("GET", `/issues/${issue}/transitions`);
  let moved = false;
  for (const t of transitions) {
    try {
      await armature("POST", `/issues/${issue}/transitions`, { transitionId: t.id });
      moved = true;
      break;
    } catch {
      // A transition that wants a resolution or a comment is passed over.
    }
  }
  expect(moved, `Armature let alice move ${issue} on`).toBe(true);
  const now = await armature<{ issue: { status: { name: string } } }>("GET", `/issues/${issue}`);
  expect(now.issue.status.name, "the transition changed the status").not.toBe(before.issue.status.name);

  // The chip was read before the move and is cached for a minute, so only the webhook clears it within the wait.
  await expect(async () => {
    await page.reload();
    await expect(chip).toContainText(now.issue.status.name, { timeout: 1_000 });
  }).toPass({ timeout: FOLLOW_TIMEOUT_MS });

  await page.request.put(api(`/projects/${key}/archive`));
  await page.request.delete(api(`/projects/${key}`));
});

// Connecting at sign-in (#274): alice, holding no token of her own in Exciter, signs in at the one Keycloak and
// links a requirement to an Armature issue and a Stator page; the suite's seed registered Exciter as a connected
// app of both. Stator's part waits for Stator to offer the exchange (praetorianer777/Stator#461).
test("alice signs in to Exciter and links an issue and a page without connecting either", async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  const api = (path: string) => `${EXCITER}/api/v1${path}`;
  await signIn(page, EXCITER);
  // A token the person pasted stays at sign-in, so the first test's goes, and the next sign-in exchanges hers.
  await page.request.delete(api("/armature/account/token"));
  await page.request.delete(api("/stator/account/token"));
  await context.clearCookies();
  await signIn(page, EXCITER);

  const account = (await (await page.request.get(api("/armature/account"))).json()) as { account: { source: string } };
  expect(account.account.source, "alice is connected to Armature by her sign-in").toBe("sign_in");

  const key = `SXC${Date.now().toString(36).toUpperCase().slice(-5)}`;
  expect((await page.request.post(api("/projects"), { data: { key, name: "Connected at sign-in" } })).ok()).toBe(true);
  expect((await page.request.post(api(`/projects/${key}/requirements`), { data: { title: "Brakes engage within 100 ms" } })).ok()).toBe(true);
  const { issues } = await armature<{ issues: { key: string }[] }>("GET", "/issues?limit=1&text=");
  const issue = issues[0]!.key;
  const linked = await page.request.post(api(`/requirements/${key}-R1/armature-links`), { data: { issueKey: issue, relation: "implements" } });
  expect(linked.ok(), `alice links ${issue} with the token her sign-in exchanged`).toBe(true);

  const stator = (await (await page.request.get(api("/stator/account"))).json()) as { account: { source: string; signIn: { message: string } | null } };
  if (stator.account.source === "sign_in") {
    const found = (await (await page.request.get(api("/stator/search?q=a"))).json()) as { pages?: { id: string }[] };
    const first = found.pages?.[0];
    expect(first, "Stator shows alice a page to link").toBeDefined();
    const pageLink = await page.request.post(api(`/requirements/${key}-R1/stator-links`), { data: { pageId: first!.id, relation: "describes" } });
    expect(pageLink.ok(), "alice links a Stator page with the token her sign-in exchanged").toBe(true);
  } else {
    test
      .info()
      .annotations.push({ type: "skipped", description: `Stator part: ${stator.account.signIn?.message ?? "Stator does not offer the exchange yet."}` });
  }

  await page.request.put(api(`/projects/${key}/archive`));
  await page.request.delete(api(`/projects/${key}`));
  await context.close();
});

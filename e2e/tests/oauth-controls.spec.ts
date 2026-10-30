import { createHash, randomBytes } from "node:crypto";
import { request, type Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey, uniqueName } from "../fixtures/seed";
import { expectAccessible, confirm, toast, dismissToasts } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// The OAuth controls no other spec clicks (#329): consent narrowed to a project, an administrator disconnecting
// somebody's app on All connected apps, and a partner app's secret copied and rotated.

// Nothing listens there; the browser is stopped on its way back and answered by the test instead.
const CALLBACK = "http://127.0.0.1:9/callback";
const STATE = "the-state";
// web/src/config.ts SETTINGS_PATH and INSTALLATION_PATH, and the prefix of a partner's secret, backend/internal/oauth.
const SETTINGS_PATH = "/settings";
const INSTALLATION_PATH = "/settings/installation";
const SECRET_PREFIX = "exciter_ocs_";
// A refresh token nobody holds: the token endpoint reaches the grant, and refuses it, only past the client's secret.
const NOBODYS_REFRESH = "exciter_ort_nobody";

const KEY = uniqueKey("OCON");
const OTHER = uniqueKey("OCOT");

/** A client of this spec's own, put straight into the database, so registering's count by address is never met. */
async function aClient(name: string): Promise<string> {
  const id = `e2e-${randomBytes(8).toString("hex")}`;
  await withDatabase((db) =>
    db.query(`INSERT INTO oauth_client (id, kind, name, redirect_uris, grant_types) VALUES ($1, 'registered', $2, ARRAY[$3], ARRAY['authorization_code'])`, [
      id,
      name,
      CALLBACK,
    ]),
  );
  return id;
}

/** Begins connecting as the client would, and answers the consent page's request with the verifier to trade it. */
async function askToConnect(client: string): Promise<{ pending: string; verifier: string }> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const resource = ((await (await anyone.get("/.well-known/oauth-protected-resource")).json()) as { resource: string }).resource;
    const verifier = randomBytes(32).toString("base64url");
    const query = new URLSearchParams({
      response_type: "code",
      client_id: client,
      redirect_uri: CALLBACK,
      state: STATE,
      code_challenge: createHash("sha256").update(verifier).digest("base64url"),
      code_challenge_method: "S256",
      resource,
      scope: "mcp",
    });
    const sent = await anyone.get(`/api/v1/oauth/authorize?${query}`, { maxRedirects: 0 });
    expect(sent.status()).toBe(302);
    const pending = new URL(sent.headers()["location"]!).searchParams.get("request");
    expect(pending).toBeTruthy();
    return { pending: pending!, verifier };
  } finally {
    await anyone.dispose();
  }
}

/** Allows the client on the person's consent page, optionally narrowed to one project, and answers its access token. */
async function connect(page: Page, client: string, project?: string): Promise<string> {
  const { pending, verifier } = await askToConnect(client);
  await page.route(`${CALLBACK}**`, (route) => route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Back in the app</title>" }));
  await page.goto(`/oauth/consent?request=${pending}`);
  if (project) {
    const chip = page.locator(`[data-consent] [data-project="${project}"]`);
    await chip.click();
    await expect(chip).toHaveAttribute("aria-pressed", "true");
  }
  await page.locator('[data-action="allow-app"]').click();
  await page.waitForURL((url) => url.href.startsWith(CALLBACK));
  const code = new URL(page.url()).searchParams.get("code")!;
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const traded = await anyone.post("/api/v1/oauth/token", {
      form: { grant_type: "authorization_code", code, redirect_uri: CALLBACK, code_verifier: verifier, client_id: client },
    });
    expect(traded.status()).toBe(200);
    return ((await traded.json()) as { access_token: string }).access_token;
  } finally {
    await anyone.dispose();
  }
}

/** Calls MCP with an access token, and answers the HTTP status and the JSON-RPC result. */
async function mcp(token: string, method: string, params?: object): Promise<{ status: number; result?: Record<string, unknown> }> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const answer = await anyone.post("/api/v1/mcp", {
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      data: { jsonrpc: "2.0", id: 1, method, params },
    });
    return { status: answer.status(), result: answer.ok() ? ((await answer.json()) as { result: Record<string, unknown> }).result : undefined };
  } finally {
    await anyone.dispose();
  }
}

/** Posts to the token endpoint as a partner app with its secret, and answers the status and the RFC error. */
async function asPartner(id: string, secret: string): Promise<{ status: number; error: string }> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const basic = Buffer.from(`${encodeURIComponent(id)}:${encodeURIComponent(secret)}`).toString("base64");
    const answer = await anyone.post("/api/v1/oauth/token", {
      headers: { Authorization: `Basic ${basic}` },
      form: { grant_type: "refresh_token", refresh_token: NOBODYS_REFRESH },
    });
    return { status: answer.status(), error: ((await answer.json()) as { error: string }).error };
  } finally {
    await anyone.dispose();
  }
}

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("OAuth controls", { tag: "@desktop" }, () => {
  const clients: string[] = [];
  const partners: string[] = [];
  test.afterEach(async ({ api }) => {
    // A client's grants go with it.
    await withDatabase((db) => db.query("DELETE FROM oauth_client WHERE id = ANY($1)", [clients.splice(0)]));
    for (const id of partners.splice(0)) await api.DELETE("/oauth/clients/{clientID}", { params: { path: { clientID: id } } });
    await removeProject(api, KEY);
    await removeProject(api, OTHER);
  });

  test("alice connects an app to one project alone, and its token lists that project and no other", async ({ page, api }, testInfo) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Connected" } }));
    must(await api.POST("/projects", { body: { key: OTHER, name: "Left out" } }));
    const client = await aClient(uniqueName(testInfo, "Narrow assistant"));
    clients.push(client);

    const token = await connect(page, client, KEY);
    const called = await mcp(token, "tools/call", { name: "list_projects", arguments: {} });
    expect(called.status).toBe(200);
    const keys = ((called.result?.structuredContent as { projects: { key: string }[] }).projects ?? []).map((each) => each.key);
    expect(keys).toEqual([KEY]);
  });

  test("alice disconnects bob's app on All connected apps, and its token is refused", async ({ page, pageAs, people }, testInfo) => {
    const name = uniqueName(testInfo, "Bob's assistant");
    const client = await aClient(name);
    clients.push(client);
    const token = await connect(await pageAs("bob"), client);
    expect((await mcp(token, "tools/list")).status).toBe(200);

    await page.goto(SETTINGS_PATH);
    await page.locator('[data-hub-card="settings"]').getByRole("link", { name: "All connected apps" }).click();
    await expect(page).toHaveURL(/\/settings\/all-connected-apps$/);
    const row = page.locator(`[data-app-row="${name}"]`);
    await expect(row).toHaveAttribute("data-app-owner", people.bob.email);
    await expectAccessible(page);

    await row.getByRole("button", { name: `Disconnect ${name} of ${people.bob.name}` }).click();
    await expect(page.locator("[data-confirm]")).toContainText(`Disconnect app ${name}?`);
    await expect(page.locator("[data-confirm]")).toContainText("It is refused from now on, until it is connected again.");
    await confirm(page);
    await expect(toast(page, `${name} was disconnected.`)).toHaveText(`${name} was disconnected.`);
    await dismissToasts(page);
    await expect(row).toHaveCount(0);
    await expect.poll(async () => (await mcp(token, "tools/list")).status).toBe(401);
  });

  test("alice copies a partner app's new secret, shown once, and the old one is refused", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "Rotated wiki");
    const made = must(await api.POST("/oauth/clients", { body: { name, keycloakClientId: "stator" } })).client;
    partners.push(made.id);
    const old = made.secret!;
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);

    await page.goto(INSTALLATION_PATH);
    const row = page.locator(`[data-partner-row="${name}"]`);
    await row.locator('[data-action="rotate-partner-secret"]').click();
    await expect(page.locator("[data-confirm]")).toContainText(`The old one stops at once, so enter the new one in ${name} straight away.`);
    await confirm(page);
    const field = page.getByLabel(`Secret of ${name}`, { exact: true });
    await expect(field).toHaveValue(new RegExp(`^${SECRET_PREFIX}`));
    const fresh = await field.inputValue();
    expect(fresh).not.toBe(old);
    await expectAccessible(page);

    await page.locator('[data-action="copy-partner-secret"]').click();
    await expect(page.locator("[data-partner-secret]").getByRole("status")).toHaveText("Copied the secret.");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(fresh);
    await page.locator('[data-action="partner-secret-done"]').click();
    await expect(field).toHaveCount(0);
    await page.reload();
    await expect(row).toBeVisible();
    expect(await page.content()).not.toContain(fresh);

    expect(await asPartner(made.id, old)).toEqual({ status: 401, error: "invalid_client" });
    expect(await asPartner(made.id, fresh)).toEqual({ status: 400, error: "invalid_grant" });
  });
});

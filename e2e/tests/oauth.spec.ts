import { createHash, randomBytes } from "node:crypto";
import { request } from "@playwright/test";
import { expect, test } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// Nothing listens there; the browser is stopped on its way back and answered by the test instead.
const CALLBACK = "http://127.0.0.1:9/callback";
const STATE = "the-state";

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

/** Begins connecting as the client would, and answers the request the consent page is sent with. */
async function askToConnect(client: string, scope = "mcp"): Promise<string> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const resource = ((await (await anyone.get("/.well-known/oauth-protected-resource")).json()) as { resource: string }).resource;
    const challenge = createHash("sha256").update(randomBytes(32).toString("base64url")).digest("base64url");
    const query = new URLSearchParams({
      response_type: "code",
      client_id: client,
      redirect_uri: CALLBACK,
      state: STATE,
      code_challenge: challenge,
      code_challenge_method: "S256",
      resource,
      scope,
    });
    const sent = await anyone.get(`/api/v1/oauth/authorize?${query}`, { maxRedirects: 0 });
    expect(sent.status()).toBe(302);
    const pending = new URL(sent.headers()["location"]!).searchParams.get("request");
    expect(pending).toBeTruthy();
    return pending!;
  } finally {
    await anyone.dispose();
  }
}

test.describe("connecting an app", { tag: "@desktop" }, () => {
  const made: string[] = [];
  test.afterEach(async () => {
    // A client's grants go with it.
    await withDatabase((db) => db.query("DELETE FROM oauth_client WHERE id = ANY($1)", [made.splice(0)]));
  });

  test.beforeEach(async ({ page }) => {
    await page.route(`${CALLBACK}**`, (route) => route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Back in the app</title>" }));
  });

  test("alice allows an app narrowed to reading, and is sent back to it with a code", async ({ page }) => {
    const client = await aClient("Desk assistant");
    made.push(client);
    await page.goto(`/oauth/consent?request=${await askToConnect(client)}`);
    await expect(page.getByText("Desk assistant wants to work in Exciter as you.")).toBeVisible();
    await expect(page.locator("[data-consent-host]")).toHaveText("You go back to 127.0.0.1:9 afterwards.");
    await expectAccessible(page);

    await page.getByRole("checkbox", { name: "Read only" }).check();
    await page.locator('[data-action="allow-app"]').click();
    await page.waitForURL((url) => url.href.startsWith(CALLBACK));
    const back = new URL(page.url());
    expect(back.searchParams.get("code")).toMatch(/^exciter_oac_/);
    expect(back.searchParams.get("state")).toBe(STATE);
    expect(back.searchParams.get("iss")).toBeTruthy();
  });

  test("alice denies an app that asked to read, and it hears so", async ({ page }) => {
    const client = await aClient("Reading assistant");
    made.push(client);
    await page.goto(`/oauth/consent?request=${await askToConnect(client, "mcp:read")}`);
    await expect(page.getByRole("checkbox", { name: "Read only" })).toBeDisabled();
    await expect(page.getByText("It asked only to read.")).toBeVisible();
    await expectAccessible(page);

    await page.locator('[data-action="deny-app"]').click();
    await page.waitForURL((url) => url.href.startsWith(CALLBACK));
    const back = new URL(page.url());
    expect(back.searchParams.get("error")).toBe("access_denied");
    expect(back.searchParams.get("state")).toBe(STATE);
  });

  test("a request that ran out says to start again", async ({ page }) => {
    await page.goto("/oauth/consent?request=00000000-0000-4000-8000-000000000000");
    await expect(page.getByText(/This request to connect has expired or was answered already/)).toBeVisible();
    await expectAccessible(page);
  });
});

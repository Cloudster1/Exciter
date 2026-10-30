import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";
import type { APIRequestContext } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, confirm, toast, dismissToasts } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// The controls of /admin/webhooks that webhooks.spec.ts leaves alone (#328). Each webhook listens to a project of
// its own test, since the tests of a file run side by side.

// web/src/config.ts WEBHOOKS_PATH and WEBHOOK_SIGNATURE_HEADER, and the header that names a delivery.
const WEBHOOKS_PATH = "/admin/webhooks";
const SIGNATURE_HEADER = "X-Exciter-Signature-256";
const DELIVERY_HEADER = "X-Exciter-Delivery";
const SECRET_PREFIX = "exciter_whs_";
// The api and the worker reach the stub on the stack's network by this name, which EXCITER_OUTBOUND_ALLOW lets through.
const STUB_FROM_STACK = "http://armature-stub:8080";
const REVISED = "requirement.revised";
// backend/internal/webhook DisableAfterFailures: the worker turns a webhook off at this failure, once it has failed a day.
const DISABLE_AFTER_FAILURES = 6;
// A receiver's answers the tests set.
const UNAVAILABLE = 503;
const NO_CONTENT = 204;
// How long the worker may take to queue and send a delivery.
const DELIVERY_MS = 30_000;

const REDELIVER_KEY = uniqueKey("HOOKR");
const ROTATE_KEY = uniqueKey("HOOKS");
const FAILING_KEY = uniqueKey("HOOKF");

type Received = { headers: Record<string, string>; body: string };

/** A bin of its own: where the stack posts, how the test reads back what it took and tells it what to answer. */
function bin(request: APIRequestContext) {
  const name = `e2e-${randomUUID().slice(0, 8)}`;
  return {
    address: `${STUB_FROM_STACK}/_hooks/${name}`,
    read: async (): Promise<Received[]> => {
      const answer = await request.get(`${armatureURL()}/_hooks/${name}`);
      expect(answer.ok()).toBe(true);
      return ((await answer.json()) as { deliveries: Received[] }).deliveries;
    },
    answers: async (status: number) => {
      const answer = await request.put(`${armatureURL()}/_hooks/${name}/status`, { data: { status } });
      expect(answer.status()).toBe(NO_CONTENT);
    },
  };
}

function signedWith(secret: string, delivery: Received): boolean {
  const expected = Buffer.from(`sha256=${createHmac("sha256", secret).update(delivery.body).digest("hex")}`);
  const got = Buffer.from(delivery.headers[SIGNATURE_HEADER] ?? "");
  return got.length === expected.length && timingSafeEqual(got, expected);
}

/** A project with one requirement, and a webhook on its revisions posting to address; answers its id and secret. */
async function hooked(api: ExciterApi, key: string, name: string, address: string): Promise<{ id: string; secret: string }> {
  must(await api.POST("/projects", { body: { key, name: `Hooked ${key}` } }));
  must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: key } }, body: { title: "Brakes engage within 100 ms" } }));
  const made = must(await api.POST("/webhooks", { body: { name, url: address, topics: [REVISED], projects: [key] } })).webhook;
  return { id: made.id, secret: made.secret ?? "" };
}

async function revise(api: ExciterApi, key: string, title: string, baseRevision: number) {
  must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: `${key}-R1` } }, body: { title, baseRevision } }));
}

async function cleanUp(api: ExciterApi, key: string) {
  const listed = await api.GET("/webhooks");
  for (const hook of listed.data?.webhooks ?? []) {
    if (hook.projects.some((p) => p.key === key)) await api.DELETE("/webhooks/{webhookID}", { params: { path: { webhookID: hook.id } } });
  }
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("webhook controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    for (const key of [REDELIVER_KEY, ROTATE_KEY, FAILING_KEY]) await cleanUp(api, key);
  });

  test("alice sends a failed revision again from the log once the receiver is back, as the same event", async ({ page, api, request }) => {
    const receiver = bin(request);
    await receiver.answers(UNAVAILABLE);
    const name = `Flaky ${REDELIVER_KEY}`;
    const { id } = await hooked(api, REDELIVER_KEY, name, receiver.address);
    await revise(api, REDELIVER_KEY, "Brakes engage within 80 ms", 1);
    const log = { params: { path: { webhookID: id } } };
    await expect
      .poll(async () => must(await api.GET("/webhooks/{webhookID}/deliveries", log)).deliveries.some((d) => d.topic === REVISED && d.state === "failed"), {
        timeout: DELIVERY_MS,
      })
      .toBe(true);
    await receiver.answers(NO_CONTENT);

    await page.goto(WEBHOOKS_PATH);
    const row = page.locator(`[data-webhook="${name}"]`);
    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Delivery log" }).click();
    const dialog = page.getByRole("dialog", { name: `Deliveries to ${name}` });
    const failed = dialog.locator(`[data-delivery="${REVISED}"][data-delivery-state="failed"]`);
    await expect(failed).toHaveCount(1);
    await expect(failed).toContainText("The receiver answered 503.");
    await expectAccessible(page);
    await failed.locator('[data-action="redeliver"]').click();
    await expect(dialog.getByRole("status")).toHaveText("Sent again: Delivered.");
    const delivered = dialog.locator(`[data-delivery="${REVISED}"][data-delivery-state="delivered"]`);
    await expect(delivered).toHaveCount(1);
    await expect(delivered).toContainText("by hand");
    await expect(delivered).toContainText("The receiver answered 204.");

    const both = await receiver.read();
    expect(both).toHaveLength(2);
    const [first, again] = both.map((d) => ({ event: (JSON.parse(d.body) as { id: string }).id, delivery: d.headers[DELIVERY_HEADER] }));
    expect(again!.event).toBe(first!.event);
    expect(again!.delivery).not.toBe(first!.delivery);
  });

  test("alice rotates the secret, copies the new one, and the next ping is signed with it alone", async ({ page, api, request }) => {
    const receiver = bin(request);
    const name = `Rotated ${ROTATE_KEY}`;
    const { secret: old } = await hooked(api, ROTATE_KEY, name, receiver.address);
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);

    await page.goto(WEBHOOKS_PATH);
    const row = page.locator(`[data-webhook="${name}"]`);
    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Rotate the secret" }).click();
    await expect(page.locator("[data-confirm]")).toContainText(`Rotate the secret of ${name}?`);
    await expect(page.locator("[data-confirm]")).toContainText("The receiver refuses every delivery until it has the new one.");
    await confirm(page);
    const field = page.getByLabel(`Secret for ${name}`, { exact: true });
    await expect(field).toHaveValue(new RegExp(`^${SECRET_PREFIX}`));
    const fresh = await field.inputValue();
    expect(fresh).not.toBe(old);
    await expectAccessible(page);
    await page.locator('[data-action="copy-webhook-secret"]').click();
    await expect(page.locator("[data-webhook-secret]").getByRole("status")).toHaveText("Copied the secret.");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(fresh);
    await page.locator('[data-action="webhook-secret-done"]').click();
    await expect(field).toHaveCount(0);

    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Send a test ping" }).click();
    await expect(toast(page, `The ping reached ${name}. The receiver answered 204.`)).toHaveText(`The ping reached ${name}. The receiver answered 204.`);
    await dismissToasts(page);
    const [ping] = await receiver.read();
    expect(signedWith(fresh, ping!)).toBe(true);
    expect(signedWith(old, ping!)).toBe(false);
    await page.reload();
    await expect(row).toBeVisible();
    expect(await page.content()).not.toContain(fresh);
  });

  test("a webhook the worker turned off says so, alice turns it on so the next revision arrives, then deletes it", async ({ page, api, request }) => {
    const receiver = bin(request);
    await receiver.answers(UNAVAILABLE);
    const name = `Failing ${FAILING_KEY}`;
    const { id } = await hooked(api, FAILING_KEY, name, receiver.address);
    // A day of failures but the last: the next one is what makes the worker turn it off.
    await withDatabase((db) =>
      db.query("UPDATE webhook_endpoint SET failures = $2, failing_since = now() - interval '25 hours' WHERE id = $1", [id, DISABLE_AFTER_FAILURES - 1]),
    );
    await revise(api, FAILING_KEY, "Brakes engage within 80 ms", 1);
    await expect.poll(async () => must(await api.GET("/webhooks")).webhooks.find((h) => h.id === id)?.disabledReason, { timeout: DELIVERY_MS }).toBe("failing");

    await page.goto(WEBHOOKS_PATH);
    const row = page.locator(`[data-webhook="${name}"]`);
    await expect(row).toHaveAttribute("data-webhook-enabled", "false");
    await expect(row.locator("[data-webhook-state]")).toHaveText("Turned off: failing");
    await expect(row).toContainText(
      `Turned off after ${DISABLE_AFTER_FAILURES} failed attempts over a day. Fix the receiver, send a test ping, then turn it on again.`,
    );
    await expectAccessible(page);

    await receiver.answers(NO_CONTENT);
    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Turn on" }).click();
    await expect(toast(page, `${name} is on again.`)).toHaveText(`${name} is on again.`);
    await dismissToasts(page);
    await expect(row).toHaveAttribute("data-webhook-enabled", "true");
    await expect(row.locator("[data-webhook-state]")).toHaveText("On");
    await expect(row).not.toContainText("Turned off after");

    await revise(api, FAILING_KEY, "Brakes engage within 70 ms", 2);
    await expect
      .poll(
        async () =>
          (await receiver.read()).some((d) => {
            const env = JSON.parse(d.body) as { topic: string; payload: { revision: number } };
            return env.topic === REVISED && env.payload.revision === 3 && d.headers[SIGNATURE_HEADER] !== undefined;
          }),
        { timeout: DELIVERY_MS },
      )
      .toBe(true);

    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await dismissToasts(page);
    await page.getByRole("menuitem", { name: "Delete" }).click();
    await expect(page.locator("[data-confirm]")).toContainText(`Delete webhook ${name}?`);
    await expect(page.locator("[data-confirm]")).toContainText("It stops receiving at once, and its delivery log goes with it.");
    await confirm(page);
    await expect(toast(page, `${name} was deleted.`)).toHaveText(`${name} was deleted.`);
    await dismissToasts(page);
    await expect(row).toHaveCount(0);
    await page.reload();
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Webhooks");
    await expect(row).toHaveCount(0);
    const gone = await api.GET("/webhooks/{webhookID}/deliveries", { params: { path: { webhookID: id } } });
    expect(gone.response.status).toBe(404);
    const left = await withDatabase((db) => db.query("SELECT count(*)::int AS n FROM webhook_delivery WHERE endpoint_id = $1", [id]));
    expect(left.rows[0]!.n).toBe(0);
  });
});

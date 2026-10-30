import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";
import type { APIRequestContext } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Outbound webhooks (#48), adapted from Stator's spec: a webhook for alice's project alone, so other specs' events never
// reach it, whose bin on the armature-stub receives a revision signed with the secret shown once.

// web/src/config.ts WEBHOOKS_PATH and WEBHOOK_SIGNATURE_HEADER.
const WEBHOOKS_PATH = "/admin/webhooks";
const SIGNATURE_HEADER = "X-Exciter-Signature-256";
const SECRET_PREFIX = "exciter_whs_";
// The api and the worker reach the stub on the stack's network by this name, which EXCITER_OUTBOUND_ALLOW lets through.
const STUB_FROM_STACK = "http://armature-stub:8080";

const KEY = uniqueKey("HOOK");
const REQ_KEY = `${KEY}-R1`;
// The CloudEvents test's own project, since the tests of a file run side by side.
const CE_KEY = uniqueKey("HOOKC");
// The CloudEvents envelope's content type and type prefix (#133), backend/internal/webhook.
const CLOUDEVENTS = "application/cloudevents+json; charset=utf-8";
const TYPE_PREFIX = "dev.exciter.";

type Received = { headers: Record<string, string>; body: string };

/** A bin of its own: where the stack posts, and how the test reads back what it took. */
function bin(): { address: string; read: (request: APIRequestContext) => Promise<Received[]> } {
  const name = `e2e-${randomUUID().slice(0, 8)}`;
  return {
    address: `${STUB_FROM_STACK}/_hooks/${name}`,
    read: async (request) => {
      const answer = await request.get(`${armatureURL()}/_hooks/${name}`);
      expect(answer.ok()).toBe(true);
      return ((await answer.json()) as { deliveries: Received[] }).deliveries;
    },
  };
}

/** Whether a body carries the signature the secret makes, as a receiver would check it. */
function signedWith(secret: string, delivery: Received): boolean {
  const expected = Buffer.from(`sha256=${createHmac("sha256", secret).update(delivery.body).digest("hex")}`);
  const got = Buffer.from(delivery.headers[SIGNATURE_HEADER] ?? "");
  return got.length === expected.length && timingSafeEqual(got, expected);
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

test.describe("outbound webhooks", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => {
    await cleanUp(api, KEY);
    await cleanUp(api, CE_KEY);
  });

  test("alice adds a webhook for her project, copies its secret once, and a revision reaches the receiver signed", async ({ page, api, request }) => {
    must(await api.POST("/projects", { body: { key: KEY, name: "Hooked" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title: "Brakes engage within 100 ms" } }));
    const receiver = bin();

    await page.goto(WEBHOOKS_PATH);
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Webhooks");
    await page.getByLabel("Name", { exact: true }).fill("Test bench");
    await page.getByLabel("Address", { exact: true }).fill(receiver.address);
    await page.getByLabel("Everything, events added later too").uncheck();
    await page.getByLabel("A requirement gets a new revision").check();
    await page.locator(`[data-project="${KEY}"]`).check();
    await expectAccessible(page);
    await page.locator('[data-action="create-webhook"]').click();

    const secretField = page.getByLabel("Secret for Test bench", { exact: true });
    await expect(secretField).toHaveValue(new RegExp(`^${SECRET_PREFIX}`));
    const secret = await secretField.inputValue();
    await expect(page.locator("[data-webhook-secret]")).toContainText(SIGNATURE_HEADER);
    await page.locator('[data-action="webhook-secret-done"]').click();
    const row = page.locator('[data-webhook="Test bench"]');
    await expect(row).toContainText("A requirement gets a new revision");
    await expect(row.locator("[data-webhook-projects]")).toHaveText(KEY);
    await page.reload();
    await expect(row).toBeVisible();
    expect(await page.content()).not.toContain(secret);

    must(await api.PATCH("/requirements/{reqKey}", { params: { path: { reqKey: REQ_KEY } }, body: { title: "Brakes engage within 80 ms", baseRevision: 1 } }));
    await expect.poll(async () => (await receiver.read(request)).length, { timeout: 30_000 }).toBe(1);
    const [revision] = await receiver.read(request);
    expect(signedWith(secret, revision!)).toBe(true);
    expect(JSON.parse(revision!.body)).toMatchObject({
      source: "exciter",
      topic: "requirement.revised",
      payload: { requirement: { key: REQ_KEY, title: "Brakes engage within 80 ms" }, revision: 2 },
    });

    await row.getByRole("button", { name: "Actions for Test bench" }).click();
    await page.getByRole("menuitem", { name: "Send a test ping" }).click();
    await expect(toast(page, "The ping reached Test bench. The receiver answered 204.")).toHaveText("The ping reached Test bench. The receiver answered 204.");
    await dismissToasts(page);
    await row.getByRole("button", { name: "Actions for Test bench" }).click();
    await page.getByRole("menuitem", { name: "Delivery log" }).click();
    const log = page.getByRole("dialog", { name: "Deliveries to Test bench" });
    await expect(log.locator('[data-delivery="ping"]')).toHaveAttribute("data-delivery-state", "delivered");
    await expect(log.locator('[data-delivery="requirement.revised"]')).toHaveAttribute("data-delivery-state", "delivered");
    await expectAccessible(page);
    await page.keyboard.press("Escape");

    await row.getByRole("button", { name: "Actions for Test bench" }).click();
    await page.getByRole("menuitem", { name: "Turn off" }).click();
    await expect(row).toHaveAttribute("data-webhook-enabled", "false");
    await expect(toast(page, "Test bench is off. Nothing is sent until it is turned on again.")).toHaveText(
      "Test bench is off. Nothing is sent until it is turned on again.",
    );
    await dismissToasts(page);
  });

  test("alice adds a webhook in the CloudEvents envelope, and a revision reaches the receiver as a CloudEvent, signed as before", async ({
    page,
    api,
    request,
  }) => {
    must(await api.POST("/projects", { body: { key: CE_KEY, name: "Brokered" } }));
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: CE_KEY } }, body: { title: "Log every login" } }));
    const receiver = bin();

    await page.goto(WEBHOOKS_PATH);
    await page.getByLabel("Name", { exact: true }).fill("Broker");
    await page.getByLabel("Address", { exact: true }).fill(receiver.address);
    await page.getByLabel("Everything, events added later too").uncheck();
    await page.getByLabel("A requirement gets a new revision").check();
    await page.locator(`[data-project="${CE_KEY}"]`).check();
    await page.getByLabel("Envelope", { exact: true }).selectOption({ label: "Version 2, CloudEvents" });
    await expectAccessible(page);
    await page.locator('[data-action="create-webhook"]').click();
    const secret = await page.getByLabel("Secret for Broker", { exact: true }).inputValue();
    await page.locator('[data-action="webhook-secret-done"]').click();
    const row = page.locator('[data-webhook="Broker"]');
    await expect(row.locator("[data-webhook-version]")).toHaveText("Version 2, CloudEvents");

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: `${CE_KEY}-R1` } },
        body: { title: "Log every login and logout", baseRevision: 1 },
      }),
    );
    await expect.poll(async () => (await receiver.read(request)).length, { timeout: 30_000 }).toBe(1);
    const [revision] = await receiver.read(request);
    expect(signedWith(secret, revision!)).toBe(true);
    expect(revision!.headers["Content-Type"]).toBe(CLOUDEVENTS);
    expect(JSON.parse(revision!.body)).toMatchObject({
      specversion: "1.0",
      type: `${TYPE_PREFIX}requirement.revised`,
      datacontenttype: "application/json",
      dataversion: 1,
      data: { requirement: { key: `${CE_KEY}-R1`, title: "Log every login and logout" }, revision: 2 },
    });

    await row.getByRole("button", { name: "Actions for Broker" }).click();
    await page.getByRole("menuitem", { name: "Send in Version 1" }).click();
    await expect(row.locator("[data-webhook-version]")).toHaveText("Version 1");
    await expect(toast(page, "Broker is sent in Version 1 from the next delivery on.")).toHaveText("Broker is sent in Version 1 from the next delivery on.");
    await dismissToasts(page);
  });
});

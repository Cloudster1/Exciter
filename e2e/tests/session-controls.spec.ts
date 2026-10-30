import { readFileSync } from "node:fs";
import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

// Every control of the project's sessions page and a session's report (#317): alice opens a session over a baseline
// and one over a query of what changed since its last approval, and downloads the report while bob's inbox lists it.

const KEY = uniqueKey("SESC");
const DAY_MS = 24 * 60 * 60 * 1000;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project where alice administers, everybody edits and bob approves, with a section of two requirements. */
async function aProject(api: ExciterApi, people: { alice: { id: string }; bob: { id: string } }) {
  const project = { params: { path: { projectKey: KEY } } };
  must(await api.POST("/projects", { body: { key: KEY, name: "Session controls" } }));
  await setProjectGrants(api, KEY, [
    { subject: { type: "user", id: people.alice.id }, role: "admin" },
    { subject: { type: "everyone" }, role: "editor" },
    { subject: { type: "user", id: people.bob.id }, role: "reviewer" },
  ]);
  const write = async (body: Record<string, unknown>) =>
    must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: body as never })).requirement;
  const braking = await write({ kind: "section", title: "Braking" });
  const pads = await write({ title: "Brake pads wear evenly", place: { parentId: braking.id, afterId: null } });
  const lights = await write({ title: "Brake lights follow the pedal", place: { parentId: braking.id, afterId: pads.id } });
  return { project, braking, pads, lights };
}

/** A datetime-local value two days ahead, as the deadline field takes it. */
function twoDaysAhead(): string {
  const at = new Date(Date.now() + 2 * DAY_MS);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}T${pad(at.getHours())}:${pad(at.getMinutes())}`;
}

/** Picks somebody in the dialog's participant picker. */
async function pick(page: Page, dialog: Locator, person: { email: string; name: string }) {
  await dialog.getByRole("combobox", { name: "Add a person or group" }).pressSequentially(person.email.split("@")[0]!);
  await page.getByRole("option", { name: new RegExp(person.name) }).click();
}

/** Clicks what downloads a file, a template chosen first if the dialog asks for one, and answers the file. */
async function downloaded(page: Page, click: () => Promise<void>) {
  const download = page.waitForEvent("download");
  await click();
  const choose = page.locator('[data-action="export-with-template"]');
  await Promise.race([download, choose.click().catch(() => undefined)]);
  const saved = await download;
  return readFileSync(await saved.path());
}

test.describe("session controls", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice opens sessions over a baseline and over a query of what changed, and the page lists them", async ({ page, api, apiAs, people }) => {
    // Two sessions opened through the dialog, a review approved before: about 25 s on CI.
    test.setTimeout(90_000);
    const { project, pads, lights } = await aProject(api, people);
    must(await api.POST("/projects/{projectKey}/baselines", { ...project, body: { name: "Release 1" } }));
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...project,
        body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: people.bob.id }] },
      }),
    );
    const asked = must(await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey: pads.key } }, body: { revision: 1 } })).review;
    const bobApi = await apiAs("bob");
    await expect
      .poll(
        async () =>
          (await bobApi.POST("/reviews/{reviewID}/decisions", { params: { path: { reviewID: asked.id } }, body: { decision: "approve", revision: 1 } }))
            .response.status,
      )
      .toBe(200);

    await page.goto(`/p/${KEY}/sessions`);
    await expect(page.getByText("No review session yet.")).toBeVisible();
    await page.getByRole("button", { name: "Open review session" }).click();
    const dialog = page.getByRole("dialog", { name: "Open a review session" });
    await dialog.getByLabel("Title", { exact: true }).fill("Release review");
    await dialog.getByLabel("Review", { exact: true }).selectOption("baseline");
    await dialog.getByLabel("Baseline", { exact: true }).selectOption({ label: "Release 1" });
    await pick(page, dialog, people.bob);
    await pick(page, dialog, people.alice);
    await dialog.getByRole("button", { name: `Remove ${people.alice.name}` }).click();
    await expect(dialog.locator(`[data-participant="${people.alice.name}"]`)).toHaveCount(0);
    await dialog.getByLabel("Deadline", { exact: true }).fill(twoDaysAhead());
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Open session" }).click();
    await expect(page.getByRole("heading", { name: "Release review" })).toBeVisible();
    await expect(page.locator("[data-session-item]")).toHaveCount(2);
    await expect(page.locator(`[data-session-item="${pads.key}"]`)).toBeVisible();
    await expect(page.locator(`[data-session-item="${lights.key}"]`)).toBeVisible();
    await expect(page.locator(`[data-session-progress="${people.bob.name}"]`)).toContainText("Approver");
    await expect(page.locator(`[data-session-progress="${people.alice.name}"]`)).toHaveCount(0);
    await expect(page.locator("main")).toContainText("A baseline");
    await expect(page.locator("main")).toContainText("Due ");
    const first = new URL(page.url()).pathname.split("/").pop()!;
    must(await api.POST("/review-sessions/{sessionID}/close", { params: { path: { sessionID: first } }, body: {} }));

    await page.goto(`/p/${KEY}/sessions`);
    await page.getByRole("button", { name: "Open review session" }).click();
    await dialog.getByLabel("Title", { exact: true }).fill("Changed lights review");
    await dialog.getByLabel("Review", { exact: true }).selectOption("query");
    await dialog.getByLabel("Query", { exact: true }).fill("title ~ brake");
    await dialog.getByLabel("Only what changed since").selectOption({ label: "Each requirement's last approval" });
    await pick(page, dialog, people.bob);
    await pick(page, dialog, people.alice);
    await dialog.getByLabel(`Role of ${people.alice.name}`).selectOption("reviewer");
    await dialog.getByLabel("Deadline", { exact: true }).fill(twoDaysAhead());
    await dialog.getByRole("button", { name: "Open session" }).click();
    await expect(page.getByRole("heading", { name: "Changed lights review" })).toBeVisible();
    await expect(page.locator("[data-session-item]")).toHaveCount(1);
    await expect(page.locator(`[data-session-item="${lights.key}"]`)).toBeVisible();
    await expect(page.locator(`[data-session-progress="${people.alice.name}"]`)).toContainText("Reviewer");
    await expect(page.locator("main")).toContainText("Query: title ~ brake");

    await page.getByRole("link", { name: "All review sessions" }).click();
    const rows = page.getByRole("list", { name: "Review sessions" }).locator("[data-session]");
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toHaveAttribute("data-session", "Changed lights review");
    await expect(rows.nth(0)).toContainText("Open");
    await expect(rows.nth(1)).toHaveAttribute("data-session", "Release review");
    await expect(rows.nth(1)).toContainText("Closed");
    await expectAccessible(page);
  });

  test("alice downloads the report, and bob's inbox lists the session until it closes", async ({ page, api, pageAs, people }) => {
    // The PDF is printed by the render service: about 20 s on CI.
    test.setTimeout(90_000);
    const { project, braking } = await aProject(api, people);
    const session = must(
      await api.POST("/projects/{projectKey}/review-sessions", {
        ...project,
        body: {
          title: "Braking review",
          scope: { kind: "section", rootKey: braking.key },
          participants: [{ subjectType: "user", userId: people.bob.id, role: "approver" }],
        },
      }),
    ).session;

    const bob = await pageAs("bob");
    const mine = bob.getByRole("list", { name: "Your review sessions" });
    await openUntil(bob, "/reviews", () => expect(mine.getByRole("link", { name: "Braking review" })).toBeVisible(ONE_LOOK));
    await expect(mine.locator('[data-session="Braking review"]')).toContainText("2 wait for you");

    await openUntil(page, `/p/${KEY}/sessions/${session.id}`, () => expect(page.getByRole("heading", { name: "Braking review" })).toBeVisible(ONE_LOOK));
    const pdf = await downloaded(page, () => page.locator('[data-action="report-pdf"]').click());
    expect(pdf.subarray(0, 5).toString()).toBe("%PDF-");
    const word = await downloaded(page, () => page.locator('[data-action="report-docx"]').click());
    expect(word.subarray(0, 2).toString()).toBe("PK");

    await page.getByRole("button", { name: "Close session" }).click();
    await page.getByRole("dialog", { name: "Close the session" }).getByRole("button", { name: "Close session" }).click();
    await expect(page.getByText("The session is closed.")).toBeVisible();
    await openUntil(bob, "/reviews", () => expect(bob.locator("[data-my-sessions]")).toBeVisible(ONE_LOOK));
    await openUntil(bob, "/reviews", () => expect(bob.locator('[data-my-sessions] [data-session="Braking review"]')).toHaveCount(0, ONE_LOOK));
  });
});

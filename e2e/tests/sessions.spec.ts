import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { setProjectGrants } from "../fixtures/grants";

// Review sessions (#204): alice opens a session over a section from the document, bob decides each requirement in
// review mode, a revised one comes back in round two, and alice closes the session with a baseline.

const KEY = uniqueKey("SESS");

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

test.describe("review sessions", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("alice opens a session over a section, bob decides in two rounds, and alice closes it with a baseline", async ({ page, api, pageAs, people }) => {
    // Two people decide over two rounds and the session closes with a baseline: about 20 s on CI.
    test.setTimeout(60_000);
    const project = { params: { path: { projectKey: KEY } } };
    must(await api.POST("/projects", { body: { key: KEY, name: "Sessions" } }));
    await setProjectGrants(api, KEY, [
      { subject: { type: "user", id: people.alice.id }, role: "admin" },
      { subject: { type: "everyone" }, role: "editor" },
      { subject: { type: "user", id: people.bob.id }, role: "reviewer" },
    ]);
    const write = async (body: Record<string, unknown>) =>
      must(await api.POST("/projects/{projectKey}/requirements", { ...project, body: body as never })).requirement;
    const braking = await write({ kind: "section", title: "Braking" });
    const pads = await write({ title: "Pads wear evenly", place: { parentId: braking.id, afterId: null } });
    const lights = await write({ title: "Lights follow the pedal", place: { parentId: braking.id, afterId: pads.id } });

    await page.goto(`/p/${KEY}/spec`);
    await page.locator(`[data-spec-entry="${braking.key}"] [data-action="open-session"]`).click();
    const dialog = page.getByRole("dialog", { name: "Open a review session" });
    await dialog.getByLabel("Title").fill("Braking review");
    await expect(dialog.getByLabel("Section")).toHaveValue(braking.key);
    await dialog.getByRole("combobox", { name: "Add a person or group" }).pressSequentially(people.bob.email.split("@")[0]!);
    await page.getByRole("option", { name: new RegExp(people.bob.name) }).click();
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Open session" }).click();
    await expect(page.getByRole("heading", { name: "Braking review" })).toBeVisible();
    const sessionURL = page.url();

    const bob = await pageAs("bob");
    await openUntil(bob, new URL(sessionURL).pathname, () => expect(bob.getByRole("link", { name: "Review in the document" })).toBeVisible(ONE_LOOK));
    await bob.getByRole("link", { name: "Review in the document" }).click();
    await expect(bob.locator("[data-reviewing-session]")).toContainText("Braking review");
    await bob.locator(`[data-spec-entry="${pads.key}"] [data-action="approve-entry"]`).click();
    await bob.locator(`[data-spec-entry="${lights.key}"]`).getByLabel("Your comment").fill("Say how fast.");
    await bob.locator(`[data-spec-entry="${lights.key}"] [data-action="reject-entry"]`).click();
    await expect(bob.locator(`[data-spec-entry="${lights.key}"]`)).toContainText(/reject/i);
    await expectAccessible(bob);

    must(
      await api.PATCH("/requirements/{reqKey}", {
        params: { path: { reqKey: lights.key } },
        body: { title: "Lights follow the pedal within 50 ms", baseRevision: 1 },
      }),
    );
    await openUntil(page, new URL(sessionURL).pathname, () =>
      expect(page.locator(`[data-session-item="${lights.key}"]`)).toHaveAttribute("data-session-outcome", "rejected", ONE_LOOK),
    );
    await page.getByRole("button", { name: "Start next round" }).click();
    await page.getByRole("dialog", { name: "Start the next round" }).getByRole("button", { name: "Start" }).click();
    await expect(page.getByText("Round 2 started.")).toBeVisible();

    await openUntil(bob, `/p/${KEY}/spec?session=${new URL(sessionURL).pathname.split("/").pop()}&waiting=true&changes=round`, () =>
      expect(bob.locator(`[data-spec-entry="${lights.key}"] [data-action="approve-entry"]`)).toBeVisible(ONE_LOOK),
    );
    await bob.locator(`[data-spec-entry="${lights.key}"] [data-action="approve-entry"]`).click();

    await openUntil(page, new URL(sessionURL).pathname, () =>
      expect(page.locator(`[data-session-item="${lights.key}"]`)).toHaveAttribute("data-session-outcome", "approved", ONE_LOOK),
    );
    await page.getByRole("button", { name: "Close session" }).click();
    const closing = page.getByRole("dialog", { name: "Close the session" });
    await closing.getByLabel("Take a baseline of what was approved").check();
    await closing.getByLabel("Baseline name").fill("Braking agreed");
    await expectAccessible(page);
    await closing.getByRole("button", { name: "Close session" }).click();
    await expect(page.getByText("The session is closed.")).toBeVisible();
    await expect.poll(async () => must(await api.GET("/projects/{projectKey}/baselines", project)).baselines.map((b) => b.name)).toContain("Braking agreed");

    // The report the render service prints: who took part, and each requirement's decisions round by round.
    await page.goto(`/print/sessions/${new URL(sessionURL).pathname.split("/").pop()}`);
    await expect(page.locator("html[data-print-ready]")).toBeAttached();
    const report = page.locator('[data-print-view] [data-print-document="session"]');
    await expect(report.getByRole("heading", { level: 1, name: "Braking review" })).toBeVisible();
    await expect(report).toContainText("Baseline taken: Braking agreed");
    await expect(report.getByRole("row", { name: new RegExp(people.bob.name) })).toBeVisible();
    await expect(report.locator(`[data-report-item="${pads.key}"]`)).toContainText("Round 1, Revision 1: Approved");
    const revised = report.locator(`[data-report-item="${lights.key}"]`);
    await expect(revised).toContainText("Lights follow the pedal within 50 ms");
    await expect(revised).toContainText("Round 1, Revision 1: Rejected");
    await expect(revised).toContainText("Say how fast.");
    await expect(revised).toContainText("Round 2, Revision 2: Approved");
    await expectAccessible(page);
  });
});

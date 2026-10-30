import { request, type Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { uniqueKey } from "../fixtures/seed";
import { expectAccessible, toast, dismissToasts } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// The evidence settings and the code tags page (#323): alice names the counting branch and a tag word in the
// project's settings and sees each take effect where the evidence shows, and walks and filters the code tags.

const BRANCH_KEY = uniqueKey("EVBR");
const WORDS_KEY = uniqueKey("EVWD");
const TAGS_KEY = uniqueKey("EVTG");
const COMMIT = "4f2c9a1e7b3d";
// What the code tags page shows at once, as CODE_TAGS_PAGE_SIZE in web/src/config.ts says, and one tag more.
const PAGE_SIZE = 50;
const GHOSTS = PAGE_SIZE + 1;
// Keys no requirement of the project holds, so each ghost's tag is orphaned.
const FIRST_GHOST = 900;

async function removeProject(api: ExciterApi, key: string) {
  const path = { params: { path: { projectKey: key } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

/** A project with one requirement and an evidence token for it, as a pipeline holds one. */
async function projectWithToken(api: ExciterApi, key: string) {
  must(await api.POST("/projects", { body: { key, name: "Evidence" } }));
  must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: key } }, body: { title: "Brakes engage within 100 ms" } }));
  return must(await api.POST("/tokens", { body: { name: `ci ${key}`, scopes: ["evidence"] } })).token;
}

/** Calls the API with the token alone, as exciter-ci does. */
async function asCI<T>(secret: string, call: (ci: Awaited<ReturnType<typeof request.newContext>>) => Promise<T>) {
  const ci = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  try {
    return await call(ci);
  } finally {
    await ci.dispose();
  }
}

/** Uploads a run of one test on a branch and waits for the worker to land it. */
async function ranOn(secret: string, key: string, branch: string, failed: boolean) {
  const outcome = failed ? `<failure message="lights stayed off"/>` : "";
  const report = `<?xml version="1.0"?><testsuites><testsuite name="brakes"><testcase classname="brakes" name="engage [verifies ${key}-R1]">${outcome}</testcase></testsuite></testsuites>`;
  await asCI(secret, async (ci) => {
    const made = await ci.post(`/api/v1/projects/${key}/test-runs`, {
      multipart: { repository: "acme/checkout", commit: COMMIT, branch, file: { name: "junit.xml", mimeType: "application/xml", buffer: Buffer.from(report) } },
    });
    expect(made.status()).toBe(201);
    const id = (await made.json()).run.id as string;
    await expect.poll(async () => (await (await ci.get(`/api/v1/test-runs/${id}`)).json()).run.state, { timeout: 30_000 }).toBe("done");
  });
}

interface Tag {
  path: string;
  line: number;
  word: string;
  key: string;
  revision?: number;
}

async function scanned(secret: string, key: string, tags: Tag[]) {
  await asCI(secret, async (ci) => {
    const made = await ci.post(`/api/v1/projects/${key}/code-scans`, {
      data: { repository: "acme/checkout", commit: COMMIT, branch: "main", fileUrl: "https://git.example/acme/checkout/blob/{commit}/{path}#L{line}", tags },
    });
    expect(made.status()).toBe(201);
  });
}

/** Reads the coverage report until it says what is expected: the worker clears its cache once it hears of a change. */
async function coverageReads(page: Page, key: string, said: string) {
  await page.goto(`/projects/${key}/coverage`);
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole("list", { name: "Requirements" })).toContainText(said, { timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
}

test.describe("evidence settings and code tags", { tag: "@desktop" }, () => {
  test("alice names the counting branch, and the results and coverage follow it", async ({ page, api }) => {
    const token = await projectWithToken(api, BRANCH_KEY);
    try {
      must(
        await api.PUT("/projects/{projectKey}/coverage-rules", {
          params: { path: { projectKey: BRANCH_KEY } },
          body: { decomposed: false, implemented: false, verified: true, verifiedFrom: ["tests"] },
        }),
      );
      await ranOn(token.secret!, BRANCH_KEY, "main", false);
      await ranOn(token.secret!, BRANCH_KEY, "release", true);
      await coverageReads(page, BRANCH_KEY, "Verified: Full");

      await page.goto(`/projects/${BRANCH_KEY}/settings/trace`);
      const card = page.locator("[data-evidence-settings]");
      await card.getByLabel("Counting branch").fill("release");
      await card.locator('[data-action="save-evidence-settings"]').click();
      await expect(toast(page, "Saved.")).toBeVisible();
      await dismissToasts(page);
      await expectAccessible(page);

      await page.goto(`/projects/${BRANCH_KEY}/r/${BRANCH_KEY}-R1`);
      const panel = page.locator("[data-test-results]");
      await expect(panel.getByRole("heading", { name: "On release, which counts" })).toBeVisible();
      await expect(panel.getByRole("heading", { name: "On main", exact: true })).toBeVisible();
      await coverageReads(page, BRANCH_KEY, "Verified: Partial");
    } finally {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
      await removeProject(api, BRANCH_KEY);
    }
  });

  test("alice adds a tag word, and a tag scanned before with it shows on its requirement", async ({ page, api }) => {
    const token = await projectWithToken(api, WORDS_KEY);
    try {
      await scanned(token.secret!, WORDS_KEY, [{ path: "brakes/engage.go", line: 12, word: "satisfies", key: `${WORDS_KEY}-R1` }]);
      await page.goto(`/projects/${WORDS_KEY}/r/${WORDS_KEY}-R1`);
      const code = page.locator("[data-code-links]");
      await expect(code).toContainText("No scan tags this requirement yet.");

      await page.goto(`/projects/${WORDS_KEY}/settings/trace`);
      const card = page.locator("[data-code-tag-words]");
      await card.getByLabel("Words for implements").fill("implements, satisfies");
      await card.locator('[data-action="save-code-tag-words"]').click();
      await expect(toast(page, "Saved.")).toBeVisible();
      await dismissToasts(page);
      await expectAccessible(page);

      await page.goto(`/projects/${WORDS_KEY}/r/${WORDS_KEY}-R1`);
      await expect(code.getByRole("link", { name: "brakes/engage.go:12" })).toHaveAttribute(
        "href",
        `https://git.example/acme/checkout/blob/${COMMIT}/brakes/engage.go#L12`,
      );
      await expect(code.getByText("Implements")).toBeVisible();
    } finally {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
      await removeProject(api, WORDS_KEY);
    }
  });

  test("alice filters the code tags by state and turns their pages", async ({ page, api }) => {
    const token = await projectWithToken(api, TAGS_KEY);
    try {
      must(
        await api.PATCH("/requirements/{reqKey}", {
          params: { path: { reqKey: `${TAGS_KEY}-R1` } },
          body: { title: "Brakes engage within 80 ms", baseRevision: 1 },
        }),
      );
      const ghosts: Tag[] = Array.from({ length: GHOSTS }, (_, i) => ({
        path: `ghosts/${String(i).padStart(3, "0")}.go`,
        line: 1,
        word: "implements",
        key: `${TAGS_KEY}-R${FIRST_GHOST + i}`,
      }));
      await scanned(token.secret!, TAGS_KEY, [...ghosts, { path: "brakes/engage.go", line: 12, word: "implements", key: `${TAGS_KEY}-R1`, revision: 1 }]);

      await page.goto(`/projects/${TAGS_KEY}/code-tags`);
      const table = page.getByRole("table", { name: "Code tags" });
      const rows = table.locator("[data-code-tag]");
      const next = page.getByRole("button", { name: "Next" });
      const previous = page.getByRole("button", { name: "Previous" });
      await expect(rows).toHaveCount(PAGE_SIZE);
      await expect(previous).toBeDisabled();
      await expectAccessible(page);
      await next.click();
      // The orphaned come first, so the second page holds the last of them and the outdated one.
      await expect(rows).toHaveCount(GHOSTS + 1 - PAGE_SIZE);
      await expect(table.locator('[data-code-tag="brakes/engage.go:12"]')).toHaveAttribute("data-state", "outdated");
      await expect(next).toBeDisabled();
      await previous.click();
      await expect(rows).toHaveCount(PAGE_SIZE);
      await expect(table.locator('[data-code-tag="ghosts/000.go:1"]')).toBeVisible();

      const show = page.getByLabel("Show");
      await show.selectOption("outdated");
      await expect(rows).toHaveCount(1);
      await expect(rows).toHaveAttribute("data-state", "outdated");
      await expect(table).toContainText("Outdated: revision 1 of 2");
      await expect(next).toBeHidden();

      await show.selectOption("orphaned");
      await expect(rows).toHaveCount(PAGE_SIZE);
      await expect(table.locator('[data-state="outdated"]')).toHaveCount(0);
      await next.click();
      await expect(rows).toHaveCount(GHOSTS - PAGE_SIZE);
      await expect(rows).toHaveAttribute("data-state", "orphaned");
    } finally {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
      await removeProject(api, TAGS_KEY);
    }
  });
});

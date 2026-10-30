import type { Locator, Page } from "@playwright/test";
import type { ExciterApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { ANONYMOUS, expect, LOGIN_PATH } from "../fixtures/auth";
import { peopleTest as test } from "../fixtures/people";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueKey } from "../fixtures/seed";
import { menuItem, openAccountMenu, openShell, scrollsSideways } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// What jsdom can only pretend to (#331): the sidebar folded across a reload, its counts against the real server,
// the way back from settings, the palette's places and full search, the drawer's links, the profile and sign-in.

const KEY = uniqueKey("SHL");
const REQ_KEY = `${KEY}-R1`;
const REQUIREMENT = `/p/${KEY}/r/${REQ_KEY}`;
const SEARCH = `/search?q=brake&project=${KEY}`;

async function removeProject(api: ExciterApi) {
  const path = { params: { path: { projectKey: KEY } } };
  const found = await api.GET("/projects/{projectKey}", path);
  if (!found.data) return;
  if (!found.data.project.archivedAt) await api.PUT("/projects/{projectKey}/archive", path);
  await api.DELETE("/projects/{projectKey}", path);
}

const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** The spec's project, with one requirement about the brake pedal and one about the lights. */
async function makeProject(api: ExciterApi) {
  must(await api.POST("/projects", { body: { key: KEY, name: "Shell walk" } }));
  const write = async (title: string, statement: string) =>
    must(await api.POST("/projects/{projectKey}/requirements", { params: { path: { projectKey: KEY } }, body: { title, statement: paragraph(statement) } }));
  await write("The brake pedal stops the car", "Pressing the brake pedal halfway stops the car within 40 metres.");
  await write("The lights dim at night", "Headlights dim for oncoming traffic.");
}

/** The number a sidebar entry shows beside its name, without the words a screen reader hears. */
const count = (page: Page, name: string): Locator => page.locator(`[data-sidebar] [data-count="${name}"] [aria-hidden="true"]`);

const sidebarGroup = (page: Page, group: string) => page.locator(`[data-sidebar] [data-sidebar-group="${group}"]`);

test.describe("the shell in a real browser", { tag: "@desktop" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("the sidebar folds to the rail and stays folded after a reload, and so does a group", async ({ page }) => {
    await openShell(page, "/");
    await expect(page.locator('[data-sidebar="open"]')).toBeVisible();
    await page.locator('[data-rail] [data-action="sidebar"]').click();
    await expect(page.locator("[data-sidebar]")).toHaveCount(0);
    await expect(page.locator("[data-rail]")).toBeVisible();
    await expect(page.locator('[data-action="sidebar"]')).toHaveAccessibleName("Expand the sidebar");

    await page.reload();
    await expect(page.locator("main#main")).toBeVisible();
    await expect(page.locator("[data-rail]")).toBeVisible();
    await expect(page.locator("[data-sidebar]")).toHaveCount(0);

    const rail = page.locator("[data-rail]");
    for (const [name, path] of [
      ["Search", "/search"],
      ["Projects", "/projects"],
      ["Home", "/"],
    ] as const) {
      const link = rail.getByRole("link", { name, exact: true });
      await link.hover();
      await expect(page.getByRole("tooltip")).toHaveText(name);
      await link.click();
      await expect(page).toHaveURL(`${WEB_URL}${path}`);
      await expect(page.locator("[data-sidebar]")).toHaveCount(0);
    }

    await page.getByRole("button", { name: "Expand the sidebar" }).click();
    await expect(page.locator('[data-sidebar="open"]')).toBeVisible();
    const requirements = sidebarGroup(page, "requirements");
    await expect(requirements).toHaveAttribute("data-open", "true");
    await requirements.getByRole("button", { name: "Requirements" }).click();
    await expect(requirements).toHaveAttribute("data-open", "false");
    await expect(requirements.getByRole("link", { name: "Search" })).toBeHidden();

    await page.reload();
    await expect(page.locator('[data-sidebar="open"]')).toBeVisible();
    await expect(sidebarGroup(page, "requirements")).toHaveAttribute("data-open", "false");
    await expect(sidebarGroup(page, "requirements").getByRole("link", { name: "Search" })).toBeHidden();
    await sidebarGroup(page, "requirements").getByRole("button", { name: "Requirements" }).click();
    await expect(sidebarGroup(page, "requirements").getByRole("link", { name: "Search" })).toBeVisible();
  });

  test("bob's sidebar counts the review alice asks of him and his unread notifications, and both fall", async ({ api, pageAs, people }) => {
    // Bob waits for the worker twice and decides on the requirement page: about 15 s on CI.
    test.setTimeout(60_000);
    await makeProject(api);
    const project = { params: { path: { projectKey: KEY } } };
    must(
      await api.PUT("/projects/{projectKey}/approval-rules", {
        ...project,
        body: { required: 1, authorMayApprove: false, approvers: [{ subjectType: "user", userId: people.bob.id }] },
      }),
    );
    must(await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey: REQ_KEY } }, body: { revision: 1 } }));
    must(
      await api.POST("/requirements/{reqKey}/comments", {
        params: { path: { reqKey: REQ_KEY } },
        body: {
          body: {
            type: "doc",
            content: [
              {
                type: "paragraph",
                content: [
                  { type: "text", text: "Have a look, " },
                  { type: "mention", attrs: { id: people.bob.id, label: people.bob.name } },
                ],
              },
            ],
          },
        },
      }),
    );

    const bob = await pageAs("bob");
    // The review request and the mention reach bob through the worker.
    await openUntil(bob, "/", async () => {
      await expect(count(bob, "reviews")).toHaveText("1", ONE_LOOK);
      await expect(count(bob, "notifications")).toHaveText("2", ONE_LOOK);
    });
    const sidebar = bob.getByRole("navigation", { name: "Where you are" });
    await expect(sidebar.getByRole("link", { name: /^Reviews.*1 review waits for you$/ })).toHaveAttribute("href", "/reviews");
    await expect(sidebar.getByRole("link", { name: /^Notifications.*2 unread/ })).toHaveAttribute("href", "/notifications");

    await sidebar.getByRole("link", { name: /^Notifications/ }).click();
    await expect(bob).toHaveURL(/\/notifications$/);
    await bob
      .locator("[data-notification-list]")
      .getByRole("link", { name: new RegExp(`mentioned you in a comment on ${REQ_KEY}`) })
      .click();
    await expect(bob).toHaveURL(new RegExp(`${REQUIREMENT}#thread-`));
    await expect(count(bob, "notifications")).toHaveText("1");
    await expect(count(bob, "reviews")).toHaveText("1");

    const approval = bob.getByRole("region", { name: "Approval" });
    await approval.locator('[data-action="approve-review"]').click();
    await expect(approval.locator('[data-action="approve-review"]')).toHaveCount(0);
    await expect(count(bob, "reviews")).toHaveCount(0);
    await expect(count(bob, "notifications")).toHaveText("1");
  });

  test("Back to Exciter returns to the whole address the settings area was opened from", async ({ page, context, api }) => {
    await makeProject(api);
    await openShell(page, SEARCH);
    const results = page.locator("[data-search-count]");
    await expect(results).toHaveText("1 result");
    await expect(page.locator(`[data-search-hit="${REQ_KEY}"]`)).toBeVisible();
    const from = page.url();
    expect(from).toBe(`${WEB_URL}${SEARCH}`);

    await openAccountMenu(page);
    await menuItem(page, "profile").click();
    await expect(page).toHaveURL(`${WEB_URL}/settings/profile`);
    const settings = page.locator("[data-settings-sidebar]");
    await settings.getByRole("link", { name: "Watching" }).click();
    await expect(page).toHaveURL(`${WEB_URL}/settings/watching`);
    await settings.getByRole("link", { name: "Themes" }).click();
    await expect(page).toHaveURL(`${WEB_URL}/settings/themes`);

    const back = page.locator('[data-action="back-from-settings"]');
    await expect(back).toHaveAttribute("href", SEARCH);
    await back.click();
    await expect(page).toHaveURL(from);
    await expect(results).toHaveText("1 result");
    await expect(page.locator(`[data-search-hit="${REQ_KEY}"]`)).toBeVisible();
    await expect(page.locator("[data-settings-sidebar]")).toHaveCount(0);

    const fresh = await context.newPage();
    await openShell(fresh, "/settings/notifications");
    const freshBack = fresh.locator('[data-action="back-from-settings"]');
    await expect(freshBack).toHaveAttribute("href", "/");
    await freshBack.click();
    await expect(fresh).toHaveURL(`${WEB_URL}/`);
    await expect(fresh.locator("[data-home]")).toBeVisible();
  });

  test("alice's palette reaches the audit log as Admin / Audit log, and the full search for what she typed", async ({ page, api }) => {
    await makeProject(api);
    await openShell(page, "/projects");
    await page.keyboard.press("ControlOrMeta+k");
    const palette = page.getByRole("dialog", { name: "Go to" });
    await page.locator("[data-quick-switcher-input]").fill("aud");
    const audit = palette.locator('[data-quick-switcher-option="place:/admin/audit"]');
    await expect(audit).toContainText("Admin / Audit log");
    await expect(palette.locator('[data-quick-switcher-option^="place:"]')).toHaveCount(1);
    await audit.click();
    await expect(page).toHaveURL(`${WEB_URL}/admin/audit`);
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Audit log" })).toBeVisible();
    await expect(palette).toHaveCount(0);

    await page.keyboard.press("ControlOrMeta+k");
    await page.locator("[data-quick-switcher-input]").fill("brake pedal");
    const everything = palette.locator('[data-quick-switcher-option="search"]');
    await expect(everything).toContainText("Search everything for brake pedal");
    await everything.click();
    await expect(page).toHaveURL((url) => url.pathname === "/search" && url.searchParams.get("q") === "brake pedal" && [...url.searchParams].length === 1);
    expect(new URL(page.url()).search).toBe("?q=brake+pedal");
    await expect(page.locator("[data-search-count]")).toBeVisible();
    await expect(page.locator(`[data-search-hit="${REQ_KEY}"]`)).toBeVisible();
    await expect(page.locator(`[data-search-hit="${KEY}-R2"]`)).toHaveCount(0);

    const searchEntry = page.locator("[data-sidebar]").getByRole("link", { name: "Search", exact: true });
    await searchEntry.focus();
    await page.keyboard.press("ControlOrMeta+k");
    await expect(page.locator("[data-quick-switcher-input]")).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(palette).toHaveCount(0);
    await expect(searchEntry).toBeFocused();
  });

  test("bob's palette offers no settings place, and the Access page tells him it is not his", async ({ pageAs }) => {
    const bob = await pageAs("bob");
    await openShell(bob, "/");
    await bob.keyboard.press("ControlOrMeta+k");
    const palette = bob.getByRole("dialog", { name: "Go to" });
    await bob.locator("[data-quick-switcher-input]").fill("members");
    const everything = palette.locator('[data-quick-switcher-option="search"]');
    await expect(everything).toContainText("Search everything for members");
    await expect(palette.locator('[data-quick-switcher-option^="place:"]')).toHaveCount(0);
    // Requirements of projects open to everyone may match the word; nothing else may be offered beside them.
    const offered = await palette
      .locator("[data-quick-switcher-option]")
      .evaluateAll((each) => each.map((option) => option.getAttribute("data-quick-switcher-option")));
    expect(offered.at(-1)).toBe("search");
    expect(offered.slice(0, -1).every((key) => key?.startsWith("requirement:"))).toBe(true);
    await bob.keyboard.press("Escape");

    await openShell(bob, "/settings/members");
    await expect(bob).toHaveURL(`${WEB_URL}/admin/access/members`);
    const refusal = bob.locator("[data-admin-refused]");
    await expect(refusal).toContainText("Only administrators can open this page");
    await expect(refusal).toContainText("Ask an administrator of this installation to make the change, or to give you the role.");
    await expect(bob.locator("main").getByRole("heading", { level: 1, name: "Admin" })).toBeVisible();
    await expect(bob.locator("[data-member]")).toHaveCount(0);
  });

  test("Your profile shows who is signed in: bob a member, alice an administrator", async ({ page, pageAs, people }) => {
    const bob = await pageAs("bob");
    await openShell(bob, "/settings/profile");
    const bobs = bob.locator("[data-profile]");
    await expect(bobs.locator("[data-profile-name]")).toHaveText(people.bob.name);
    await expect(bobs).toContainText(people.bob.email);
    await expect(bobs.locator("[data-profile-standing]")).toHaveText("Member");

    await openShell(page, "/settings/profile");
    const alices = page.locator("[data-profile]");
    await expect(alices.locator("[data-profile-name]")).toHaveText(people.alice.name);
    await expect(alices).toContainText(people.alice.email);
    await expect(alices.locator("[data-profile-standing]")).toHaveText("MemberYou administer Exciter.");
  });
});

test.describe("the drawer at phone width", { tag: "@mobile" }, () => {
  test.afterEach(async ({ api }) => removeProject(api));

  test("its links close it, its close button and Escape hand focus back, and its theme button switches the palette", async ({ page, api }) => {
    await makeProject(api);
    await openShell(page, "/");
    const opener = page.locator('[data-action="drawer"]');
    const drawer = page.getByRole("dialog", { name: "Navigation" });

    await opener.click();
    await drawer
      .getByRole("navigation", { name: "Where you are" })
      .locator('[data-sidebar-group="requirements"]')
      .getByRole("link", { name: "Projects" })
      .click();
    await expect(drawer).toHaveCount(0);
    await expect(page).toHaveURL(`${WEB_URL}/projects`);
    await expect(opener).toBeFocused();
    await expect(page.locator(`[data-project-row="${KEY}"]`)).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);

    await opener.click();
    await expect(drawer).toBeVisible();
    await drawer.locator('[data-action="close-drawer"]').click();
    await expect(drawer).toHaveCount(0);
    await expect(opener).toBeFocused();

    await opener.click();
    await expect(drawer).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
    await expect(opener).toBeFocused();

    await opener.click();
    const theme = drawer.locator('[data-action="theme"]');
    await expect(page.locator("html")).not.toHaveAttribute("data-theme", /./);
    await theme.click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await theme.click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.keyboard.press("Escape");

    await openShell(page, REQUIREMENT);
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toContainText("The brake pedal stops the car");
    expect(await scrollsSideways(page)).toBe(false);
  });
});

test.describe("signing in keeps next inside the application", { tag: "@desktop" }, () => {
  test.use({ storageState: ANONYMOUS });

  for (const next of ["//evil.example/x", "https://evil.example/x"]) {
    test(`a password sign-in from ${LOGIN_PATH}?next=${next} lands on the home page`, async ({ page, people }) => {
      await page.goto(`${LOGIN_PATH}?next=${encodeURIComponent(next)}`);
      await page.getByLabel("Email", { exact: true }).fill(people.alice.email);
      await page.getByLabel("Password", { exact: true }).fill(people.alice.password);
      await page.locator('[data-action="password-sign-in"]').click();
      await expect(page).toHaveURL(`${WEB_URL}/`);
      await expect(page.locator("[data-home]")).toBeVisible();
    });
  }

  test("a sign-in turned away as switched off says so and what to do", async ({ page }) => {
    await page.goto(`${LOGIN_PATH}?sso=inactive`);
    await expect(page.locator("[data-login]").getByRole("alert")).toHaveText("Your account has been switched off. Ask an administrator.");
  });
});

test.describe("the sign-in page for somebody signed in", { tag: "@desktop" }, () => {
  for (const path of [LOGIN_PATH, `${LOGIN_PATH}?next=${encodeURIComponent("//evil.example/x")}`]) {
    test(`alice opening ${path} is sent home`, async ({ page }) => {
      await page.goto(path);
      await expect(page).toHaveURL(`${WEB_URL}/`);
      await expect(page.locator("[data-home]")).toBeVisible();
      await expect(page.locator("[data-login]")).toHaveCount(0);
    });
  }
});

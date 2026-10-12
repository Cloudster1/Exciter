import type { TestInfo } from "@playwright/test";
import type { components } from "../../web/src/api/schema";
import { createExciterApi, must, type ExciterApi } from "./api";
import { stateFile } from "./auth";

export type Theme = components["schemas"]["Theme"];
export type ThemeSpec = components["schemas"]["Spec"];

/** A name no other test, worker or earlier run has used, so tests can share an organization. */
export function uniqueName(testInfo: TestInfo, label: string): string {
  return `e2e ${testInfo.workerIndex}-${Date.now().toString(36)} ${label}`;
}

/** A theme that overrides nothing; pass what the test needs on top. */
export function themeSpec(overrides: Partial<ThemeSpec> = {}): ThemeSpec {
  return { colors: { light: {}, dark: {} }, fonts: {}, shape: {}, shadows: {}, cursors: {}, icons: {}, css: "", ...overrides };
}

export async function createTheme(api: ExciterApi, name: string, spec: ThemeSpec = themeSpec(), shared = false): Promise<Theme> {
  return must(await api.POST("/themes", { body: { name, shared, spec } })).theme;
}

export async function listThemes(api: ExciterApi): Promise<Theme[]> {
  return must(await api.GET("/themes")).themes;
}

/** Uses a theme, or with null goes back to whatever the organization shows. */
export async function chooseTheme(api: ExciterApi, themeId: string | null): Promise<void> {
  must(await api.PUT("/themes/active", { body: { themeId } }));
}

/** Leaves demo with no default theme and nothing chosen by alice or bob, and checks it: specs there judge the
 *  built-in look, contrast included, so a theme any run left behind must never reach them. */
export async function clearDemoThemes(): Promise<void> {
  const alice = await createExciterApi(stateFile("alice"));
  const bob = await createExciterApi(stateFile("bob"));
  try {
    must(await alice.PUT("/themes/default", { body: { themeId: null } }));
    for (const api of [alice, bob]) {
      await chooseTheme(api, null);
      const active = must(await api.GET("/themes/active"));
      if (active.theme !== null) throw new Error(`demo still shows the theme ${active.theme.name} after clearing it.`);
    }
  } finally {
    await alice.dispose();
    await bob.dispose();
  }
}

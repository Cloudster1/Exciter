import { describe, expect, it, vi } from "vitest";
import type * as ThemesApi from "@/api/themes";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Theme } from "@/api/themes";
import { emptySpec } from "@/lib/theme-css";
import { renderAt } from "@/test/app";
import { activeThemeMeta, inThemeView } from "./themes";

function theme(over: Partial<Theme>): Theme {
  return {
    id: "t1",
    ownerId: "me",
    ownerName: "Me",
    name: "Magenta",
    shared: false,
    active: false,
    default: false,
    inUse: 0,
    createdAt: "2026-09-29T08:00:00Z",
    updatedAt: "2026-09-29T08:00:00Z",
    spec: emptySpec(),
    assets: [],
    ...over,
  };
}

vi.mock("@/api/themes", async (importOriginal) => {
  const real = await importOriginal<typeof ThemesApi>();
  const idle = () => ({ mutate: vi.fn(), isPending: false, error: null });
  return {
    ...real,
    useActiveTheme: () => ({ data: { theme: null, source: "" } }),
    useThemes: () => ({
      data: { themes: [{ ...theme({ id: "s1", ownerId: "someone", ownerName: "Someone", name: "Shared One", shared: true, default: true, inUse: 3 }) }] },
      isLoading: false,
      error: null,
    }),
    useChooseTheme: idle,
    useSetDefaultTheme: idle,
    useImportTheme: idle,
    useUpdateTheme: idle,
    useDeleteTheme: idle,
  };
});

describe("the themes page", () => {
  it("sorts a theme into mine or shared with me", () => {
    expect(inThemeView(theme({}), "mine", "me")).toBe(true);
    expect(inThemeView(theme({}), "shared", "me")).toBe(false);
    expect(inThemeView(theme({ ownerId: "you", shared: true }), "shared", "me")).toBe(true);
    expect(inThemeView(theme({ ownerId: "you", shared: false }), "shared", "me")).toBe(false);
  });

  it("says which theme the reader sees and where it came from", () => {
    expect(activeThemeMeta(null, "", undefined)).toBe("You are using the built-in theme.");
    expect(activeThemeMeta(theme({}), "chosen", undefined)).toBe("You are using Magenta.");
    expect(activeThemeMeta(theme({}), "armature", undefined)).toBe("You are using Magenta, the theme you use in Armature.");
    expect(activeThemeMeta(null, "armature", theme({}))).toBe("You are using the built-in theme, as you do in Armature.");
    expect(activeThemeMeta(theme({}), "everyone", undefined)).toBe("You are using Magenta, the default for everyone.");
    expect(activeThemeMeta(null, "", theme({ name: "House" }))).toBe("You are using the built-in theme, over House, the default for everyone.");
  });

  it("lists what is shared, offers import, and is reached from the account menu", async () => {
    await renderAt("/");
    await screen.findByText("No projects yet");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Themes" }));
    expect(await screen.findByRole("heading", { name: "Themes" })).toBeInTheDocument();
    expect(screen.getByText("You are using the built-in theme, over Shared One, the default for everyone.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Import theme" })).toBeInTheDocument();
    expect(screen.getByLabelText("Theme file")).toHaveAttribute("accept", "application/json,.json");

    await userEvent.click(screen.getByRole("button", { name: "Shared with me" }));
    const row = await screen.findByText("Shared One");
    expect(row.closest("tr")).toHaveAttribute("data-theme-default", "true");
    expect(screen.getByText("3 people")).toBeInTheDocument();
  });

  it("names Themes in the breadcrumbs of a theme's editor, as the way back", async () => {
    await renderAt("/settings/themes/new");
    const crumbs = within(document.querySelector<HTMLElement>("[data-shell-strip]")!).getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByRole("link", { name: "Themes" })).toHaveAttribute("href", "/settings/themes");
    expect(within(crumbs).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/settings");
  });
});

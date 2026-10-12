import { beforeEach, describe, expect, it, vi } from "vitest";
import type * as ThemesApi from "@/api/themes";
import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { THEME_PREVIEW_DEBOUNCE_MS } from "@/config";
import { CUSTOM_THEME_STYLE_ID } from "@/lib/theme";
import { emptySpec } from "@/lib/theme-css";
import { renderAt } from "@/test/app";

// The server is stood in for at the hooks: what is under test is what the
// editor draws and what it asks to save.
const api = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  choose: vi.fn(),
  theme: undefined as unknown,
}));

vi.mock("@/api/themes", async (importOriginal) => {
  const real = await importOriginal<typeof ThemesApi>();
  const mutation = (mutate: unknown) => () => ({ mutate, isPending: false, error: null });
  const idle = () => ({ mutate: vi.fn(), isPending: false, error: null });
  return {
    ...real,
    useActiveTheme: () => ({ data: { theme: null, source: "" } }),
    useThemes: () => ({ data: { themes: [] }, isLoading: false, error: null }),
    useTheme: (id: string | undefined) => ({ data: id ? { theme: api.theme } : undefined, isLoading: false, error: null }),
    useThemeExamples: () => ({
      data: {
        examples: [
          {
            key: "deep-tech",
            name: "Deep-Tech",
            description: "Slate and glass.",
            spec: { ...real.emptySpec(), colors: { light: { accent: "#2f6fd6" }, dark: {} } },
          },
          {
            key: "constellation",
            name: "Constellation",
            description: "A network behind everything.",
            spec: { ...real.emptySpec(), effect: "constellation", colors: { light: {}, dark: { accent: "#4fd1c5" } } },
          },
        ],
      },
    }),
    useCreateTheme: mutation(api.create),
    useUpdateTheme: mutation(api.update),
    useChooseTheme: mutation(api.choose),
    useUploadThemeAsset: idle,
    useDeleteThemeAsset: idle,
  };
});

beforeEach(() => {
  api.create.mockReset();
  api.update.mockReset();
  api.choose.mockReset();
  document.getElementById(CUSTOM_THEME_STYLE_ID)?.remove();
});

describe("the theme editor", () => {
  it("starts a new theme from an example, named after it, and saves what was picked", async () => {
    await renderAt("/settings/themes/new");
    const save = await screen.findByRole("button", { name: "Save theme" });
    expect(save).toBeDisabled();

    const start = screen.getByRole("radiogroup", { name: "Start from" });
    expect(within(start).getByRole("radio", { name: /Blank/ })).toHaveAttribute("aria-checked", "true");
    await userEvent.click(within(start).getByRole("radio", { name: /Constellation/ }));
    expect(screen.getByLabelText("Theme name")).toHaveValue("Constellation");

    await userEvent.click(save);
    expect(api.create).toHaveBeenCalledTimes(1);
    const [input] = api.create.mock.calls[0]!;
    expect(input.name).toBe("Constellation");
    expect(input.shared).toBe(false);
    expect(input.spec.effect).toBe("constellation");
    expect(input.spec.colors.dark.accent).toBe("#4fd1c5");
  });

  it("changes a colour of one palette, and previews it on the page when asked", async () => {
    await renderAt("/settings/themes/new");
    // Pasted rather than typed: every keystroke redraws all 44 colour fields,
    // and on a busy machine the keystrokes alone outlasted the test's time.
    await userEvent.click(await screen.findByLabelText("Theme name"));
    await userEvent.paste("Magenta");
    await userEvent.click(screen.getByLabelText("Links, the current item"));
    await userEvent.paste("#ff0066");
    expect(screen.getByText("1 of 44 colours changed.")).toBeInTheDocument();

    // The preview waits for the draft to settle; the clock is moved past that
    // rather than waited out.
    vi.useFakeTimers();
    try {
      fireEvent.click(screen.getByRole("switch", { name: "Preview on this page" }));
      expect(document.getElementById(CUSTOM_THEME_STYLE_ID)).toBeNull();
      act(() => vi.advanceTimersByTime(THEME_PREVIEW_DEBOUNCE_MS));
      expect(document.getElementById(CUSTOM_THEME_STYLE_ID)?.textContent).toContain("--color-accent: #ff0066;");
    } finally {
      vi.useRealTimers();
    }

    await userEvent.click(screen.getByRole("button", { name: "Save theme" }));
    const [input] = api.create.mock.calls[0]!;
    expect(input.spec.colors).toEqual({ light: { accent: "#ff0066" }, dark: {} });
  });

  it("opens a saved theme with its files, and marks the ones the theme uses", async () => {
    api.theme = {
      id: "11111111-1111-1111-1111-111111111111",
      ownerId: "u",
      ownerName: "Owner",
      name: "Blocks",
      shared: true,
      active: false,
      default: false,
      inUse: 0,
      createdAt: "2026-09-29T08:00:00Z",
      updatedAt: "2026-09-29T08:00:00Z",
      spec: { ...emptySpec(), icons: { home: { assetId: "a1" } } },
      assets: [
        { id: "a1", name: "block.svg", contentType: "image/svg+xml", size: 2048, createdAt: "2026-09-29T08:00:00Z" },
        { id: "a2", name: "face.woff2", contentType: "font/woff2", size: 40960, createdAt: "2026-09-29T08:00:00Z" },
      ],
    };
    await renderAt("/settings/themes/11111111-1111-1111-1111-111111111111");
    expect(await screen.findByLabelText("Theme name")).toHaveValue("Blocks");
    expect(screen.queryByRole("radiogroup", { name: "Start from" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Shared with the organization" })).toHaveAttribute("aria-checked", "true");

    await userEvent.click(screen.getByRole("tab", { name: "Files" }));
    const used = document.querySelector('[data-theme-asset="block.svg"]') as HTMLElement;
    expect(within(used).getByText("In use")).toBeInTheDocument();
    expect(within(used).getByRole("button", { name: "Remove block.svg" })).toBeDisabled();
    expect(within(document.querySelector('[data-theme-asset="face.woff2"]') as HTMLElement).getByRole("button", { name: "Remove face.woff2" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "Use this theme" }));
    expect(api.choose).toHaveBeenCalledWith("11111111-1111-1111-1111-111111111111", expect.anything());
    await userEvent.click(screen.getByRole("button", { name: "Save theme" }));
    expect(api.update.mock.calls[0]![0]).toMatchObject({ id: "11111111-1111-1111-1111-111111111111", name: "Blocks", shared: true });
  });
});

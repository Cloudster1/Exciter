import { beforeEach, describe, expect, it, vi } from "vitest";
import type * as ThemesApi from "@/api/themes";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Theme, ThemeSpec } from "@/api/themes";
import { KILOBYTE, THEME_ASSET_MAX_BYTES } from "@/config";
import { emptySpec } from "@/lib/theme-css";
import { renderAt } from "@/test/app";

// The server is stood in for at the hooks, as in ThemeEditor.test.tsx: what is under test is what each tab puts in
// the spec "Save theme" sends, and what the Files tab asks the server to do.
const api = vi.hoisted(() => ({
  update: vi.fn(),
  upload: vi.fn(),
  remove: vi.fn(),
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
    useThemeExamples: () => ({ data: { examples: [] } }),
    useCreateTheme: idle,
    useUpdateTheme: mutation(api.update),
    useChooseTheme: idle,
    useUploadThemeAsset: mutation(api.upload),
    useDeleteThemeAsset: mutation(api.remove),
  };
});

const THEME_ID = "22222222-2222-2222-2222-222222222222";
const PICTURE = { id: "p1", name: "tile.png", contentType: "image/png", size: 3 * KILOBYTE, createdAt: "2026-09-29T08:00:00Z" };
const FONT = { id: "f1", name: "face.woff2", contentType: "font/woff2", size: 40 * KILOBYTE, createdAt: "2026-09-29T08:00:00Z" };

function aTheme(spec: Partial<ThemeSpec> = {}): Theme {
  return {
    id: THEME_ID,
    ownerId: "u",
    ownerName: "Owner",
    name: "Parts",
    shared: false,
    active: true,
    default: false,
    inUse: 1,
    createdAt: "2026-09-29T08:00:00Z",
    updatedAt: "2026-09-29T08:00:00Z",
    spec: { ...emptySpec(), ...spec },
    assets: [PICTURE, FONT],
  };
}

async function openTab(name: string) {
  await renderAt(`/settings/themes/${THEME_ID}`);
  await screen.findByLabelText("Theme name");
  await userEvent.click(screen.getByRole("tab", { name }));
}

/** Presses Save theme and answers the spec it sent. */
async function saved(): Promise<ThemeSpec> {
  await userEvent.click(screen.getByRole("button", { name: "Save theme" }));
  expect(api.update).toHaveBeenCalledTimes(1);
  return api.update.mock.calls[0]![0].spec as ThemeSpec;
}

beforeEach(() => {
  api.update.mockReset();
  api.upload.mockReset();
  api.remove.mockReset();
  api.theme = aTheme();
});

describe("each tab of the theme editor", () => {
  it("Colours: resetting the only colour changed takes it out of the spec", async () => {
    await openTab("Colours");
    await userEvent.click(screen.getByLabelText("Links, the current item"));
    await userEvent.paste("#ff0066");
    expect(screen.getByText("1 of 44 colours changed.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Reset Links, the current item" }));
    // With none changed the count gives way to the sentence that says so.
    expect(screen.getByText("Nothing changed yet: every colour is the stylesheet's.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset Links, the current item" })).toBeNull();
    expect((await saved()).colors).toEqual({ light: {}, dark: {} });
  });

  it("Shape: sets the control radius and a shadow", async () => {
    await openTab("Shape");
    await userEvent.type(screen.getByLabelText("Control radius"), "4");
    await userEvent.click(screen.getByLabelText("Raised"));
    await userEvent.paste("0 1px 2px #000");
    const spec = await saved();
    expect(spec.shape).toEqual({ radiusControl: 4 });
    expect(spec.shadows).toEqual({ "1": "0 1px 2px #000" });
  });

  it("Type: names the text face and gives it the uploaded font", async () => {
    await openTab("Type");
    await userEvent.click(screen.getByLabelText("Text family"));
    await userEvent.paste("House Sans");
    // Only fonts are offered for a face, never the theme's pictures.
    const file = screen.getByRole("combobox", { name: "Text family font file" });
    expect(within(file).queryByRole("option", { name: PICTURE.name })).toBeNull();
    await userEvent.selectOptions(file, FONT.id);
    const spec = await saved();
    expect(spec.fonts).toEqual({ sans: { family: "House Sans", assetId: FONT.id } });
  });

  it("Cursors: gives the pointer a picture and the point it clicks from", async () => {
    await openTab("Cursors");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Pointer cursor picture" }), PICTURE.id);
    const pointer = document.querySelector<HTMLElement>('[data-theme-cursor="pointer"]')!;
    await userEvent.type(within(pointer).getByLabelText("Point x"), "3");
    await userEvent.type(within(pointer).getByLabelText("Point y"), "5");
    expect((await saved()).cursors).toEqual({ pointer: { assetId: PICTURE.id, hotspotX: 3, hotspotY: 5 } });
  });

  it("Icons: replaces one glyph with path data, one path a line", async () => {
    await openTab("Icons");
    await userEvent.type(screen.getByLabelText("Find an icon"), "home");
    await userEvent.click(screen.getByLabelText("home path data"));
    await userEvent.paste("M2 8h12\nM8 2v12");
    expect((await saved()).icons).toEqual({ home: { paths: ["M2 8h12", "M8 2v12"] } });
  });

  it("Backdrop: lays a picture behind the content, covering, then tiled", async () => {
    await openTab("Backdrop");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Backdrop picture" }), PICTURE.id);
    expect(screen.getByRole("button", { name: "Cover" })).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(screen.getByRole("button", { name: "Tile" }));
    expect((await saved()).backdrop).toEqual({ assetId: PICTURE.id, fit: "tile" });
  });

  it("Advanced: keeps the extra CSS and shows how much of the limit it takes", async () => {
    const css = `[data-rail] { opacity: .9 } /* ${"x".repeat(2 * KILOBYTE)} */`;
    await openTab("Advanced");
    expect(screen.getByText(/0 of 32 KB\.$/)).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText("Extra CSS"));
    await userEvent.paste(css);
    expect(screen.getByText(/2 of 32 KB\.$/)).toBeInTheDocument();
    expect((await saved()).css).toBe(css);
  });
});

describe("the Files tab", () => {
  const input = () => document.querySelector<HTMLInputElement>("[data-theme-file-input]")!;

  it("asks to save a new theme before it takes files", async () => {
    await renderAt("/settings/themes/new");
    await userEvent.click(await screen.findByRole("tab", { name: "Files" }));
    expect(screen.getByText("Save the theme first; then pictures and fonts can be added to it here.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add a file" })).toBeNull();
  });

  it("refuses a file over the limit without sending it, and says when one was added", async () => {
    await openTab("Files");
    const huge = new File(["x"], "huge.png", { type: "image/png" });
    Object.defineProperty(huge, "size", { value: THEME_ASSET_MAX_BYTES + 1 });
    fireEvent.change(input(), { target: { files: [huge] } });
    expect(await screen.findByText("That file is over 12 MB. Make it smaller and try again.")).toBeInTheDocument();
    expect(api.upload).not.toHaveBeenCalled();

    api.upload.mockImplementation((_: unknown, { onSuccess }: { onSuccess: (made: unknown) => void }) => onSuccess({ asset: { ...PICTURE, name: "new.png" } }));
    const small = new File(["png"], "new.png", { type: "image/png" });
    fireEvent.change(input(), { target: { files: [small] } });
    expect(api.upload).toHaveBeenCalledWith({ id: THEME_ID, file: small }, expect.anything());
    expect(await screen.findByText("Added new.png.")).toBeInTheDocument();
    expect(screen.queryByText("That file is over 12 MB. Make it smaller and try again.")).toBeNull();
  });

  it("asks before it removes a file, and a no removes nothing", async () => {
    await openTab("Files");
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    api.remove.mockImplementation((_: unknown, { onSuccess }: { onSuccess: () => void }) => onSuccess());
    try {
      const remove = within(document.querySelector<HTMLElement>('[data-theme-asset="tile.png"]')!).getByRole("button", { name: "Remove tile.png" });
      await userEvent.click(remove);
      expect(confirm).toHaveBeenLastCalledWith("Remove tile.png from the theme's files?");
      expect(api.remove).not.toHaveBeenCalled();
      await userEvent.click(remove);
      expect(api.remove).toHaveBeenCalledWith({ id: THEME_ID, assetId: PICTURE.id }, expect.anything());
      expect(await screen.findByText("Removed tile.png.")).toBeInTheDocument();
    } finally {
      confirm.mockRestore();
    }
  });
});

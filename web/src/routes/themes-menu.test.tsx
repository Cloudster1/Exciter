import { beforeEach, describe, expect, it, vi } from "vitest";
import type * as ThemesApi from "@/api/themes";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Theme } from "@/api/themes";
import { emptySpec } from "@/lib/theme-css";
import { renderAt, signedIn } from "@/test/app";

// The server is stood in for at the hooks: what is under test is what each row's menu offers its reader and asks
// the server to do.
const api = vi.hoisted(() => ({
  themes: [] as unknown[],
  setDefault: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  choose: vi.fn(),
}));

vi.mock("@/api/themes", async (importOriginal) => {
  const real = await importOriginal<typeof ThemesApi>();
  const mutation = (mutate: unknown) => () => ({ mutate, isPending: false, error: null });
  return {
    ...real,
    useActiveTheme: () => ({ data: { theme: null, source: "" } }),
    useThemes: () => ({ data: { themes: api.themes }, isLoading: false, error: null }),
    useChooseTheme: mutation(api.choose),
    useSetDefaultTheme: mutation(api.setDefault),
    useImportTheme: () => ({ mutate: vi.fn(), isPending: false, error: null }),
    useUpdateTheme: mutation(api.update),
    useDeleteTheme: mutation(api.remove),
  };
});

const ME = signedIn.user.id;
const member = { ...signedIn, role: "member" as const };

function theme(over: Partial<Theme>): Theme {
  return {
    id: "t1",
    ownerId: ME,
    ownerName: "Ada Lovelace",
    name: "Mine",
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

const rowOf = (name: string) => document.querySelector<HTMLElement>(`[data-theme-row="${name}"]`)!;

/** Opens a row's menu and answers its items by label. */
async function menuOf(name: string) {
  await userEvent.click(within(rowOf(name)).getByRole("button", { name: `Actions for ${name}` }));
  const menu = await screen.findByRole("menu");
  return (label: string) => within(menu).getByRole("menuitem", { name: label });
}

beforeEach(() => {
  for (const each of [api.setDefault, api.update, api.remove, api.choose]) each.mockReset();
});

describe("a theme's row menu", () => {
  it("gives the owner Edit, Share and Delete, and the name leads to the editor", async () => {
    api.themes = [theme({})];
    await renderAt("/settings/themes", { me: member });
    expect(within(rowOf("Mine")).getByRole("link", { name: "Mine" })).toHaveAttribute("href", "/settings/themes/t1");
    const item = await menuOf("Mine");
    for (const label of ["Edit", "Share with everyone", "Delete"]) expect(item(label)).not.toHaveAttribute("aria-disabled");
    // A member names no default, whosever the theme.
    expect(screen.queryByRole("menuitem", { name: "Make it the default for everyone" })).toBeNull();
    await userEvent.click(item("Share with everyone"));
    expect(api.update).toHaveBeenCalledWith({ id: "t1", shared: true }, expect.anything());
  });

  it("gives a member nothing to change on another person's shared theme, and no link to its editor", async () => {
    api.themes = [theme({ id: "s1", ownerId: "someone", ownerName: "Someone", name: "Theirs", shared: true })];
    await renderAt("/settings/themes", { me: member });
    await userEvent.click(screen.getByRole("button", { name: "Shared with me" }));
    expect(within(rowOf("Theirs")).queryByRole("link")).toBeNull();
    expect(within(rowOf("Theirs")).getByText("Theirs")).toBeInTheDocument();
    const item = await menuOf("Theirs");
    for (const label of ["Edit", "Stop sharing", "Delete"]) expect(item(label)).toHaveAttribute("aria-disabled", "true");
    expect(item("Use this theme")).not.toHaveAttribute("aria-disabled");
    await userEvent.click(item("Delete"));
    expect(api.remove).not.toHaveBeenCalled();
  });

  it("offers an administrator the default only on a shared theme, and takes it off the default", async () => {
    api.themes = [
      theme({ id: "p1", name: "Private" }),
      theme({ id: "s1", name: "Shared", shared: true }),
      theme({ id: "d1", name: "House", shared: true, default: true }),
    ];
    await renderAt("/settings/themes");
    const onPrivate = await menuOf("Private");
    expect(onPrivate("Share with everyone")).not.toHaveAttribute("aria-disabled");
    expect(screen.queryByRole("menuitem", { name: "Make it the default for everyone" })).toBeNull();
    await userEvent.keyboard("{Escape}");

    const onShared = await menuOf("Shared");
    expect(screen.queryByRole("menuitem", { name: "No longer the default for everyone" })).toBeNull();
    await userEvent.click(onShared("Make it the default for everyone"));
    expect(api.setDefault).toHaveBeenLastCalledWith("s1", expect.anything());

    const onDefault = await menuOf("House");
    expect(screen.queryByRole("menuitem", { name: "Make it the default for everyone" })).toBeNull();
    await userEvent.click(onDefault("No longer the default for everyone"));
    expect(api.setDefault).toHaveBeenLastCalledWith(null, expect.anything());
  });

  it("names in Delete's question how many use the theme, and a cancelled one deletes nothing", async () => {
    api.themes = [theme({ inUse: 3 }), theme({ id: "t2", name: "Lone", inUse: 1 })];
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    try {
      await renderAt("/settings/themes", { me: member });
      await userEvent.click((await menuOf("Mine"))("Delete"));
      expect(confirm).toHaveBeenLastCalledWith("Delete Mine and its files for good? The 3 people using it go back to the built-in theme.");
      expect(api.remove).not.toHaveBeenCalled();
      await userEvent.click((await menuOf("Lone"))("Delete"));
      expect(confirm).toHaveBeenLastCalledWith("Delete Lone and its files for good? The one person using it goes back to the built-in theme.");
      expect(api.remove).toHaveBeenCalledWith("t2", expect.anything());
    } finally {
      confirm.mockRestore();
    }
  });
});

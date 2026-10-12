import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DEFAULT_THEME_PATH } from "@/config";
import { emptySpec } from "@/lib/theme-css";
import { renderAt, signedIn, stubApi } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

function theme(id: string, name: string, over: Record<string, unknown> = {}) {
  return {
    id,
    ownerId: "0195f000-0000-7000-8000-00000000000b",
    ownerName: "Bob Builder",
    name,
    shared: true,
    spec: emptySpec(),
    assets: [],
    inUse: 0,
    active: false,
    default: false,
    createdAt: "2026-09-29T08:00:00Z",
    updatedAt: "2026-09-29T08:00:00Z",
    ...over,
  };
}

const blocks = theme("0195f000-0000-7000-8000-0000000000e1", "Blocks");
const mine = theme("0195f000-0000-7000-8000-0000000000e2", "Mine alone", { shared: false, ownerId: signedIn.user.id });

describe("the organization's default theme", () => {
  it("offers the built-in theme and the shared ones, and names the one picked", async () => {
    const sent = stubApi({
      "GET /themes": { status: 200, body: { themes: [blocks, mine] } },
      "PUT /themes/default": { status: 200, body: { theme: { ...blocks, default: true } } },
    });
    await renderAt(DEFAULT_THEME_PATH);
    const builtIn = await screen.findByRole("radio", { name: /Built-in theme/ });
    expect(builtIn).toHaveAttribute("aria-checked", "true");
    expect(screen.queryByRole("radio", { name: /Mine alone/ })).toBeNull();

    await userEvent.click(screen.getByRole("radio", { name: /Blocks/ }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT" && r.path === "/themes/default")).toBe(true));
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ themeId: blocks.id });
    expect(await screen.findByRole("status")).toHaveTextContent("Blocks is what everybody sees until they choose.");
  });

  it("says where to share one when nothing is shared yet", async () => {
    stubApi({ "GET /themes": { status: 200, body: { themes: [mine] } } });
    await renderAt(DEFAULT_THEME_PATH);
    expect(await screen.findByText(/Nobody has shared a theme yet/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Themes" })).toHaveAttribute("href", "/settings/themes");
  });

  it("tells a member the choice is the administrators'", async () => {
    const sent = stubApi({});
    await renderAt(DEFAULT_THEME_PATH, { me: { ...signedIn, organization: { ...signedIn.organization!, role: "member" } } });
    expect(await screen.findByText(/Only an owner or administrator of this organization names its default theme/)).toBeInTheDocument();
    expect(sent.filter((r) => r.path === "/themes")).toHaveLength(0);
  });
});

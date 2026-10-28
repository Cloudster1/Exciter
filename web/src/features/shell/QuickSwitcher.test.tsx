import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuickHit } from "@/api/search";
import { SEARCH_PATH } from "@/config";
import { t } from "@/i18n";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const hit = (key: string, title: string): QuickHit => ({
  requirement: {
    id: `0195f000-0000-7000-8000-0000000002${key.replace(/\D/g, "").slice(-2).padStart(2, "0")}`,
    key,
    projectKey: "BRK",
    kind: "requirement",
    title,
    revision: 1,
    status: { id: "0195f000-0000-7000-8000-000000000b01", name: "Draft", category: "draft" },
    trashed: false,
  },
  projectName: "Brakes",
  outline: "1",
});

const found = (...hits: QuickHit[]): Answer => ({ status: 200, body: { requirements: hits } });
const options = () =>
  within(screen.getByRole("listbox"))
    .getAllByRole("option")
    .map((option) => option.getAttribute("data-quick-switcher-option"));

async function openPalette() {
  await userEvent.keyboard("{Control>}k{/Control}");
  return screen.getByRole("combobox");
}

describe("the palette when the quick search goes wrong", () => {
  it("says the quick search is not answering, and still offers the full search", async () => {
    const sent = stubApi({
      "GET /search/quick": { status: 500, body: { error: { code: "internal", message: "Something went wrong on our side." } } },
    });
    await renderAt("/");
    await userEvent.type(await openPalette(), "brake");
    const error = await waitFor(() => {
      const shown = document.querySelector("[data-quick-switcher-error]");
      expect(shown).not.toBeNull();
      return shown!;
    });
    expect(error).toHaveTextContent(t.search.quickFailed);
    expect(error).toHaveTextContent("Quick search is not answering. Press Enter on the last option to try the full search.");
    expect(sent.some((each) => each.path === "/search/quick")).toBe(true);
    expect(options()).toEqual(["search"]);
  });

  it("does not offer an answer to an earlier word once the word has changed", async () => {
    const waiting = new Map<string, (answer: Answer) => void>();
    const asked: string[] = [];
    stubApi({
      "GET /search/quick": (request) => {
        const q = new URL(request.url).searchParams.get("q") ?? "";
        asked.push(q);
        if (q === "bra") return found(hit("BRK-R1", "Brace the caliper"));
        return new Promise<Answer>((resolve) => waiting.set(q, resolve));
      },
    });
    await renderAt("/");
    const input = await openPalette();
    await userEvent.type(input, "bra");
    await screen.findByRole("option", { name: /BRK-R1 Brace the caliper/ });

    await userEvent.type(input, "ke");
    await waitFor(() => expect(asked).toContain("brake"));
    // The answer for "brake" is still on its way: what came back for "bra" is not offered for it.
    expect(options()).toEqual(["search"]);

    waiting.get("brake")!(found(hit("BRK-R2", "Brake within 100 ms")));
    await screen.findByRole("option", { name: /BRK-R2 Brake within 100 ms/ });
    expect(options()).toEqual([`requirement:${hit("BRK-R2", "").requirement.id}`, "search"]);
  });
});

describe("the palette from the pointer", () => {
  it("goes to the full search when Search everything is picked with the mouse", async () => {
    stubApi({ "GET /search/quick": found() });
    const router = await renderAt("/");
    await userEvent.type(await openPalette(), "brake pedal");
    const everything = screen.getByRole("option", { name: /Search everything for brake pedal/ });
    await userEvent.click(everything);
    await arrival(router, SEARCH_PATH);
    expect(router.state.location.search).toEqual({ q: "brake pedal" });
    expect(screen.queryByRole("dialog", { name: t.switcher.title })).toBeNull();
  });

  it("closes on a press on the scrim, and goes nowhere", async () => {
    const router = await renderAt("/");
    await openPalette();
    const dialog = screen.getByRole("dialog", { name: t.switcher.title });
    await userEvent.click(dialog.parentElement!);
    expect(screen.queryByRole("dialog", { name: t.switcher.title })).toBeNull();
    expect(router.state.location.pathname).toBe("/");
  });

  it("stays open on a press inside the dialog", async () => {
    await renderAt("/");
    await openPalette();
    const dialog = screen.getByRole("dialog", { name: t.switcher.title });
    await userEvent.click(dialog);
    expect(screen.getByRole("dialog", { name: t.switcher.title })).toBeInTheDocument();
  });
});

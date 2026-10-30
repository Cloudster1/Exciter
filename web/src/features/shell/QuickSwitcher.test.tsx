import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuickHit } from "@/api/search";
import { RECENT_STORAGE_KEY, SEARCH_PATH } from "@/config";
import { t } from "@/i18n";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { aProject } from "@/test/projects";

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
  within(screen.getByRole("dialog", { name: t.switcher.title }))
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

describe("the palette's groups", () => {
  it("reads each kind out as a group of its own, the projects and actions among them", async () => {
    stubApi({
      "GET /search/quick": found(hit("BRK-R1", "Brake within 100 ms")),
      "GET /projects": { status: 200, body: { projects: [aProject({ key: "BRK", name: "Brakes" })] } },
    });
    await renderAt("/");
    await userEvent.type(await openPalette(), "br");
    await screen.findByRole("option", { name: /BRK-R1 Brake within 100 ms/ });
    await screen.findByRole("option", { name: /^Brakes/ });
    expect(screen.getByRole("listbox", { name: "Requirements" })).toContainElement(screen.getByRole("option", { name: /BRK-R1/ }));
    expect(screen.getByRole("listbox", { name: "Projects" })).toContainElement(screen.getByRole("option", { name: /^Brakes/ }));
    expect(screen.getByRole("listbox", { name: "Search" })).toContainElement(screen.getByRole("option", { name: /Search everything for br/ }));
  });

  it("offers what can be done, by its words", async () => {
    const sent = stubApi({ "GET /search/quick": found(), "POST /notifications/read": { status: 204 } });
    await renderAt("/");
    await userEvent.type(await openPalette(), "mark all");
    const action = await screen.findByRole("option", { name: /Mark all notifications as read/ });
    expect(screen.getByRole("listbox", { name: "Actions" })).toContainElement(action);
    await userEvent.click(action);
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === "/notifications/read")).toBe(true));
  });

  it("offers the requirements opened last before anything is typed", async () => {
    localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify([{ id: "r1", key: "BRK-R7", title: "Hold on a slope", projectKey: "BRK" }]));
    try {
      await renderAt("/");
      await openPalette();
      const recent = screen.getByRole("listbox", { name: "Recently opened" });
      expect(within(recent).getByRole("option")).toHaveTextContent("BRK-R7 Hold on a slope");
      expect(options()[0]).toBe("recent:r1");
    } finally {
      localStorage.removeItem(RECENT_STORAGE_KEY);
    }
  });
});

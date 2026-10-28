import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { StatorAccount, StatorPageHit } from "@/api/stator";
import { STATOR_SEARCH_DEBOUNCE_MS } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

// The page picker of the link form (#41), driven by the keyboard as a combobox is.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://stator.example.com";
const account: StatorAccount = {
  configured: true,
  source: "token",
  signIn: null,
  baseUrl: BASE,
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000c1", name: "Bob", email: "bob@acme.stator.test" },
  checkedAt: "2026-10-01T09:00:00Z",
};

function aHit(n: number, title: string): StatorPageHit {
  const id = `0195f000-0000-7000-8000-00000000a00${n}`;
  return { id, title, spaceKey: "ENG", spaceName: "Engineering", path: [], url: `${BASE}/s/ENG/p/${id}` };
}

const hits = [aHit(1, "Brake timing"), aHit(2, "Braking system"), aHit(3, "Brake wear")];

function stubForm() {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement() } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /stator/account": { status: 200, body: { account } },
    "GET /requirements/CHK-R1/stator-links": { status: 200, body: { links: [] } },
    "GET /stator/search": { status: 200, body: { status: "ok", pages: hits } },
  });
}

async function openForm() {
  const user = userEvent.setup();
  await renderAt("/p/CHK/r/CHK-R1");
  const form = await screen.findByRole("form", { name: "Link a Stator page" });
  return { user, form, box: within(form).getByRole("combobox", { name: "Page" }) };
}

function selected(form: HTMLElement): string | null {
  return within(within(form).getByRole("listbox", { name: "Stator pages", hidden: true })).queryByRole("option", { selected: true })?.textContent ?? null;
}

describe("the page picker", () => {
  it("moves through the pages with the arrows and picks with Enter, without linking", async () => {
    const sent = stubForm();
    const { user, form, box } = await openForm();
    await user.type(box, "brak");
    await within(form).findByRole("option", { name: /Brake wear/ });
    expect(selected(form)).toContain("Brake timing");
    await user.keyboard("{ArrowDown}");
    expect(selected(form)).toContain("Braking system");
    await user.keyboard("{ArrowDown}{ArrowDown}");
    expect(selected(form)).toContain("Brake wear");
    await user.keyboard("{ArrowUp}");
    expect(selected(form)).toContain("Braking system");
    const active = within(within(form).getByRole("listbox", { name: "Stator pages" })).getByRole("option", { selected: true });
    expect(box).toHaveAttribute("aria-activedescendant", active.id);

    await user.keyboard("{Enter}");
    expect(form.querySelector(`[data-stator-picked="${hits[1]!.id}"]`)).toHaveTextContent("Braking system");
    expect(within(form).queryByRole("combobox", { name: "Page" })).toBeNull();
    expect(sent.some((r) => r.method === "POST")).toBe(false);

    await user.click(within(form).getByRole("button", { name: "Pick another" }));
    expect(within(form).getByRole("combobox", { name: "Page" })).toHaveValue("");
    expect(form.querySelector("[data-stator-picked]")).toBeNull();
  });

  it("closes the list on Escape and leaves the words typed", async () => {
    stubForm();
    const { user, form, box } = await openForm();
    await user.type(box, "brak");
    const list = await within(form).findByRole("listbox", { name: "Stator pages" });
    await within(form).findByRole("option", { name: /Brake timing/ });
    expect(list).toBeVisible();
    await user.keyboard("{Escape}");
    expect(list).not.toBeVisible();
    expect(box).toHaveAttribute("aria-expanded", "false");
    expect(box).toHaveValue("brak");
    expect(screen.getByRole("form", { name: "Link a Stator page" })).toBeInTheDocument();
  });

  it("asks Stator once for words typed quickly, after the pause", async () => {
    const sent = stubForm();
    const { user, form, box } = await openForm();
    await user.type(box, "brake tim");
    await within(form).findByRole("option", { name: /Brake timing/ });
    // Wait past another pause, so a search sent late would be counted too.
    await new Promise((resolve) => setTimeout(resolve, 2 * STATOR_SEARCH_DEBOUNCE_MS));
    expect(sent.filter((r) => r.path === "/stator/search")).toHaveLength(1);
  });
});

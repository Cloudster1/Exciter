import { describe, expect, it } from "vitest";
import { render, within } from "@testing-library/react";
import type { StatorPage, StatorPages, StatorStatus } from "@/api/stator";
import { STATOR_ACCOUNT_PATH } from "@/config";
import { PageChip, pageChipState, type PageChipState } from "./PageChip";

// What a page's chip shows for each answer of the lookup (#41): the page, or its space with why there is no more.

const TIMING = "0195f000-0000-7000-8000-00000000a001";
const HREF = `https://stator.example.com/s/ENG/p/${TIMING}`;
const timing: StatorPage = {
  id: TIMING,
  title: "Brake timing",
  spaceKey: "ENG",
  spaceName: "Engineering",
  path: ["Engineering home", "Braking system"],
  version: 2,
  url: HREF,
  updatedAt: "2026-10-01T09:00:00Z",
  updatedByName: "Alice",
};

function lookup(status: StatorStatus | undefined, pages: [string, StatorPage | null][] = []): StatorPages {
  return { status, pages: new Map(pages) };
}

function chip(state: PageChipState) {
  const { container } = render(<PageChip pageId={TIMING} href={HREF} spaceKey="ENG" version={3} state={state} />);
  return container.querySelector<HTMLElement>(`[data-stator-page="${TIMING}"]`)!;
}

describe("pageChipState", () => {
  it.each<[string, StatorPages, boolean, PageChipState["kind"]]>([
    ["a viewer without a token", lookup("ok", [[TIMING, timing]]), false, "connect"],
    ["Stator not connected", lookup("not_configured"), true, "connect"],
    ["a token not stored", lookup("not_connected"), true, "connect"],
    ["a token Stator refused", lookup("rejected"), true, "connect"],
    ["Stator not answering", lookup("unreachable"), true, "unreachable"],
    ["an id not answered yet", lookup(undefined), true, "loading"],
    ["an id another lookup answered", lookup("ok", [["0195f000-0000-7000-8000-00000000a002", null]]), true, "loading"],
    ["a page the viewer may not read", lookup("ok", [[TIMING, null]]), true, "hidden"],
    ["a page the viewer reads", lookup("ok", [[TIMING, timing]]), true, "page"],
  ])("gives %s the %s state", (_, found, connected, kind) => {
    expect(pageChipState(TIMING, found, connected).kind).toBe(kind);
  });

  it("hands the page it found to the chip", () => {
    expect(pageChipState(TIMING, lookup("ok", [[TIMING, timing]]), true)).toEqual({ kind: "page", page: timing });
  });
});

describe("PageChip", () => {
  it("draws the page as the viewer reads it", () => {
    const drawn = chip({ kind: "page", page: timing });
    expect(drawn).toHaveAttribute("data-state", "page");
    expect(drawn).toHaveAttribute("href", HREF);
    expect(drawn).toHaveTextContent("Brake timing");
    expect(drawn).toHaveTextContent("Engineering home / Braking system");
    expect(drawn).toHaveTextContent("Version 2");
  });

  it("sends a viewer without a token to connect, with the space and the stored version", () => {
    const drawn = chip({ kind: "connect" });
    expect(drawn).toHaveAttribute("data-state", "connect");
    expect(within(drawn).getByRole("link", { name: "ENG Page in Stator" })).toHaveAttribute("href", HREF);
    expect(drawn).toHaveTextContent("Version 3");
    expect(within(drawn).getByRole("link", { name: "Connect your Stator account to see this page" })).toHaveAttribute("href", STATOR_ACCOUNT_PATH);
  });

  it("says Stator did not answer, and offers no connect", () => {
    const drawn = chip({ kind: "unreachable" });
    expect(drawn).toHaveTextContent("Stator did not answer");
    expect(within(drawn).queryByRole("link", { name: /Connect your Stator account/ })).toBeNull();
  });

  it("shows the space alone while the page loads", () => {
    const drawn = chip({ kind: "loading" });
    expect(drawn).toHaveAttribute("data-state", "loading");
    expect(within(drawn).getByRole("link", { name: "ENG Page in Stator" })).toBeInTheDocument();
    expect(drawn).not.toHaveTextContent("Stator did not answer");
    expect(drawn).not.toHaveTextContent("Not available to you in Stator");
    expect(within(drawn).queryByRole("link", { name: /Connect your Stator account/ })).toBeNull();
  });

  it("says a page the viewer may not read is not available to them", () => {
    expect(chip({ kind: "hidden" })).toHaveTextContent("Not available to you in Stator");
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Watch } from "@/api/watching";
import { WATCHES_PAGE_SIZE } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { aProject } from "@/test/projects";
import { t } from "@/i18n";

// The edges of watching (#312): the watching list a page at a time, stopping the last watch of a later page, and
// saying so when a stop or a project watch fails.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const broken = { status: 500, body: { error: { code: "internal", message: "Broken." } } };

/** A project watch on a key of its own, the nth. */
function aWatch(n: number): Watch {
  return {
    id: `0195f000-0000-7000-8000-${String(n).padStart(12, "0")}`,
    kind: "project",
    projectKey: `P${n}`,
    requirement: null,
    createdAt: "2026-10-01T08:00:00Z",
  };
}

/** The watching list's API over a set of watches the test may shrink. */
function stubWatches(watches: Watch[], more: Record<string, Answer | (() => Answer)> = {}) {
  const asked: number[] = [];
  const sent = stubApi({
    "GET /watches": (request) => {
      const offset = Number(new URL(request.url).searchParams.get("offset") ?? 0);
      asked.push(offset);
      return { status: 200, body: { watches: watches.slice(offset, offset + WATCHES_PAGE_SIZE), total: watches.length, limit: WATCHES_PAGE_SIZE, offset } };
    },
    ...more,
  });
  return { sent, asked };
}

describe("the watching list, a page at a time", () => {
  it("walks to the next page and back", async () => {
    const watches = Array.from({ length: WATCHES_PAGE_SIZE + 5 }, (_, i) => aWatch(i));
    stubWatches(watches);
    await renderAt("/settings/watching");
    expect(await screen.findByText(t.watch.range(1, WATCHES_PAGE_SIZE, watches.length))).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: t.watch.next }));
    expect(await screen.findByText(t.watch.range(WATCHES_PAGE_SIZE + 1, watches.length, watches.length))).toBeInTheDocument();
    expect(screen.getByText(t.watch.projectNamed(`P${WATCHES_PAGE_SIZE}`))).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: t.watch.previous }));
    expect(await screen.findByText(t.watch.range(1, WATCHES_PAGE_SIZE, watches.length))).toBeInTheDocument();
  });

  it("still offers the earlier watches once the last of a later page is stopped", async () => {
    const watches = Array.from({ length: WATCHES_PAGE_SIZE + 1 }, (_, i) => aWatch(i));
    const last = watches.at(-1)!;
    const answers = stubWatches(watches, {
      [`DELETE /projects/${last.projectKey}/watch`]: () => {
        watches.pop();
        return { status: 204 };
      },
    });
    await renderAt("/settings/watching");
    await userEvent.click(await screen.findByRole("button", { name: t.watch.next }));
    const name = t.watch.projectNamed(last.projectKey);
    await userEvent.click(await screen.findByRole("button", { name: t.watch.stopNamed(name) }));
    await waitFor(() => expect(answers.sent.some((each) => each.method === "DELETE" && each.path === `/projects/${last.projectKey}/watch`)).toBe(true));
    expect(await screen.findByText(t.watch.projectNamed("P0"))).toBeInTheDocument();
    expect(screen.queryByText(t.watch.listEmpty)).toBeNull();
    expect(answers.asked.at(-1)).toBe(0);
  });

  it("says so when a stop fails, and keeps the row", async () => {
    stubWatches([aWatch(1)], { "DELETE /projects/P1/watch": broken });
    await renderAt("/settings/watching");
    await userEvent.click(await screen.findByRole("button", { name: t.watch.stopNamed(t.watch.projectNamed("P1")) }));
    expect(await screen.findByText(t.watch.failed)).toBeInTheDocument();
    expect(screen.getByText(t.watch.projectNamed("P1"))).toBeInTheDocument();
  });
});

describe("the project's watch button", () => {
  it("says so when watching the project fails", async () => {
    stubApi({
      "GET /projects/CHK": { status: 200, body: { project: aProject() } },
      "GET /projects/CHK/requirements": { status: 200, body: { requirements: [], total: 0, limit: 50, offset: 0 } },
      "PUT /projects/CHK/watch": broken,
    });
    await renderAt("/projects/CHK");
    await userEvent.click(await screen.findByRole("button", { name: "Watch project" }));
    expect(await screen.findByText(t.watch.failed)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Watch project" })).toHaveAttribute("aria-pressed", "false");
  });
});

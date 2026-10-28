import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ArmatureIssue } from "@/api/armature";
import { ARMATURE_SEARCH_DEBOUNCE_MS } from "@/config";
import { stubApi } from "@/test/app";
import { IssuePicker } from "./IssuePicker";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";

function anIssue(key: string, summary: string): ArmatureIssue {
  return {
    key,
    summary,
    url: `${BASE}/issues/${key}`,
    type: "Task",
    status: { name: "To do", category: "todo" },
    assigneeName: "Alice",
    updatedAt: "2026-10-01T09:00:00Z",
  };
}

const FOUND = [anIssue("CP-1", "Set up the build pipeline"), anIssue("CP-2", "Fix sign-in"), anIssue("CP-3", "Cache the build")];

/** The picker inside a form, as the trace form holds it, with Armature's search stubbed. */
function renderPicker() {
  const asked: string[] = [];
  stubApi({
    "GET /armature/search": (request) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      asked.push(q);
      return { status: 200, body: { status: "ok", issues: q === "build" ? FOUND : [] } };
    },
  });
  const onPick = vi.fn();
  const onSubmit = vi.fn((event: SubmitEvent) => event.preventDefault());
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <form aria-label="Trace" onSubmit={(event) => onSubmit(event.nativeEvent as SubmitEvent)}>
        <IssuePicker label="Issue" onPick={onPick} />
      </form>
    </QueryClientProvider>,
  );
  return { onPick, onSubmit, asked, box: screen.getByRole("combobox", { name: "Issue" }) };
}

function selected(): string | null {
  return document.querySelector('[role="option"][aria-selected="true"]')?.getAttribute("data-issue-option") ?? null;
}

describe("the issue picker", () => {
  it("asks Armature once for what was typed fast, after the pause", async () => {
    const user = userEvent.setup();
    const { box, asked } = renderPicker();
    await user.type(box, "build");
    await screen.findByRole("option", { name: /Set up the build pipeline/ });
    // Long enough for any search a keystroke still had pending to have gone out.
    await new Promise((resolve) => setTimeout(resolve, ARMATURE_SEARCH_DEBOUNCE_MS * 2));
    expect(asked).toEqual(["build"]);
  });

  it("moves through the options with the arrows and picks with Enter, without submitting the form", async () => {
    const user = userEvent.setup();
    const { box, onPick, onSubmit } = renderPicker();
    await user.type(box, "build");
    await screen.findByRole("option", { name: /Cache the build/ });
    expect(selected()).toBe("CP-1");
    await user.keyboard("{ArrowDown}");
    expect(selected()).toBe("CP-2");
    expect(box).toHaveAttribute("aria-activedescendant", document.querySelector('[data-issue-option="CP-2"]')!.id);
    await user.keyboard("{ArrowDown}{ArrowDown}");
    expect(selected()).toBe("CP-3");
    await user.keyboard("{ArrowUp}");
    expect(selected()).toBe("CP-2");
    await user.keyboard("{Enter}");
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith(FOUND[1]);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(box).toHaveValue("");
    expect(screen.getByRole("listbox", { hidden: true })).not.toBeVisible();
  });

  it("closes the list with Escape and keeps what was typed", async () => {
    const user = userEvent.setup();
    const { box, onPick } = renderPicker();
    await user.type(box, "build");
    const listbox = await screen.findByRole("listbox");
    expect(listbox).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(listbox).not.toBeVisible());
    expect(box).toHaveAttribute("aria-expanded", "false");
    expect(box).toHaveValue("build");
    await user.keyboard("{Enter}");
    expect(onPick).not.toHaveBeenCalled();
  });

  it("says when nothing the viewer may see matches", async () => {
    const user = userEvent.setup();
    const { box } = renderPicker();
    await user.type(box, "SEC-1");
    expect(await screen.findByText("No issue that you may see matches. Try its key, or other words.")).toBeVisible();
    expect(screen.queryByRole("option")).toBeNull();
  });
});

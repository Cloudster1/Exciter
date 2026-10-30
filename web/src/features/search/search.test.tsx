import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Hit, QuickHit } from "@/api/search";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const ref = (key: string, title: string) => ({
  id: `0195f000-0000-7000-8000-0000000001${key.slice(-2).padStart(2, "0")}`,
  key,
  projectKey: "CHK",
  kind: "requirement" as const,
  title,
  revision: 2,
  status: { id: "0195f000-0000-7000-8000-000000000b01", name: "Draft", category: "draft" as const },
  trashed: false,
});

const hit = (key: string, over: Partial<Hit> = {}): Hit => ({
  requirement: ref(key, "Pay the basket"),
  projectName: "Checkout",
  title: [
    { text: "Pay the ", match: false },
    { text: "basket", match: true },
  ],
  snippet: [
    { text: "The shopper pays for the whole ", match: false },
    { text: "basket", match: true },
  ],
  labels: ["checkout"],
  updatedAt: "2026-10-01T08:00:00Z",
  updatedByName: "Ada Lovelace",
  ...over,
});

describe("the full search", () => {
  it("shows the hits with their matches marked, a page at a time, and keeps the search in the address", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /search": (request) => {
        const params = new URL(request.url).searchParams;
        asked.push(params);
        return { status: 200, body: { hits: [hit("CHK-R1")], total: 25, limit: 20, offset: Number(params.get("offset") ?? 0) } };
      },
    });
    const router = await renderAt("/search?q=basket&label=checkout");
    expect(await screen.findByText("1 to 1 of 25 results")).toBeInTheDocument();
    expect(asked[0]?.get("q")).toBe("basket");
    expect(asked[0]?.getAll("label")).toEqual(["checkout"]);
    const row = document.querySelector('[data-search-hit="CHK-R1"]') as HTMLElement;
    expect(within(row).getByRole("link", { name: "Pay the basket" })).toHaveAttribute("href", "/p/CHK/r/CHK-R1");
    expect(within(row).getAllByText("basket")[0]?.tagName).toBe("MARK");
    expect(row).toHaveTextContent("Checkout / Ada Lovelace changed it on");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "basket", label: "checkout", page: 2 }));
    await waitFor(() => expect(asked.at(-1)?.get("offset")).toBe("20"));

    const box = screen.getByRole("searchbox", { name: "Search for" });
    await userEvent.clear(box);
    await userEvent.type(box, "refund{Enter}");
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "refund", label: "checkout" }));
  });

  it("narrows by a status category and says when nothing matches or the search is refused", async () => {
    stubApi({
      "GET /search": (request) => {
        const params = new URL(request.url).searchParams;
        if (params.get("q") === "x".repeat(3)) {
          return {
            status: 422,
            body: { error: { code: "validation_failed", message: "Keep the search to 200 characters.", fields: { q: "Keep it short." } } },
          };
        }
        return { status: 200, body: { hits: [], total: 0, limit: 20, offset: 0 } };
      },
    });
    const router = await renderAt("/search?q=nothing");
    expect(await screen.findByText("Nothing matches")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText("Status"), "approved");
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "nothing", category: "approved" }));
    await router.navigate({ to: "/search", search: { q: "xxx" } });
    expect(await screen.findByText("Keep the search to 200 characters.")).toBeInTheDocument();
  });
});

describe("the palette", () => {
  it("offers requirements by what was typed, and the full search for it", async () => {
    const found: QuickHit[] = [{ requirement: ref("CHK-R4", "Print the receipt"), projectName: "Checkout", outline: "2.1" }];
    const asked: string[] = [];
    stubApi({
      "GET /search/quick": (request) => {
        asked.push(new URL(request.url).searchParams.get("q") ?? "");
        return { status: 200, body: { requirements: found } };
      },
      "GET /projects/CHK": { status: 404, body: { error: { code: "not_found", message: "Not here." } } },
      "GET /requirements/CHK-R4": { status: 404, body: { error: { code: "not_found", message: "Not here." } } },
    });
    const router = await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "chk-r4");
    const option = await screen.findByRole("option", { name: /CHK-R4 Print the receipt/ });
    expect(option).toHaveTextContent("Checkout 2.1");
    expect(asked.at(-1)).toBe("chk-r4");
    const options = within(screen.getByRole("dialog", { name: "Go to" })).getAllByRole("option");
    expect(options.at(-1)).toHaveTextContent("Search everything for chk-r4");
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/p/CHK/r/CHK-R4"));

    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "receipt");
    await screen.findByRole("option", { name: /CHK-R4/ });
    await userEvent.keyboard("{End}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));
    expect(router.state.location.search).toEqual({ q: "receipt" });
  });
});

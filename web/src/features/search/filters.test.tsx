import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TermHit } from "@/api/glossary";
import type { Hit } from "@/api/search";
import { searchRoute } from "@/routes/search";
import { renderAt, signedIn, stubApi } from "@/test/app";

afterEach(() => vi.unstubAllGlobals());

const hit: Hit = {
  requirement: {
    id: "0195f000-0000-7000-8000-000000000101",
    key: "CHK-R1",
    projectKey: "CHK",
    kind: "requirement",
    title: "Pay the basket",
    revision: 1,
    status: null,
    trashed: false,
  },
  projectName: "Checkout",
  title: [{ text: "Pay the basket", match: false }],
  snippet: [],
  labels: [],
  updatedAt: "2026-10-01T08:00:00Z",
  updatedByName: "Ada Lovelace",
};
const term: TermHit = { id: "0195f000-0000-7000-8000-000000000201", projectKey: "CHK", name: "Basket", status: "approved", snippet: "What a shopper buys." };

function stubSearch() {
  const asked: URLSearchParams[] = [];
  stubApi({
    "GET /search": (request) => {
      const params = new URL(request.url).searchParams;
      asked.push(params);
      const offset = Number(params.get("offset") ?? 0);
      return { status: 200, body: { hits: [hit], terms: offset === 0 ? [term] : [], total: 25, limit: 20, offset } };
    },
  });
  return asked;
}

describe("the search's filters", () => {
  it("narrows by owner and by day and orders by the latest change, each in the address and the request", async () => {
    const asked = stubSearch();
    const router = await renderAt("/search?q=basket");
    await screen.findByText("1 to 1 of 25 results");

    await userEvent.selectOptions(screen.getByLabelText("Owner"), `You (${signedIn.user.name})`);
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "basket", owner: signedIn.user.id }));
    await waitFor(() => expect(asked.at(-1)?.getAll("owner")).toEqual([signedIn.user.id]));

    fireEvent.change(screen.getByLabelText("Changed on or after"), { target: { value: "2026-09-01" } });
    await waitFor(() => expect(asked.at(-1)?.get("updatedAfter")).toBe("2026-09-01"));
    fireEvent.change(screen.getByLabelText("Changed before"), { target: { value: "2026-10-01" } });
    await waitFor(() => expect(asked.at(-1)?.get("updatedBefore")).toBe("2026-10-01"));
    expect(router.state.location.search).toMatchObject({ updatedAfter: "2026-09-01", updatedBefore: "2026-10-01" });

    await userEvent.selectOptions(screen.getByLabelText("Order"), "Latest change first");
    await waitFor(() => expect(asked.at(-1)?.get("sort")).toBe("updated"));
    expect(router.state.location.search).toMatchObject({ sort: "updated" });

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "basket", sort: "updated" }));
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
    await waitFor(() => expect(asked.at(-1)?.get("owner")).toBeNull());
  });

  it("drops a label when its chip is removed", async () => {
    const asked = stubSearch();
    const router = await renderAt("/search?q=basket&label=checkout,paper");
    await screen.findByText("1 to 1 of 25 results");
    expect(asked.at(-1)?.getAll("label")).toEqual(["checkout", "paper"]);
    await userEvent.click(screen.getByRole("button", { name: "Remove label checkout" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "basket", label: "paper" }));
    await waitFor(() => expect(asked.at(-1)?.getAll("label")).toEqual(["paper"]));
  });

  it("shows the glossary's terms above the first page of hits alone", async () => {
    stubSearch();
    await renderAt("/search?q=basket");
    await screen.findByText("1 to 1 of 25 results");
    expect(document.querySelector('[data-search-term="Basket"]')).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("21 to 21 of 25 results")).toBeInTheDocument();
    expect(document.querySelector("[data-search-terms]")).toBeNull();
  });
});

describe("the search's address", () => {
  it("keeps only what it can search by", () => {
    const validate = searchRoute.options.validateSearch as (search: Record<string, unknown>) => Record<string, unknown>;
    expect(validate({ q: " basket ", page: 1, sort: "oldest" })).toEqual({ q: "basket" });
    expect(validate({ q: "basket", page: "abc", sort: "updated" })).toEqual({ q: "basket", sort: "updated" });
    expect(validate({ page: 3, owner: "  ", label: "paper" })).toEqual({ page: 3, label: "paper" });
  });
});

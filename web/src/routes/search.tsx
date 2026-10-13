import { createRoute, useNavigate } from "@tanstack/react-router";
import { SearchScreen, type SearchAddress } from "@/features/search/SearchScreen";
import { appRoute } from "./app";

const text = (value: unknown): string | undefined => (typeof value === "string" && value.trim() ? value.trim() : undefined);

/** The full search, which the address holds whole so a search can be shared and gone back to. */
export const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/search",
  validateSearch: (search: Record<string, unknown>): SearchAddress => {
    const page = Number(search.page);
    return Object.fromEntries(
      Object.entries({
        q: text(search.q),
        project: text(search.project),
        category: text(search.category),
        label: text(search.label),
        owner: text(search.owner),
        updatedAfter: text(search.updatedAfter),
        updatedBefore: text(search.updatedBefore),
        sort: search.sort === "updated" ? "updated" : undefined,
        page: Number.isInteger(page) && page > 1 ? page : undefined,
      }).filter(([, value]) => value !== undefined),
    ) as SearchAddress;
  },
  component: function SearchPage() {
    const address = searchRoute.useSearch();
    const navigate = useNavigate();
    return <SearchScreen address={address} onChange={(next, replace) => void navigate({ to: "/search", search: next, replace })} />;
  },
});

import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { QUERY_SUGGEST_LIMIT, QUICK_SEARCH_LIMIT, SEARCH_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components, operations } from "./schema";

type Wire = components["schemas"];

/** A run of a hit's title or snippet; the matched runs are marked, and none of it is ever markup. */
export type Segment = Wire["Segment"];
/** A requirement whose words matched a full search. */
export type Hit = Wire["SearchHit"];
/** A requirement the Ctrl K palette offers. */
export type QuickHit = Wire["QuickSearchHit"];
/** What a full search asks: the words and the filters beside them. */
export type SearchRequest = NonNullable<operations["search"]["parameters"]["query"]>;
/** What the grid asks of the server, in the words of the contract. */
export type GridParams = NonNullable<operations["listGrid"]["parameters"]["query"]>;
export type View = Wire["View"];
export type ViewInput = Wire["ViewInput"];
export type ViewKind = View["kind"];
export type ViewSettings = Wire["ViewSettings"];
export type Filter = Wire["ViewFilter"];
export type Sort = Wire["ViewSort"];
export type AttributeCondition = Wire["AttributeCondition"];
export type Op = AttributeCondition["op"];
/** What may come next at the caret of a grid's query (#47). */
export type Suggestions = Wire["Suggestions"];
export type Completion = Suggestions["completion"];

export const searchQueryKey = ["search"] as const;
const viewsKey = (projectKey: string) => ["views", projectKey.toUpperCase()] as const;

/** A full search, a page of hits at a time. */
export function useSearch(request: SearchRequest) {
  return useQuery({
    queryKey: [...searchQueryKey, "full", request],
    queryFn: async () => (await api.GET("/search", { params: { query: { ...request, limit: SEARCH_PAGE_SIZE } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** Requirements whose key or title start with what was typed; nothing typed asks nothing. */
export function useQuickSearch(typed: string) {
  return useQuery({
    queryKey: [...searchQueryKey, "quick", typed],
    queryFn: async (): Promise<QuickHit[]> =>
      (await api.GET("/search/quick", { params: { query: { q: typed, limit: QUICK_SEARCH_LIMIT } } })).data!.requirements,
    enabled: typed !== "",
    placeholderData: keepPreviousData,
  });
}

/** A window of a project's grid, narrowed and ordered as params say. */
export function useGrid(projectKey: string, params: GridParams, window: { limit: number; offset: number }) {
  return useQuery({
    queryKey: ["requirements", "list", projectKey.toUpperCase(), "grid", params, window],
    queryFn: async () => (await api.GET("/projects/{projectKey}/requirements", { params: { path: { projectKey }, query: { ...params, ...window } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** What may come next at the caret of a query being typed in a project's grid; the last answer stays until the next. */
export function useQuerySuggestions(projectKey: string, typed: { q: string; at: number }, enabled: boolean) {
  return useQuery({
    queryKey: ["requirements", "suggest", projectKey.toUpperCase(), typed],
    queryFn: async () =>
      (
        await api.GET("/projects/{projectKey}/requirements/suggest", {
          params: { path: { projectKey }, query: { q: typed.q, at: typed.at, limit: QUERY_SUGGEST_LIMIT } },
        })
      ).data!,
    enabled,
    placeholderData: keepPreviousData,
  });
}

/** The reader's views of one kind of a project's, the grid's unless asked (#152), then the shared ones. */
export function useViews(projectKey: string, kind: ViewKind = "grid") {
  return useQuery({
    queryKey: [...viewsKey(projectKey), kind],
    queryFn: async (): Promise<View[]> => (await api.GET("/projects/{projectKey}/views", { params: { path: { projectKey }, query: { kind } } })).data!.views,
  });
}

/** Saves, changes and deletes views, and has the list ask again after each. */
export function useViewActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: viewsKey(projectKey) });
  return {
    create: useMutation({
      mutationFn: async (body: ViewInput): Promise<View> =>
        (await api.POST("/projects/{projectKey}/views", { params: { path: { projectKey } }, body })).data!.view,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, body }: { id: string; body: ViewInput }): Promise<View> =>
        (await api.PATCH("/views/{viewID}", { params: { path: { viewID: id } }, body })).data!.view,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/views/{viewID}", { params: { path: { viewID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}

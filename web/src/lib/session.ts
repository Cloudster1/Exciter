import { MutationCache, QueryCache, QueryClient, type InvalidateOptions, type InvalidateQueryFilters, type QueryKey } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { LOGIN_PATH, QUERY_STALE_MS } from "@/config";

/** Marks a query or mutation that answers a 401 itself, such as asking who is signed in. */
export const HANDLES_UNAUTHORIZED = { handlesUnauthorized: true } as const;

/** A query client whose invalidation is never answered by a first load that was asked before the change (#372). */
class FreshQueryClient extends QueryClient {
  // A fetch in flight without data is joined rather than started again, so a list still loading when something is
  // saved would come back without it; that load is dropped and the list asked for anew.
  override async invalidateQueries<TTaggedQueryKey extends QueryKey = QueryKey>(
    filters?: InvalidateQueryFilters<TTaggedQueryKey>,
    options?: InvalidateOptions,
  ): Promise<void> {
    const loading = this.getQueryCache()
      .findAll(filters)
      .filter((query) => query.state.fetchStatus === "fetching" && query.state.data === undefined);
    await Promise.all(loading.map((query) => query.cancel({ revert: true })));
    return super.invalidateQueries(filters, options);
  }
}

/** The application's query client. A 401 from any request means the session is gone, and onUnauthorized says what follows. */
export function createQueryClient(onUnauthorized: (client: QueryClient) => void): QueryClient {
  const onError = (error: unknown, meta: Record<string, unknown> | undefined) => {
    if (error instanceof ApiError && error.isUnauthenticated && !meta?.handlesUnauthorized) onUnauthorized(client);
  };
  const client: QueryClient = new FreshQueryClient({
    queryCache: new QueryCache({ onError: (error, query) => onError(error, query.meta) }),
    mutationCache: new MutationCache({ onError: (error, _vars, _ctx, mutation) => onError(error, mutation.meta) }),
    defaultOptions: {
      queries: {
        // A failed request is usually a real answer (401, 404, validation), not
        // a blip worth retrying three times while the user waits.
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: QUERY_STALE_MS,
      },
    },
  });
  return client;
}

/** A path of this application to return to, never another site and never the sign-in page itself. */
export function safeNext(next: string | undefined): string | undefined {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return undefined;
  return next === LOGIN_PATH || next.startsWith(`${LOGIN_PATH}?`) ? undefined : next;
}

import { useQuery } from "@tanstack/react-query";
import { META_STALE_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** Which product and build answers, and what it speaks. */
export type Meta = components["schemas"]["MetaResponse"];

/** Which build of the API is answering, for the foot of the sidebar. */
export function useMeta() {
  return useQuery({
    queryKey: ["meta"],
    queryFn: async (): Promise<Meta> => (await api.GET("/meta")).data!,
    staleTime: META_STALE_MS,
  });
}

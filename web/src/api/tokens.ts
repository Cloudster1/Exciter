import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A personal access token; its secret is there only in the answer that made it. */
export type ApiToken = Wire["APIToken"];
/** A token in everybody's list, with whose it is. */
export type OrgApiToken = Wire["OrgAPIToken"];
export type NewApiToken = Wire["NewAPIToken"];

export const tokensQueryKey = ["tokens"] as const;
const everyonesTokensKey = [...tokensQueryKey, "everyone"] as const;

/** The caller's own tokens, newest first. */
export function useTokens() {
  return useQuery({
    queryKey: [...tokensQueryKey, "mine"],
    queryFn: async (): Promise<ApiToken[]> => (await api.GET("/tokens")).data!.tokens,
  });
}

/** Everybody's tokens, for the administrators. */
export function useEveryonesTokens() {
  return useQuery({
    queryKey: everyonesTokensKey,
    queryFn: async (): Promise<OrgApiToken[]> => (await api.GET("/tokens/everyone")).data!.tokens,
  });
}

export function useCreateToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: NewApiToken): Promise<ApiToken> => (await api.POST("/tokens", { body })).data!.token,
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** Revokes one of the caller's tokens, or with anybodys set anybody's, as an administrator. */
export function useRevokeToken(anybodys = false) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      if (anybodys) await api.DELETE("/tokens/everyone/{tokenID}", { params: { path: { tokenID: id } } });
      else await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

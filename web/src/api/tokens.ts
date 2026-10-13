import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A personal access token; its secret is there only in the answer that made it. */
export type ApiToken = Wire["APIToken"];
/** A token in the organization's list, with whose it is. */
export type OrgApiToken = Wire["OrgAPIToken"];
export type NewApiToken = Wire["NewAPIToken"];

export const tokensQueryKey = ["tokens"] as const;
const orgTokensKey = [...tokensQueryKey, "organization"] as const;

/** The caller's own tokens, newest first. */
export function useTokens() {
  return useQuery({
    queryKey: [...tokensQueryKey, "mine"],
    queryFn: async (): Promise<ApiToken[]> => (await api.GET("/tokens")).data!.tokens,
  });
}

/** Every token of the organization, for its administrators. */
export function useOrgTokens() {
  return useQuery({
    queryKey: orgTokensKey,
    queryFn: async (): Promise<OrgApiToken[]> => (await api.GET("/org/tokens")).data!.tokens,
  });
}

export function useCreateToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: NewApiToken): Promise<ApiToken> => (await api.POST("/tokens", { body })).data!.token,
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** Revokes one of the caller's tokens, or with organization set anybody's, as an administrator. */
export function useRevokeToken(organization = false) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      if (organization) await api.DELETE("/org/tokens/{tokenID}", { params: { path: { tokenID: id } } });
      else await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

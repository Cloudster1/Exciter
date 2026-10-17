import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { installationQueryKey, type Installation } from "./installation";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A connection waiting for its person's consent (#188). */
export type AuthorizationRequest = Wire["OAuthAuthorizationRequest"];
export type ConsentInput = Wire["OAuthConsent"];

/** The request the consent page was sent with. */
export function useAuthorizationRequest(id: string | undefined) {
  return useQuery({
    queryKey: ["oauth", "request", id],
    enabled: !!id,
    retry: false,
    queryFn: async (): Promise<AuthorizationRequest> => (await api.GET("/oauth/requests/{requestID}", { params: { path: { requestID: id! } } })).data!.request,
  });
}

/** Allows or denies the request, and answers where the browser goes back to. */
export function useDecideAuthorization(id: string) {
  return useMutation({
    mutationFn: async (body: ConsentInput): Promise<string> =>
      (await api.POST("/oauth/requests/{requestID}", { params: { path: { requestID: id } }, body })).data!.redirectTo,
  });
}

/** Switches OAuth connections on or off for the installation. */
export function useSwitchOAuthConnections() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (enabled: boolean): Promise<Installation> => (await api.PUT("/installation/oauth-connections", { body: { enabled } })).data!.installation,
    onSuccess: (installation) => queryClient.setQueryData(installationQueryKey, installation),
  });
}

/** A client the caller connected, what it may do and when it was last used (#189). */
export type Grant = Wire["OAuthGrant"];
/** A connected client in everybody's list, with whose it is. */
export type OrgGrant = Wire["OrgOAuthGrant"];

export const grantsQueryKey = ["oauth", "grants"] as const;
const everyonesGrantsKey = [...grantsQueryKey, "everyone"] as const;

/** The caller's connected apps, by last use. */
export function useGrants() {
  return useQuery({
    queryKey: [...grantsQueryKey, "mine"],
    queryFn: async (): Promise<Grant[]> => (await api.GET("/oauth/grants")).data!.grants,
  });
}

/** Everybody's connected apps, for the administrators. */
export function useEveryonesGrants() {
  return useQuery({
    queryKey: everyonesGrantsKey,
    queryFn: async (): Promise<OrgGrant[]> => (await api.GET("/oauth/grants/everyone")).data!.grants,
  });
}

/** Ends one of the caller's connections, or with anybodys set anybody's, as an administrator. */
export function useEndGrant(anybodys = false) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      if (anybodys) await api.DELETE("/oauth/grants/everyone/{grantID}", { params: { path: { grantID: id } } });
      else await api.DELETE("/oauth/grants/{grantID}", { params: { path: { grantID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: grantsQueryKey }),
  });
}

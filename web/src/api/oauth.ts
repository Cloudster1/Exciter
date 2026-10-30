import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
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

/** A tool beside Exciter that trades a person's token from the shared sign-in for a grant here (#273). */
export type PartnerApp = Wire["OAuthPartnerApp"];
export type PartnerAppInput = Wire["OAuthPartnerAppInput"];

export const partnerAppsQueryKey = ["oauth", "partners"] as const;

/** The installation's partner apps, by name, for the administrators. */
export function usePartnerApps() {
  return useQuery({
    queryKey: partnerAppsQueryKey,
    queryFn: async (): Promise<PartnerApp[]> => (await api.GET("/oauth/clients")).data!.clients,
  });
}

/** Registers a partner app; the answer carries its secret, once. */
export function useCreatePartnerApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: PartnerAppInput): Promise<PartnerApp> => (await api.POST("/oauth/clients", { body })).data!.client,
    onSettled: () => queryClient.invalidateQueries({ queryKey: partnerAppsQueryKey }),
  });
}

/** Changes a partner app; what is left out keeps its value. */
export function useUpdatePartnerApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: PartnerAppInput & { id: string }): Promise<PartnerApp> =>
      (await api.PATCH("/oauth/clients/{clientID}", { params: { path: { clientID: id } }, body })).data!.client,
    onSettled: () => queryClient.invalidateQueries({ queryKey: partnerAppsQueryKey }),
  });
}

/** Gives a partner app a new secret, answered once; the old one stops at once. */
export function useRotatePartnerSecret() {
  return useMutation({
    mutationFn: async (id: string): Promise<PartnerApp> =>
      (await api.POST("/oauth/clients/{clientID}/rotate-secret", { params: { path: { clientID: id } } })).data!.client,
  });
}

/** Removes a partner app, and every grant it holds. */
export function useDeletePartnerApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/oauth/clients/{clientID}", { params: { path: { clientID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: partnerAppsQueryKey }),
  });
}

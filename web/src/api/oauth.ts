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

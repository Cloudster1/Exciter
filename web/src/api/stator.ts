import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

// The Stator client, as the Armature one; each issue of M4 adds what it reads.

type Wire = components["schemas"];

/** The installation's Stator instance, as its administrators see it; the webhook secret never comes back. */
export type StatorConnection = Wire["StatorConnection"];
export type StatorConnectionInput = Wire["StatorConnectionInput"];
/** The caller's own link to Stator; the token never comes back. */
export type StatorAccount = Wire["StatorAccount"];
export type StatorStatus = StatorAccount["status"];

export const statorQueryKey = ["stator"] as const;
export const statorConnectionQueryKey = [...statorQueryKey, "connection"] as const;
export const statorAccountQueryKey = [...statorQueryKey, "account"] as const;

export function useStatorConnection() {
  return useQuery({
    queryKey: statorConnectionQueryKey,
    queryFn: async (): Promise<StatorConnection | null> => (await api.GET("/stator/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is asked again after any change to the
// connection.
export function useSaveStatorConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: StatorConnectionInput): Promise<StatorConnection> => (await api.PUT("/stator/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(statorConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey });
    },
  });
}

export function useRemoveStatorConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/stator/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(statorConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey });
    },
  });
}

export function useStatorAccount() {
  return useQuery({
    queryKey: statorAccountQueryKey,
    queryFn: async (): Promise<StatorAccount> => (await api.GET("/stator/account")).data!.account,
  });
}

export function useConnectStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<StatorAccount> => (await api.PUT("/stator/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => queryClient.setQueryData(statorAccountQueryKey, account),
  });
}

// A check reads what Stator offers again, which the connection shows its administrators.
export function useCheckStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<StatorAccount> => (await api.POST("/stator/account/check")).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(statorAccountQueryKey, account);
      void queryClient.invalidateQueries({ queryKey: statorConnectionQueryKey });
    },
  });
}

export function useDisconnectStator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/stator/account/token");
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: statorAccountQueryKey }),
  });
}

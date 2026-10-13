import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

// Stator's Armature client, cut to what #26 calls; each issue of M2 adds what it reads.

type Wire = components["schemas"];

/** The organization's Armature instance, as its administrators see it; the webhook secret never comes back. */
export type ArmatureConnection = Wire["ArmatureConnection"];
export type ArmatureConnectionInput = Wire["ArmatureConnectionInput"];
/** The caller's own link to Armature; the token never comes back. */
export type ArmatureAccount = Wire["ArmatureAccount"];
export type ArmatureStatus = ArmatureAccount["status"];

export const armatureQueryKey = ["armature"] as const;
export const armatureConnectionQueryKey = [...armatureQueryKey, "connection"] as const;
export const armatureAccountQueryKey = [...armatureQueryKey, "account"] as const;

export function useArmatureConnection() {
  return useQuery({
    queryKey: armatureConnectionQueryKey,
    queryFn: async (): Promise<ArmatureConnection | null> => (await api.GET("/armature/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is asked again after any change to the
// connection.
export function useSaveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ArmatureConnectionInput): Promise<ArmatureConnection> => (await api.PUT("/armature/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(armatureConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
    },
  });
}

export function useRemoveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(armatureConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
    },
  });
}

export function useArmatureAccount() {
  return useQuery({
    queryKey: armatureAccountQueryKey,
    queryFn: async (): Promise<ArmatureAccount> => (await api.GET("/armature/account")).data!.account,
  });
}

export function useConnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<ArmatureAccount> => (await api.PUT("/armature/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => queryClient.setQueryData(armatureAccountQueryKey, account),
  });
}

export function useCheckArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ArmatureAccount> => (await api.POST("/armature/account/check")).data!.account,
    onSuccess: (account) => queryClient.setQueryData(armatureAccountQueryKey, account),
  });
}

export function useDisconnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/account/token");
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey }),
  });
}

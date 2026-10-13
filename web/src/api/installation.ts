import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { meQueryKey } from "./auth";
import { api } from "./client";
import type { components } from "./schema";

/** What everybody signed in may read about the installation. */
export type Installation = components["schemas"]["Installation"];

export const installationQueryKey = ["installation"] as const;

export function useInstallation() {
  return useQuery({
    queryKey: installationQueryKey,
    queryFn: async (): Promise<Installation> => (await api.GET("/installation")).data!.installation,
  });
}

// The sidebar and the breadcrumbs read the name from who is signed in, so that is asked again too.
export function useRenameInstallation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (name: string): Promise<Installation> => (await api.PUT("/installation", { body: { name } })).data!.installation,
    onSuccess: (installation) => {
      queryClient.setQueryData(installationQueryKey, installation);
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

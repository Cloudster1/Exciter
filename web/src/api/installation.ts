import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SUITE_DEMO_POLL_MS } from "@/config";
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

/** The suite demo (#270): how far the worker got, what it made and why links wait. */
export type SuiteDemo = components["schemas"]["Demo"];

export const suiteDemoQueryKey = ["installation", "suite-demo"] as const;

const busy = (demo: SuiteDemo | null | undefined) => demo?.state === "queued" || demo?.state === "running";

/** The demo, null while nobody added it; read again while the worker is at it. */
export function useSuiteDemo() {
  return useQuery({
    queryKey: suiteDemoQueryKey,
    queryFn: async (): Promise<SuiteDemo | null> => (await api.GET("/installation/suite-demo")).data!.demo ?? null,
    refetchInterval: (query) => (busy(query.state.data) ? SUITE_DEMO_POLL_MS : false),
  });
}

function useSuiteDemoAction(path: "/installation/suite-demo" | "/installation/suite-demo/links") {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<SuiteDemo> => (await api.POST(path)).data!.demo,
    onSuccess: (demo) => queryClient.setQueryData(suiteDemoQueryKey, demo),
  });
}

export const useAddSuiteDemo = () => useSuiteDemoAction("/installation/suite-demo");
export const useLinkSuiteDemo = () => useSuiteDemoAction("/installation/suite-demo/links");

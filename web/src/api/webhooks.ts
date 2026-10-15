import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { WEBHOOK_DELIVERIES_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Where the installation's requirement events are posted; its secret is there only in the answer that made or rotated it. */
export type Webhook = Wire["Webhook"];
export type WebhookInput = Wire["WebhookInput"];
export type WebhookTopic = WebhookInput["topics"][number];
/** One attempt at one event. */
export type WebhookDelivery = Wire["WebhookDelivery"];
export type DeliveryState = WebhookDelivery["state"];

export const webhooksQueryKey = ["webhooks"] as const;

export function useWebhooks() {
  return useQuery({
    queryKey: webhooksQueryKey,
    queryFn: async (): Promise<Webhook[]> => (await api.GET("/webhooks")).data!.webhooks,
  });
}

/** A webhook's log, newest first; nothing is asked while id is null. */
export function useWebhookDeliveries(id: string | null) {
  return useQuery({
    queryKey: [...webhooksQueryKey, "deliveries", id],
    queryFn: async (): Promise<WebhookDelivery[]> =>
      (await api.GET("/webhooks/{webhookID}/deliveries", { params: { path: { webhookID: id! }, query: { limit: WEBHOOK_DELIVERIES_PAGE_SIZE } } })).data!
        .deliveries,
    enabled: id !== null,
  });
}

function useWebhookMutation<TInput, TOut>(fn: (input: TInput) => Promise<TOut>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: fn, onSettled: () => queryClient.invalidateQueries({ queryKey: webhooksQueryKey }) });
}

export function useCreateWebhook() {
  return useWebhookMutation(async (body: WebhookInput): Promise<Webhook> => (await api.POST("/webhooks", { body })).data!.webhook);
}

export function useUpdateWebhook() {
  return useWebhookMutation(
    async ({ id, ...body }: WebhookInput & { id: string }): Promise<Webhook> =>
      (await api.PATCH("/webhooks/{webhookID}", { params: { path: { webhookID: id } }, body })).data!.webhook,
  );
}

export function useDeleteWebhook() {
  return useWebhookMutation(async (id: string) => {
    await api.DELETE("/webhooks/{webhookID}", { params: { path: { webhookID: id } } });
  });
}

export function useRotateWebhookSecret() {
  return useWebhookMutation(
    async (id: string): Promise<Webhook> => (await api.POST("/webhooks/{webhookID}/rotate-secret", { params: { path: { webhookID: id } } })).data!.webhook,
  );
}

export function useTestWebhook() {
  return useWebhookMutation(
    async (id: string): Promise<WebhookDelivery> => (await api.POST("/webhooks/{webhookID}/test", { params: { path: { webhookID: id } } })).data!.delivery,
  );
}

export function useRedeliverWebhook() {
  return useWebhookMutation(
    async ({ webhookId, deliveryId }: { webhookId: string; deliveryId: string }): Promise<WebhookDelivery> =>
      (
        await api.POST("/webhooks/{webhookID}/deliveries/{deliveryID}/redeliver", {
          params: { path: { webhookID: webhookId, deliveryID: deliveryId } },
        })
      ).data!.delivery,
  );
}

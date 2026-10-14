import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { REVIEWS_PAGE_SIZE, UNREAD_POLL_MS } from "@/config";
import { api } from "./client";
import type { Subject } from "./permissions";
import { requirementQueryKey } from "./requirements";
import type { components } from "./schema";

// Review and approval of requirement revisions (#35).

type Wire = components["schemas"];

export type ApprovalRule = Wire["ApprovalRule"];
export type ApprovalRuleInput = Wire["ApprovalRuleInput"];
export type Approver = Wire["Approver"];
export type Review = Wire["Review"];
export type ReviewDecision = Wire["ReviewDecision"];
export type Decision = ReviewDecision["decision"];

/** An approver as the permission pickers show somebody. */
export function approverSubject(a: Approver): Subject {
  return a.subjectType === "user" ? { type: "user", id: a.userId ?? null, name: a.name ?? "" } : { type: "group", id: a.groupId ?? null, name: a.name ?? "" };
}

/** Somebody a picker chose, as a rule names an approver; everyone never approves. */
export function subjectApprover(s: Subject): Approver {
  return s.type === "user"
    ? { subjectType: "user", userId: s.id ?? undefined, name: s.name }
    : { subjectType: "group", groupId: s.id ?? undefined, name: s.name };
}

const reviewsKey = ["reviews"] as const;
const waitingKey = [...reviewsKey, "waiting"] as const;

function rulesKey(projectKey: string) {
  return ["projects", "one", projectKey.toUpperCase(), "approval-rules"] as const;
}

function requirementReviewsKey(reqKey: string) {
  return [...requirementQueryKey(reqKey), "reviews"] as const;
}

export function useApprovalRules(projectKey: string) {
  return useQuery({
    queryKey: rulesKey(projectKey),
    queryFn: async (): Promise<ApprovalRule[]> => (await api.GET("/projects/{projectKey}/approval-rules", { params: { path: { projectKey } } })).data!.rules,
  });
}

export function useSaveApprovalRule(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (rule: ApprovalRuleInput): Promise<ApprovalRule> =>
      (await api.PUT("/projects/{projectKey}/approval-rules", { params: { path: { projectKey } }, body: rule })).data!.rule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: rulesKey(projectKey) }),
  });
}

export function useDeleteApprovalRule(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (ruleId: string) => {
      await api.DELETE("/approval-rules/{ruleID}", { params: { path: { ruleID: ruleId } } });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: rulesKey(projectKey) }),
  });
}

/** A requirement's latest reviews, the newest first. */
export function useReviews(reqKey: string) {
  return useQuery({
    queryKey: requirementReviewsKey(reqKey),
    queryFn: async () => (await api.GET("/requirements/{reqKey}/reviews", { params: { path: { reqKey }, query: { limit: REVIEWS_PAGE_SIZE } } })).data!,
  });
}

// A review changes the requirement's approval, its reviews and somebody's inbox.
function reviewed(queryClient: QueryClient, reqKey: string) {
  void queryClient.invalidateQueries({ queryKey: requirementQueryKey(reqKey) });
  void queryClient.invalidateQueries({ queryKey: reviewsKey });
}

/** Asks for the revision the reader saw to be approved. */
export function useRequestReview(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { revision: number; comment?: string }): Promise<Review> =>
      (await api.POST("/requirements/{reqKey}/reviews", { params: { path: { reqKey } }, body: input })).data!.review,
    onSuccess: () => reviewed(queryClient, reqKey),
  });
}

/** Approves or rejects the revision under review, as read. */
export function useDecide(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ reviewId, ...body }: { reviewId: string; decision: Decision; revision: number; comment?: string }): Promise<Review> =>
      (await api.POST("/reviews/{reviewID}/decisions", { params: { path: { reviewID: reviewId } }, body })).data!.review,
    onSuccess: () => reviewed(queryClient, reqKey),
  });
}

export function useWithdrawReview(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (reviewId: string): Promise<Review> =>
      (await api.POST("/reviews/{reviewID}/withdraw", { params: { path: { reviewID: reviewId } } })).data!.review,
    onSuccess: () => reviewed(queryClient, reqKey),
  });
}

/** What waits for the reader's decision, the oldest first, asked again as often as the bell asks. */
export function useWaitingReviews(offset = 0) {
  return useQuery({
    queryKey: [...waitingKey, offset],
    queryFn: async () => (await api.GET("/reviews/waiting", { params: { query: { limit: REVIEWS_PAGE_SIZE, offset: offset || undefined } } })).data!,
    placeholderData: keepPreviousData,
    refetchInterval: UNREAD_POLL_MS,
  });
}

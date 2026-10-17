import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, REVIEWS_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// Review sessions (#204): one review of a section, a baseline's requirements or a query's, in rounds.

type Wire = components["schemas"];

export type ReviewSession = Wire["ReviewSession"];
export type SessionSummary = Wire["ReviewSessionSummary"];
export type SessionInput = Wire["ReviewSessionInput"];
export type SessionParticipant = Wire["ReviewSessionParticipant"];
export type SessionScope = Wire["ReviewSessionScope"];
export type SessionReport = Wire["ReviewSessionReport"];
export type SessionState = ReviewSession["state"];

const sessionsKey = ["sessions"] as const;
const sessionKey = (id: string) => [...sessionsKey, "one", id] as const;

/** A project's review sessions, open first. */
export function useProjectSessions(projectKey: string, offset = 0) {
  return useQuery({
    queryKey: [...sessionsKey, "of", projectKey.toUpperCase(), offset],
    queryFn: async () =>
      (await api.GET("/projects/{projectKey}/review-sessions", { params: { path: { projectKey }, query: { limit: REVIEWS_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** The open sessions the reader takes part in, nearest deadline first. */
export function useMySessions() {
  return useQuery({
    queryKey: [...sessionsKey, "mine"],
    queryFn: async () => (await api.GET("/reviews/sessions", { params: { query: { limit: REVIEWS_PAGE_SIZE } } })).data!,
  });
}

export function useSession(id: string) {
  return useQuery({
    queryKey: sessionKey(id),
    queryFn: async (): Promise<ReviewSession> => (await api.GET("/review-sessions/{sessionID}", { params: { path: { sessionID: id } } })).data!.session,
  });
}

export function useSessionReport(id: string) {
  return useQuery({
    queryKey: [...sessionKey(id), "report"],
    queryFn: async (): Promise<SessionReport> => (await api.GET("/review-sessions/{sessionID}/report", { params: { path: { sessionID: id } } })).data!.report,
  });
}

/** Where a session's report downloads, as PDF or Word. */
export function sessionReportHref(id: string, format: "pdf" | "docx"): string {
  return `${API_BASE}/review-sessions/${encodeURIComponent(id)}/report.${format}`;
}

/** Opens, changes, moves on and closes sessions; every list of them, and the reviews they ask, read again. */
export function useSessionActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () =>
    Promise.all([["sessions"], ["reviews"], ["spec"], ["requirements"], ["baselines"]].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
  return {
    open: useMutation({
      mutationFn: async (body: SessionInput): Promise<ReviewSession> =>
        (await api.POST("/projects/{projectKey}/review-sessions", { params: { path: { projectKey } }, body })).data!.session,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: Wire["ReviewSessionUpdate"] & { id: string }): Promise<ReviewSession> =>
        (await api.PATCH("/review-sessions/{sessionID}", { params: { path: { sessionID: id } }, body })).data!.session,
      onSuccess: refresh,
    }),
    round: useMutation({
      mutationFn: async ({ id, comment }: { id: string; comment?: string }): Promise<ReviewSession> =>
        (await api.POST("/review-sessions/{sessionID}/rounds", { params: { path: { sessionID: id } }, body: { comment } })).data!.session,
      onSuccess: refresh,
    }),
    close: useMutation({
      mutationFn: async ({ id, baseline }: { id: string; baseline?: { name: string; description?: string } }): Promise<ReviewSession> =>
        (await api.POST("/review-sessions/{sessionID}/close", { params: { path: { sessionID: id } }, body: { baseline } })).data!.session,
      onSuccess: refresh,
    }),
  };
}

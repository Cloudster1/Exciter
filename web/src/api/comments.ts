import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import type { Mentionable } from "@/features/editor/extensions";
import { api } from "./client";
import { requirementQueryKey } from "./requirements";
import type { components } from "./schema";

// The discussion on a requirement (#33), Stator's comments over requirements.

type Wire = components["schemas"];

/** A first comment and its replies, with what the caller may do to the thread. */
export type Thread = Wire["CommentThread"];
/** One comment; a deleted one keeps its place and author with no body. */
export type Comment = Wire["Comment"];

const threadsKey = (reqKey: string) => ["comments", reqKey.toUpperCase()] as const;

/** A requirement's threads, oldest first. */
export function useThreads(reqKey: string) {
  return useQuery({
    queryKey: threadsKey(reqKey),
    queryFn: async (): Promise<Thread[]> => (await api.GET("/requirements/{reqKey}/comments", { params: { path: { reqKey } } })).data!.threads,
  });
}

// Every change to a discussion is read again, and so is the requirement, whose counts it moved.
function useCommentMutation<TArgs, TResult>(reqKey: string, run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: threadsKey(reqKey) });
      void queryClient.invalidateQueries({ queryKey: requirementQueryKey(reqKey) });
    },
  });
}

export function useStartThread(reqKey: string) {
  return useCommentMutation(
    reqKey,
    async (body: Doc): Promise<Thread> => (await api.POST("/requirements/{reqKey}/comments", { params: { path: { reqKey } }, body: { body } })).data!.thread,
  );
}

export function useReply(reqKey: string) {
  return useCommentMutation(
    reqKey,
    async ({ commentId, body }: { commentId: string; body: Doc }): Promise<Thread> =>
      (await api.POST("/comments/{commentID}/replies", { params: { path: { commentID: commentId } }, body: { body } })).data!.thread,
  );
}

export function useEditComment(reqKey: string) {
  return useCommentMutation(
    reqKey,
    async ({ commentId, body }: { commentId: string; body: Doc }): Promise<Comment> =>
      (await api.PATCH("/comments/{commentID}", { params: { path: { commentID: commentId } }, body: { body } })).data!.comment,
  );
}

export function useDeleteComment(reqKey: string) {
  return useCommentMutation(reqKey, async (commentId: string) => {
    await api.DELETE("/comments/{commentID}", { params: { path: { commentID: commentId } } });
  });
}

export function useResolveThread(reqKey: string) {
  return useCommentMutation(reqKey, async ({ threadId, resolved }: { threadId: string; resolved: boolean }): Promise<Thread> => {
    const path = { params: { path: { threadID: threadId } } };
    const answer = resolved ? await api.PUT("/comment-threads/{threadID}/resolution", path) : await api.DELETE("/comment-threads/{threadID}/resolution", path);
    return answer.data!.thread;
  });
}

/** Who an at sign in a comment on the requirement offers: the members who may read it, found as the author types. */
export function useMentionSource(reqKey: string): (q: string) => Promise<Mentionable[]> {
  return async (q: string) => {
    const { data } = await api.GET("/requirements/{reqKey}/mentionable", { params: { path: { reqKey }, query: { q } } });
    return (data?.people ?? []).filter((p) => p.canView).map((p) => ({ id: p.id, name: p.name, email: p.email }));
  };
}

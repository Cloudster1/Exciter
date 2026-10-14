import { useEffect, useRef, useState } from "react";
import { useRouterState } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import {
  useDeleteComment,
  useEditComment,
  useMentionSource,
  useReply,
  useResolveThread,
  useStartThread,
  useThreads,
  type Comment,
  type Thread,
} from "@/api/comments";
import type { Requirement } from "@/api/requirements";
import { Avatar, Button, Card, ErrorBanner, SectionTitle, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { Editor } from "@/features/editor/Editor";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useFocusWhenRendered } from "@/lib/focus";

// A requirement's discussion (#33), Stator's comments section over requirements.

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Where the specification's count and a notification's link lead. */
export const COMMENTS_ID = "comments";

const THREAD_ANCHOR_PREFIX = "thread-";

/** The fragment of one thread, which a notification and its mail lead to (#34). */
export function threadAnchor(threadId: string): string {
  return THREAD_ANCHOR_PREFIX + threadId;
}

function authorOf(comment: Comment): string {
  return comment.authorName || t.comments.formerMember;
}

/** The comment editor with its buttons: what a comment may say, and the people of the requirement an at sign offers. */
function Composer({
  id,
  reqKey,
  label,
  submitLabel,
  initial = null,
  busy,
  error,
  onSubmit,
  onCancel,
}: {
  id: string;
  reqKey: string;
  label: string;
  submitLabel: string;
  initial?: Doc | null;
  busy: boolean;
  error?: string;
  onSubmit: (body: Doc) => void;
  onCancel?: () => void;
}) {
  const mentions = useMentionSource(reqKey);
  const [body, setBody] = useState<Doc | null>(initial);
  return (
    <form
      className="space-y-2"
      data-comment-editor={id}
      onSubmit={(event) => {
        event.preventDefault();
        if (body && !busy) onSubmit(body);
      }}
    >
      <Editor id={id} variant="comment" value={initial} onChange={setBody} mentions={mentions} aria-label={label} />
      {error && <ErrorBanner>{error}</ErrorBanner>}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" loading={busy} disabled={!body} data-action="post-comment">
          {submitLabel}
        </Button>
        {onCancel && (
          <Button type="button" size="sm" variant="ghost" onClick={onCancel} data-action="cancel-comment">
            {t.comments.cancel}
          </Button>
        )}
      </div>
    </form>
  );
}

/** The discussion on a requirement: its threads, oldest first, each a first comment and its replies, with what the
 * caller may do following each thread's and comment's `can`. */
export function CommentsSection({ requirement }: { requirement: Requirement }) {
  const { data, error, isLoading, refetch } = useThreads(requirement.key);
  const start = useStartThread(requirement.key);
  const [composing, setComposing] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null);
  const focusWhenRendered = useFocusWhenRendered();
  const threads = data ?? [];
  const open = requirement.can.comment && !requirement.trashedAt;
  const hash = useRouterState({ select: (state) => state.location.hash });
  // The thread a link names arrives with the discussion, after the router looked for it.
  useEffect(() => {
    if (!data || !hash.startsWith(THREAD_ANCHOR_PREFIX)) return;
    const target = document.getElementById(hash);
    target?.scrollIntoView?.({ block: "start" });
    target?.focus({ preventScroll: true });
  }, [data, hash]);
  return (
    <Card className="space-y-3 p-4">
      <section id={COMMENTS_ID} aria-labelledby="comments-title" className="scroll-mt-4 space-y-3" data-comments="">
        <SectionTitle id="comments-title">
          {t.comments.title}
          {requirement.comments.threads > 0 && <span className="ml-1 text-ink-subtle normal-case">({requirement.comments.threads})</span>}
        </SectionTitle>
        {error ? (
          <ErrorBanner onRetry={() => void refetch()}>{t.comments.failed}</ErrorBanner>
        ) : isLoading ? (
          <Skeleton />
        ) : (
          <>
            {threads.length === 0 ? (
              <p className="text-sm text-ink-muted" data-comments-empty="">
                {t.comments.empty}
              </p>
            ) : (
              <ol className="space-y-4" aria-labelledby="comments-title">
                {threads.map((each) => (
                  <li key={each.id}>
                    <ThreadView reqKey={requirement.key} thread={each} />
                  </li>
                ))}
              </ol>
            )}
            {!open ? (
              <p className="text-sm text-ink-muted" data-comments-read-only="">
                {requirement.can.comment ? t.comments.closed : t.comments.readOnly}
              </p>
            ) : composing ? (
              <Composer
                id="new-comment"
                reqKey={requirement.key}
                label={t.comments.addLabel}
                submitLabel={t.comments.post}
                busy={start.isPending}
                error={start.error?.message}
                onSubmit={(body) =>
                  start.mutate(body, {
                    onSuccess: (made) => {
                      setComposing(false);
                      focusWhenRendered(() => document.querySelector<HTMLElement>(`[data-thread="${CSS.escape(made.id)}"]`));
                    },
                  })
                }
                onCancel={() => {
                  start.reset();
                  setComposing(false);
                  focusWhenRendered(() => addRef.current);
                }}
              />
            ) : (
              <Button ref={addRef} variant="secondary" icon={<Icon.Comment />} onClick={() => setComposing(true)} data-action="add-comment">
                {t.comments.add}
              </Button>
            )}
          </>
        )}
      </section>
    </Card>
  );
}

/** A first comment and its replies, with reply, resolve and reopen for whoever may comment. */
function ThreadView({ reqKey, thread }: { reqKey: string; thread: Thread }) {
  const reply = useReply(reqKey);
  const resolve = useResolveThread(reqKey);
  const [replying, setReplying] = useState(false);
  const replyRef = useRef<HTMLButtonElement>(null);
  const focusWhenRendered = useFocusWhenRendered();
  const [first, ...replies] = thread.comments;
  if (!first) return null;
  return (
    <article
      id={threadAnchor(thread.id)}
      tabIndex={-1}
      aria-label={t.comments.thread(authorOf(first))}
      className={cx(
        "scroll-mt-4 rounded-overlay border border-border bg-surface p-3 outline-none focus-visible:ring-2 focus-visible:ring-accent",
        thread.resolved && "opacity-80",
      )}
      data-thread={thread.id}
      data-resolved={thread.resolved || undefined}
    >
      {thread.resolved && (
        <p className="mb-2 flex items-center gap-1 text-xs text-ink-muted" data-thread-resolved="">
          <Icon.Check className="size-3" aria-hidden="true" />
          {thread.resolvedByName ? t.comments.resolved(thread.resolvedByName) : t.comments.resolvedBySomebody}
        </p>
      )}
      <CommentView reqKey={reqKey} comment={first} />
      {replies.length > 0 && (
        <ol className="mt-3 space-y-3 border-l-2 border-border pl-3 sm:ml-8" aria-label={t.comments.replies(replies.length)}>
          {replies.map((each) => (
            <li key={each.id}>
              <CommentView reqKey={reqKey} comment={each} />
            </li>
          ))}
        </ol>
      )}
      {thread.can.reply && (
        <div className="mt-3 sm:ml-8">
          {replying ? (
            <Composer
              id={`reply-${thread.id}`}
              reqKey={reqKey}
              label={t.comments.replyLabel(authorOf(first))}
              submitLabel={t.comments.postReply}
              busy={reply.isPending}
              error={reply.error?.message}
              onSubmit={(body) =>
                reply.mutate(
                  { commentId: thread.id, body },
                  {
                    onSuccess: () => {
                      setReplying(false);
                      focusWhenRendered(() => replyRef.current);
                    },
                  },
                )
              }
              onCancel={() => {
                reply.reset();
                setReplying(false);
                focusWhenRendered(() => replyRef.current);
              }}
            />
          ) : (
            <div className="flex flex-wrap gap-1">
              <Button ref={replyRef} size="sm" variant="ghost" onClick={() => setReplying(true)} data-action="reply">
                {t.comments.reply}
              </Button>
              {thread.can.resolve && (
                <Button
                  size="sm"
                  variant="ghost"
                  loading={resolve.isPending}
                  onClick={() => resolve.mutate({ threadId: thread.id, resolved: !thread.resolved })}
                  data-action={thread.resolved ? "reopen-thread" : "resolve-thread"}
                >
                  {thread.resolved ? t.comments.reopen : t.comments.resolve}
                </Button>
              )}
            </div>
          )}
          {resolve.error && <ErrorBanner>{resolve.error.message}</ErrorBanner>}
        </div>
      )}
    </article>
  );
}

function CommentView({ reqKey, comment }: { reqKey: string; comment: Comment }) {
  const edit = useEditComment(reqKey);
  const remove = useDeleteComment(reqKey);
  const me = useMe().data?.user.id;
  const [editing, setEditing] = useState(false);
  const editRef = useRef<HTMLButtonElement>(null);
  const restoreFocus = useRef(false);
  const stopEditing = () => {
    restoreFocus.current = true;
    setEditing(false);
  };
  // The edit button is back only once the form is gone, which a save commits outside any event.
  useEffect(() => {
    if (editing || !restoreFocus.current) return;
    restoreFocus.current = false;
    editRef.current?.focus();
  }, [editing]);
  const author = authorOf(comment);
  return (
    <div className="flex gap-2.5" data-comment={comment.id} data-deleted={comment.deleted || undefined}>
      <Avatar name={author} size="md" className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-1.5 text-xs text-ink-muted">
          <span className="font-medium text-ink" data-comment-author="">
            {author}
          </span>
          <time dateTime={comment.createdAt}>{when.format(new Date(comment.createdAt))}</time>
          <span data-comment-revision={comment.revision}>· {t.comments.onRevision(comment.revision)}</span>
          {comment.editedAt && !comment.deleted && <span data-comment-edited="">· {t.comments.edited}</span>}
        </p>
        {comment.deleted || !comment.body ? (
          <p className="mt-1 text-sm text-ink-muted italic" data-comment-placeholder="">
            {t.comments.deleted}
          </p>
        ) : editing ? (
          <div className="mt-1">
            <Composer
              id={`edit-${comment.id}`}
              reqKey={reqKey}
              label={t.comments.editLabel}
              submitLabel={t.comments.save}
              initial={comment.body as Doc}
              busy={edit.isPending}
              error={edit.error?.message}
              onSubmit={(body) => edit.mutate({ commentId: comment.id, body }, { onSuccess: stopEditing })}
              onCancel={() => {
                edit.reset();
                stopEditing();
              }}
            />
          </div>
        ) : (
          <DocView doc={comment.body as Doc} className="mt-1 text-sm" data-comment-body="" />
        )}
        {!editing && (comment.can.edit || comment.can.delete) && (
          <div className="mt-1 flex gap-1">
            {comment.can.edit && (
              <Button ref={editRef} size="sm" variant="ghost" onClick={() => setEditing(true)} data-action="edit-comment">
                {t.comments.edit}
              </Button>
            )}
            {comment.can.delete && (
              <Button
                size="sm"
                variant="ghost"
                loading={remove.isPending}
                onClick={() => {
                  const mine = comment.authorId !== null && comment.authorId === me;
                  if (!window.confirm(mine ? t.comments.confirmDelete : t.comments.confirmDeleteOthers(author))) return;
                  remove.mutate(comment.id);
                }}
                data-action="delete-comment"
              >
                {t.comments.delete}
              </Button>
            )}
          </div>
        )}
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      </div>
    </div>
  );
}

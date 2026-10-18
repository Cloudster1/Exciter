import { Link } from "@tanstack/react-router";
import { useMarkRead, type Notification } from "@/api/notifications";
import { cx } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { threadAnchor } from "@/features/comments/CommentsSection";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A notification in the reader's words; the server sends only what happened. */
export function sentence(n: Notification): string {
  const scan = n.codeScan ? `${n.codeScan.repository} ${n.codeScan.branch}` : undefined;
  return t.notifications.sentence(n.kind, n.actorName, n.requirement?.key ?? n.session?.title ?? scan ?? "");
}

/** One notification, leading to its requirement at its thread when it has one; following it marks it read. */
export function NotificationRow({ notification: n, onFollow }: { notification: Notification; onFollow?: () => void }) {
  const markRead = useMarkRead();
  const unread = n.readAt === null;
  const words = (
    <>
      <span aria-hidden="true" className={cx("mt-1.5 size-2 shrink-0 rounded-full", unread ? "bg-accent" : "bg-transparent")} />
      <span className="min-w-0 flex-1">
        {unread && <span className="sr-only">{t.notifications.unread}: </span>}
        <span className={cx("block text-sm", unread && "font-medium")}>{sentence(n)}</span>
        {n.requirement && <span className="block truncate text-xs text-ink-muted">{n.requirement.title}</span>}
        {n.session && <span className="block truncate text-xs text-ink-muted">{n.session.projectKey}</span>}
        {n.codeScan && <span className="block truncate text-xs text-ink-muted">{n.codeScan.projectKey}</span>}
        {n.excerpt && <span className="mt-0.5 block truncate text-xs text-ink-muted italic">{n.excerpt}</span>}
        <span className="mt-0.5 block text-xs text-ink-subtle">{when.format(new Date(n.createdAt))}</span>
      </span>
    </>
  );
  const look = cx("flex w-full items-start gap-2 rounded-control px-2 py-2 text-left", unread ? "text-ink" : "text-ink-muted");
  const follow = () => {
    if (unread) markRead.mutate({ ids: [n.id] });
    onFollow?.();
  };
  // A code scan's leads to the project's outdated and orphaned tags (#201).
  if (n.codeScan) {
    return (
      <Link
        to="/p/$projectKey/code-tags"
        params={{ projectKey: n.codeScan.projectKey }}
        onClick={follow}
        className={cx(look, "hover:bg-surface-raised focus-visible:-outline-offset-2")}
        data-notification={n.kind}
        data-unread={unread || undefined}
      >
        {words}
      </Link>
    );
  }
  // A review session's leads to its page (#204).
  if (n.session) {
    return (
      <Link
        to="/p/$projectKey/sessions/$sessionId"
        params={{ projectKey: n.session.projectKey, sessionId: n.session.id }}
        onClick={follow}
        className={cx(look, "hover:bg-surface-raised focus-visible:-outline-offset-2")}
        data-notification={n.kind}
        data-unread={unread || undefined}
      >
        {words}
      </Link>
    );
  }
  // A requirement the reader may no longer view is not listed; one without a requirement has nowhere to lead.
  if (!n.requirement) {
    return (
      <div className={look} data-notification={n.kind} data-unread={unread || undefined}>
        {words}
      </div>
    );
  }
  return (
    <Link
      to="/p/$projectKey/r/$reqKey"
      params={{ projectKey: n.requirement.projectKey, reqKey: n.requirement.key }}
      hash={n.threadId ? threadAnchor(n.threadId) : undefined}
      onClick={() => {
        if (unread) markRead.mutate({ ids: [n.id] });
        onFollow?.();
      }}
      className={cx(look, "hover:bg-surface-raised focus-visible:-outline-offset-2")}
      data-notification={n.kind}
      data-unread={unread || undefined}
    >
      {words}
    </Link>
  );
}

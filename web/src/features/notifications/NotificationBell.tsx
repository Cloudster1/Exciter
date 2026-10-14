import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useLatestNotifications, useMarkRead, useUnreadCount } from "@/api/notifications";
import { Button, ErrorBanner, IconButton, Skeleton } from "@/components/ui";
import { useAnchored, useEscape, useFocusReturn, useOutsidePress } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { NOTIFICATION_SETTINGS_PATH, NOTIFICATIONS_PATH, UNREAD_BADGE_MAX } from "@/config";
import { t } from "@/i18n";
import { NotificationRow } from "./NotificationRow";

// The bell (#34), Stator's NotificationBell over requirements.

/** The top bar's bell: the unread count as a badge, and the latest notifications in a panel. */
export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const count = useUnreadCount();
  const unread = count.data ?? 0;
  const { refetch } = count;
  // The query library refetches when the tab becomes visible; a window that comes back from another application
  // only says so with focus.
  useEffect(() => {
    const onFocus = () => void refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [refetch]);
  const panelId = useId();
  const titleId = useId();
  const wrapRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  const outside = useRef([wrapRef, panelRef]);
  useEscape(open, close);
  useOutsidePress(open, outside.current, close);
  useFocusReturn(open, panelRef, false);
  const place = useAnchored(open, wrapRef, panelRef, { align: "end" });
  const badge = unread > UNREAD_BADGE_MAX ? `${UNREAD_BADGE_MAX}+` : String(unread);

  return (
    <div ref={wrapRef} className="relative inline-flex">
      <IconButton
        icon={<Icon.Bell />}
        label={unread > 0 ? t.notifications.bellUnread(unread) : t.notifications.bell}
        onClick={() => setOpen((was) => !was)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        data-action="notifications"
      />
      {unread > 0 && (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-2xs leading-4 font-medium text-on-accent"
          data-unread-badge={unread}
        >
          {badge}
        </span>
      )}
      {open && (
        <div
          ref={panelRef}
          id={panelId}
          role="dialog"
          aria-labelledby={titleId}
          style={place}
          className="fixed z-30 flex max-h-[70vh] w-[min(24rem,calc(100vw-1rem))] flex-col rounded-overlay border border-border bg-surface-overlay shadow-2"
          data-notification-panel=""
        >
          <NotificationPanel titleId={titleId} onClose={close} />
        </div>
      )}
    </div>
  );
}

function NotificationPanel({ titleId, onClose }: { titleId: string; onClose: () => void }) {
  const { data, error, isLoading, refetch } = useLatestNotifications(true);
  const markRead = useMarkRead();
  const items = data?.notifications ?? [];
  const anyUnread = items.some((n) => n.readAt === null);

  return (
    <>
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <h2 id={titleId} className="min-w-0 flex-1 text-sm font-semibold text-ink">
          {t.notifications.title}
        </h2>
        <Button
          size="sm"
          variant="ghost"
          disabled={!anyUnread}
          loading={markRead.isPending}
          onClick={() => markRead.mutate({ all: true })}
          data-action="mark-all-read"
        >
          {t.notifications.markAllRead}
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-1">
        {error ? (
          <div className="p-2">
            <ErrorBanner onRetry={() => void refetch()}>{t.notifications.failed}</ErrorBanner>
          </div>
        ) : isLoading ? (
          <Skeleton />
        ) : items.length === 0 ? (
          <p className="px-2 py-6 text-center text-sm text-ink-muted" data-notifications-empty="">
            {t.notifications.empty}
          </p>
        ) : (
          <ul aria-labelledby={titleId}>
            {items.map((n) => (
              <li key={n.id}>
                <NotificationRow notification={n} onFollow={onClose} />
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="flex flex-wrap justify-between gap-2 border-t border-border px-3 py-2 text-sm">
        <Link to={NOTIFICATIONS_PATH} onClick={onClose} className="text-accent hover:underline" data-action="all-notifications">
          {t.notifications.seeAll}
        </Link>
        <Link to={NOTIFICATION_SETTINGS_PATH} onClick={onClose} className="text-accent hover:underline" data-action="notification-settings">
          {t.notifications.settings}
        </Link>
      </div>
    </>
  );
}

import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMarkRead, useNotifications } from "@/api/notifications";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NOTIFICATION_SETTINGS_PATH, NOTIFICATIONS_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { NotificationRow } from "./NotificationRow";

/** Everything the caller was told, the latest first, a page at a time, or the unread alone. */
export function NotificationList() {
  const [unread, setUnread] = useState(false);
  const [offset, setOffset] = useState(0);
  const { data, error, isLoading, refetch } = useNotifications(unread, offset);
  const markRead = useMarkRead();
  const rows = data?.notifications ?? [];
  const total = data?.total ?? 0;

  return (
    <div className="mx-auto max-w-3xl" data-notification-list="">
      <PageHeader
        title={t.notifications.title}
        actions={
          <>
            <Button variant="secondary" loading={markRead.isPending} onClick={() => markRead.mutate({ all: true })} data-action="mark-all-read">
              {t.notifications.markAllRead}
            </Button>
            <Link to={NOTIFICATION_SETTINGS_PATH} className="self-center text-sm text-accent hover:underline" data-action="notification-settings">
              {t.notifications.settings}
            </Link>
          </>
        }
      />
      <div className="mb-3">
        <Checkbox
          label={t.notifications.unreadOnly}
          checked={unread}
          onChange={(event) => {
            setUnread(event.target.checked);
            setOffset(0);
          }}
          data-field="unread-only"
        />
      </div>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{t.notifications.failed}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Bell />} title={unread ? t.notifications.emptyUnread : t.notifications.empty} />
      ) : (
        <>
          <Card className="p-1">
            <ul aria-label={t.notifications.title}>
              {rows.map((n) => (
                <li key={n.id}>
                  <NotificationRow notification={n} />
                </li>
              ))}
            </ul>
          </Card>
          {total > NOTIFICATIONS_PAGE_SIZE && (
            <nav aria-label={t.notifications.title} className="mt-4 flex items-center justify-between gap-2">
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - NOTIFICATIONS_PAGE_SIZE))}>
                {t.notifications.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.notifications.range(offset + 1, offset + rows.length, total)}</span>
              <Button
                variant="secondary"
                size="sm"
                disabled={offset + NOTIFICATIONS_PAGE_SIZE >= total}
                onClick={() => setOffset(offset + NOTIFICATIONS_PAGE_SIZE)}
              >
                {t.notifications.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  );
}

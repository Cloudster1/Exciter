import type { Requirement } from "@/api/requirements";
import { useWatchers } from "@/api/watching";
import { Avatar, Dialog, ErrorBanner, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

/** Everybody who hears about the requirement, by name, and through which watch. */
export function WatchersDialog({ requirement, onClose }: { requirement: Requirement; onClose: () => void }) {
  const { data, error, isLoading, refetch } = useWatchers(requirement.key);
  return (
    <Dialog open title={t.watch.watchersTitle(requirement.key)} onClose={onClose} attrs={{ "data-watchers-dialog": "" }}>
      <p className="mb-3 text-sm text-ink-muted">{t.watch.watchersIntro}</p>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : data.watchers.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.watch.watchersNone}</p>
      ) : (
        <>
          <ul className="space-y-2" aria-label={t.watch.watchers}>
            {data.watchers.map((watcher) => (
              <li key={watcher.userId} className="flex items-center gap-2" data-watcher={watcher.name}>
                <Avatar name={watcher.name} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-ink">{watcher.name}</span>
                  <span className="block truncate text-xs text-ink-muted">{t.watch.via[watcher.via]}</span>
                </span>
              </li>
            ))}
          </ul>
          {data.total > data.watchers.length && <p className="mt-3 text-xs text-ink-muted">{t.watch.watchersMore(data.watchers.length, data.total)}</p>}
        </>
      )}
    </Dialog>
  );
}

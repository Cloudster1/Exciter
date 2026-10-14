import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useStopWatching, useWatches, type Watch } from "@/api/watching";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { WATCHES_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

// The caller's watches (#34), Stator's WatchingList over requirements and projects.

const since = localDateFormat({ dateStyle: "medium" });

function named(watch: Watch): string {
  return watch.requirement ? `${watch.requirement.key} ${watch.requirement.title}` : t.watch.projectNamed(watch.projectKey);
}

/** The requirements and projects the caller watches, the latest first, each with a way to stop. */
export function WatchingList() {
  const [offset, setOffset] = useState(0);
  const { data, error, isLoading, refetch } = useWatches(offset);
  const stop = useStopWatching();
  const [notice, setNotice] = useState("");
  const rows = data?.watches ?? [];
  const total = data?.total ?? 0;

  return (
    <div className="mx-auto max-w-3xl" data-watching-list="">
      <PageHeader title={t.watch.listTitle} meta={t.watch.listIntro} />
      {stop.error && <ErrorBanner>{t.watch.failed}</ErrorBanner>}
      <p role="status" className="mb-2 text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Eye />} title={t.watch.listEmpty} description={t.watch.listEmptyBody} />
      ) : (
        <>
          <Table>
            <thead>
              <tr>
                <Th>{t.watch.columnWhat}</Th>
                <Th>{t.watch.columnKind}</Th>
                <Th>{t.watch.columnSince}</Th>
                <Th className="w-10">
                  <span className="sr-only">{t.watch.columnActions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((watch) => (
                <tr key={watch.id} data-watch-row={named(watch)}>
                  <Td>
                    {watch.requirement ? (
                      <Link
                        to="/p/$projectKey/r/$reqKey"
                        params={{ projectKey: watch.requirement.projectKey, reqKey: watch.requirement.key }}
                        className="font-medium text-accent hover:underline"
                      >
                        <span className="font-mono">{watch.requirement.key}</span> {watch.requirement.title}
                      </Link>
                    ) : (
                      <Link to="/p/$projectKey" params={{ projectKey: watch.projectKey }} search={{}} className="font-medium text-accent hover:underline">
                        {t.watch.projectNamed(watch.projectKey)}
                      </Link>
                    )}
                  </Td>
                  <Td className="text-ink-muted">{t.watch.kind[watch.kind]}</Td>
                  <Td className="text-ink-muted">{since.format(new Date(watch.createdAt))}</Td>
                  <Td className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={t.watch.stopNamed(named(watch))}
                      loading={stop.isPending && stop.variables === watch}
                      onClick={() => stop.mutate(watch, { onSuccess: () => setNotice(t.watch.stopped(named(watch))) })}
                      data-action="stop-watching"
                    >
                      {t.watch.stop}
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
          {total > WATCHES_PAGE_SIZE && (
            <nav aria-label={t.watch.listTitle} className="mt-4 flex items-center justify-between gap-2">
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - WATCHES_PAGE_SIZE))}>
                {t.watch.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.watch.range(offset + 1, offset + rows.length, total)}</span>
              <Button variant="secondary" size="sm" disabled={offset + WATCHES_PAGE_SIZE >= total} onClick={() => setOffset(offset + WATCHES_PAGE_SIZE)}>
                {t.watch.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  );
}

import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useSuspects, type SuspectLink } from "@/api/links";
import type { Project } from "@/api/projects";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, useToast, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SUSPECTS_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { ClearLinkDialog } from "./Suspect";

const waitingSince = localDateFormat({ dateStyle: "medium" });

function End({ end }: { end: SuspectLink["source"] }) {
  return (
    <Link
      to="/projects/$projectKey/r/$reqKey"
      params={{ projectKey: end.projectKey, reqKey: end.key }}
      className="min-w-0 truncate text-sm font-medium text-ink hover:text-accent hover:underline"
    >
      <span className="font-mono">{end.key}</span> {end.title}
    </Link>
  );
}

/** The suspect links touching a project's requirements, the longest waiting first, each checked in place. */
export function SuspectList({ project }: { project: Project }) {
  const [offset, setOffset] = useState(0);
  const [checking, setChecking] = useState<SuspectLink | null>(null);
  const toast = useToast();
  const { data, error, refetch, isLoading } = useSuspects(project.key, offset);
  const rows = data?.links ?? [];
  const total = data?.total ?? 0;
  const mayCheck = project.can.edit && !project.archivedAt;
  return (
    <Page width="content" data-suspect-list={project.key}>
      <PageHeader title={t.suspect.listTitle} meta={t.suspect.listIntro} />
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Check />} title={t.suspect.none} />
      ) : (
        <>
          <ul className="flex flex-col divide-y divide-border" aria-label={t.suspect.listTitle}>
            {rows.map((link) => (
              <li key={link.id} className="flex flex-wrap items-start gap-3 py-3" data-suspect-link={link.id}>
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <End end={link.source} />
                  <span className="text-xs text-ink-muted">{link.type.forward}</span>
                  <End end={link.target} />
                  <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-ink-subtle">
                    {link.ends.map((moved) => {
                      const key = moved.end === "source" ? link.source.key : link.target.key;
                      return (
                        <span key={moved.end} data-moved={key}>
                          {t.suspect.moved(key, moved.reviewed, moved.current)}
                        </span>
                      );
                    })}
                    <span aria-hidden="true">·</span>
                    <span>{t.suspect.since(waitingSince.format(new Date(link.since)))}</span>
                  </span>
                </div>
                {mayCheck && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setChecking(link)}
                    aria-label={t.suspect.dialogTitle(link.source.key, link.type.forward, link.target.key)}
                    data-action="check-link"
                  >
                    {t.suspect.check}
                  </Button>
                )}
              </li>
            ))}
          </ul>
          {total > SUSPECTS_PAGE_SIZE && (
            <nav aria-label={t.suspect.listTitle} className="mt-4 flex items-center justify-between gap-2">
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - SUSPECTS_PAGE_SIZE))}>
                {t.suspect.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.suspect.range(offset + 1, offset + rows.length, total)}</span>
              <Button variant="secondary" size="sm" disabled={offset + SUSPECTS_PAGE_SIZE >= total} onClick={() => setOffset(offset + SUSPECTS_PAGE_SIZE)}>
                {t.suspect.next}
              </Button>
            </nav>
          )}
        </>
      )}
      {checking && (
        <ClearLinkDialog
          link={{ id: checking.id, forward: checking.type.forward, source: checking.source, target: checking.target, ends: checking.ends }}
          onCleared={() => toast.success(t.suspect.cleared(checking.target.key))}
          onClose={() => setChecking(null)}
        />
      )}
    </Page>
  );
}

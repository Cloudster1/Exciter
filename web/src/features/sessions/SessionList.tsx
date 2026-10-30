import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useMySessions, useProjectSessions, type SessionSummary } from "@/api/sessions";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Tag, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { OpenSessionDialog } from "./OpenSessionDialog";

const on = localDateFormat({ dateStyle: "medium" });

/** One session as a list shows it: its title, round, counts and deadline, and what waits for the reader. */
function SessionRow({ session: s }: { session: SessionSummary }) {
  return (
    <li className="flex flex-col gap-0.5 py-3" data-session={s.title}>
      <span className="flex flex-wrap items-center gap-2">
        <Link
          to="/p/$projectKey/sessions/$sessionId"
          params={{ projectKey: s.projectKey, sessionId: s.id }}
          className="text-sm font-medium text-ink hover:text-accent hover:underline"
        >
          {s.title}
        </Link>
        <Tag>{t.sessions.state[s.state]}</Tag>
        {s.waiting > 0 && <Tag data-session-waiting={s.waiting}>{t.sessions.waiting(s.waiting)}</Tag>}
      </span>
      <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-ink-subtle">
        <span>{s.projectKey}</span>
        <span aria-hidden="true">·</span>
        <span>{t.sessions.round(s.round)}</span>
        <span aria-hidden="true">·</span>
        <span>{t.sessions.counts(s.counts.approved, s.counts.rejected, s.counts.pending, s.counts.total)}</span>
        {s.deadline && (
          <>
            <span aria-hidden="true">·</span>
            <span>{t.sessions.due(on.format(new Date(s.deadline)))}</span>
          </>
        )}
      </span>
    </li>
  );
}

/** A project's review sessions, open first, with the editors' way to open one. */
export function SessionList({ project }: { project: Project }) {
  const { data, error, refetch } = useProjectSessions(project.key);
  const [opening, setOpening] = useState(false);
  const mayOpen = project.can.edit && !project.archivedAt;
  return (
    <Page width="narrow" className="space-y-4" data-session-list="">
      <PageHeader
        title={t.sessions.title}
        meta={t.sessions.intro}
        actions={
          mayOpen && (
            <Button icon={<Icon.Plus />} onClick={() => setOpening(true)} data-action="open-session">
              {t.sessions.open}
            </Button>
          )
        }
      />
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.sessions.length === 0 ? (
        <EmptyState icon={<Icon.Check />} title={t.sessions.none} />
      ) : (
        <ul className="flex flex-col divide-y divide-border" aria-label={t.sessions.title}>
          {data.sessions.map((s) => (
            <SessionRow key={s.id} session={s} />
          ))}
        </ul>
      )}
      {opening && <OpenSessionDialog project={project} onClose={() => setOpening(false)} />}
    </Page>
  );
}

/** The open sessions the reader takes part in, above the single reviews of the inbox. */
export function MySessions() {
  const { data, error, refetch } = useMySessions();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return (
    <section aria-labelledby="my-sessions-title" className="mb-6 space-y-2" data-my-sessions="">
      <h2 id="my-sessions-title" className="text-base font-semibold text-ink">
        {t.sessions.inbox}
      </h2>
      {data.sessions.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.sessions.inboxEmpty}</p>
      ) : (
        <ul className="flex flex-col divide-y divide-border" aria-label={t.sessions.inbox}>
          {data.sessions.map((s) => (
            <SessionRow key={s.id} session={s} />
          ))}
        </ul>
      )}
    </section>
  );
}

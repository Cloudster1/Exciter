import { useId, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { BASELINE_NAME_MAX_LENGTH } from "@/config";
import type { Project } from "@/api/projects";
import { sessionReportHref, useSession, useSessionActions, type ReviewSession } from "@/api/sessions";
import {
  Button,
  ButtonLink,
  Checkbox,
  Dialog,
  ErrorBanner,
  Field,
  PageHeader,
  SectionTitle,
  Skeleton,
  Table,
  Tag,
  Td,
  Th,
  useToast,
  DialogFooter,
  Page,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { FileExportDialog } from "@/features/print/FileExport";

const on = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

function scopeWords(s: ReviewSession): string {
  if (s.scope.kind === "section") return t.sessions.scopeOf.section(s.scope.rootKey ?? "");
  if (s.scope.kind === "query") return t.sessions.scopeOf.query(s.scope.query ?? "");
  return t.sessions.scopeOf.baseline;
}

/** A review session (#204): where each requirement and each person stands, and the moderator's rounds and close. */
export function SessionPage({ project, sessionId }: { project: Project; sessionId: string }) {
  const { data: session, error, refetch } = useSession(sessionId);
  const [asking, setAsking] = useState<"round" | "close" | null>(null);
  const toast = useToast();
  const [word, setWord] = useState(false);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!session) return <Skeleton />;
  const s = session;
  return (
    <Page width="content" className="space-y-5" data-session-page={s.id} data-session-state={s.state}>
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey/sessions" params={{ projectKey: project.key }} className="hover:text-accent hover:underline">
            {t.sessions.back}
          </Link>
        }
        title={s.title}
        meta={[
          t.sessions.state[s.state],
          t.sessions.round(s.round),
          scopeWords(s),
          t.sessions.moderatedBy(s.moderatorName),
          s.deadline ? t.sessions.due(on.format(new Date(s.deadline))) : "",
        ]
          .filter(Boolean)
          .join(" · ")}
      />
      {s.description && <p className="text-sm text-ink-muted">{s.description}</p>}
      <p className="text-sm text-ink" data-session-counts="">
        {t.sessions.counts(s.counts.approved, s.counts.rejected, s.counts.pending, s.counts.total)}
      </p>
      <div className="flex flex-wrap gap-2">
        {s.state === "open" && (
          <ButtonLink
            variant="primary"
            href={`/projects/${encodeURIComponent(project.key)}/spec?session=${s.id}&waiting=true&changes=round`}
            data-action="review-session-in-document"
          >
            {t.sessions.reviewInDocument}
          </ButtonLink>
        )}
        {s.can.round && (
          <Button variant="secondary" onClick={() => setAsking("round")} data-action="next-round">
            {t.sessions.nextRound}
          </Button>
        )}
        {s.can.close && (
          <Button variant="secondary" onClick={() => setAsking("close")} data-action="close-session">
            {t.sessions.close}
          </Button>
        )}
        <ButtonLink variant="secondary" icon={<Icon.Download />} href={sessionReportHref(s.id, "pdf")} data-action="report-pdf">
          {t.sessions.reportPdf}
        </ButtonLink>
        <Button variant="secondary" icon={<Icon.Download />} onClick={() => setWord(true)} data-action="report-docx">
          {t.sessions.reportDocx}
        </Button>
        {word && (
          <FileExportDialog format="docx" href={sessionReportHref(s.id, "docx")} title={s.title} projectKey={project.key} onClose={() => setWord(false)} />
        )}
      </div>

      <section aria-labelledby="session-requirements" className="space-y-2">
        <SectionTitle id="session-requirements">{t.sessions.requirements}</SectionTitle>
        <Table aria-labelledby="session-requirements" dense>
          <thead>
            <tr>
              <Th>{t.sessions.requirements}</Th>
              <Th>{t.sessions.revision}</Th>
              <Th>{t.sessions.outcome}</Th>
            </tr>
          </thead>
          <tbody>
            {s.items.map((item) => (
              <tr key={item.requirement.id} data-session-item={item.requirement.key} data-session-outcome={item.review.state}>
                <Td>
                  <span className="text-ink-muted tabular-nums">{item.outline}</span>{" "}
                  <Link
                    to="/projects/$projectKey/r/$reqKey"
                    params={{ projectKey: project.key, reqKey: item.requirement.key }}
                    className="font-mono text-xs text-ink-muted hover:text-accent"
                  >
                    {item.requirement.key}
                  </Link>{" "}
                  {item.requirement.title}
                  {item.openThreads > 0 && <span className="block text-xs text-ink-subtle">{t.sessions.openThreads(item.openThreads)}</span>}
                </Td>
                <Td>
                  {item.revision}
                  {item.round > 1 && <span className="block text-xs text-ink-subtle">{t.sessions.round(item.round)}</span>}
                </Td>
                <Td>
                  <Tag>{t.sessions.outcomes[item.review.state] ?? item.review.state}</Tag>
                  <span className="block text-xs text-ink-subtle">{t.sessions.approvals(item.review.approvals, item.review.required)}</span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {s.excluded.length > 0 && (
          <p className="text-xs text-ink-muted" data-session-excluded="">
            {t.sessions.excluded}: {s.excluded.map((e) => `${e.key} (${t.sessions.excludedReasons[e.reason] ?? e.reason})`).join(", ")}
          </p>
        )}
      </section>

      <section aria-labelledby="session-progress" className="space-y-2">
        <SectionTitle id="session-progress">{t.sessions.progress}</SectionTitle>
        <Table aria-labelledby="session-progress" dense>
          <thead>
            <tr>
              <Th>{t.sessions.person}</Th>
              <Th>{t.sessions.roleColumn}</Th>
              <Th>{t.sessions.decided}</Th>
              <Th>{t.sessions.commented}</Th>
            </tr>
          </thead>
          <tbody>
            {s.progress.map((p) => (
              <tr key={p.person.id} data-session-progress={p.person.name}>
                <Td>{p.person.name}</Td>
                <Td>{t.sessions.roles[p.role] ?? p.role}</Td>
                <Td>{p.role === "approver" ? t.sessions.of(p.decided, p.of) : ""}</Td>
                <Td>{t.sessions.of(p.commented, p.of)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </section>

      {asking === "round" && (
        <RoundDialog
          project={project}
          session={s}
          onClose={() => setAsking(null)}
          onDone={(next) => {
            setAsking(null);
            toast.success(t.sessions.roundNotice(next.round));
          }}
        />
      )}
      {asking === "close" && (
        <CloseDialog
          project={project}
          session={s}
          onClose={() => setAsking(null)}
          onDone={() => {
            setAsking(null);
            toast.success(t.sessions.closedNotice);
          }}
        />
      )}
    </Page>
  );
}

function RoundDialog({
  project,
  session,
  onClose,
  onDone,
}: {
  project: Project;
  session: ReviewSession;
  onClose: () => void;
  onDone: (s: ReviewSession) => void;
}) {
  const formId = useId();
  const { round } = useSessionActions(project.key);
  const [comment, setComment] = useState("");
  function submit(event: FormEvent) {
    event.preventDefault();
    round.mutate({ id: session.id, comment: comment.trim() || undefined }, { onSuccess: onDone });
  }
  return (
    <Dialog open title={t.sessions.nextRoundTitle} onClose={onClose}>
      <form id={formId} onSubmit={submit} className="space-y-3" aria-label={t.sessions.nextRoundTitle}>
        <p className="text-sm text-ink-muted">{t.sessions.nextRoundIntro}</p>
        {round.error && <ErrorBanner>{round.error.message}</ErrorBanner>}
        <Field label={t.sessions.roundComment} value={comment} rows={2} onChange={(event) => setComment(event.target.value)} />
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.sessions.cancel}
          </Button>
          <Button type="submit" form={formId} loading={round.isPending} data-action="start-round">
            {t.sessions.start}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

function CloseDialog({ project, session, onClose, onDone }: { project: Project; session: ReviewSession; onClose: () => void; onDone: () => void }) {
  const formId = useId();
  const { close } = useSessionActions(project.key);
  const [take, setTake] = useState(false);
  const [name, setName] = useState(session.title);
  function submit(event: FormEvent) {
    event.preventDefault();
    close.mutate({ id: session.id, baseline: take ? { name: name.trim() } : undefined }, { onSuccess: onDone });
  }
  return (
    <Dialog open title={t.sessions.closeTitle} onClose={onClose}>
      <form id={formId} onSubmit={submit} className="space-y-3" aria-label={t.sessions.closeTitle}>
        <p className="text-sm text-ink-muted">{t.sessions.closeIntro}</p>
        {close.error && <ErrorBanner>{close.error.message}</ErrorBanner>}
        <Checkbox label={t.sessions.takeBaseline} checked={take} onChange={(event) => setTake(event.target.checked)} />
        {take && <Field label={t.sessions.baselineName} value={name} maxLength={BASELINE_NAME_MAX_LENGTH} onChange={(event) => setName(event.target.value)} />}
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.sessions.cancel}
          </Button>
          <Button type="submit" form={formId} loading={close.isPending} disabled={take && !name.trim()} data-action="close-session-send">
            {t.sessions.closeSend}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

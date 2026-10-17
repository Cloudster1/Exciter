import { useEffect } from "react";
import { useSessionReport, type SessionReport } from "@/api/sessions";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useLightScheme } from "./PrintDocument";
import { usePrintReady } from "./printReady";

// A review session's report as the render service prints it (#204): the session, who took part and how far, and
// every requirement's decisions round by round.

const day = localDateFormat({ dateStyle: "long" });

export function PrintSessionReport({ sessionId }: { sessionId: string }) {
  const { data, error } = useSessionReport(sessionId);
  useLightScheme();
  usePrintReady(Boolean(data) && !error);
  useEffect(() => {
    if (data) document.title = data.session.title;
  }, [data]);
  if (error) {
    return (
      <main className="print-view">
        <p role="alert" data-print-failed="">
          {error.message || t.documents.printFailed}
        </p>
      </main>
    );
  }
  if (!data) return <main className="print-view" aria-busy="true" />;
  return (
    <main className="print-view" data-print-view="">
      <div hidden data-print-meta="" data-header={data.session.projectKey} data-title={data.session.title} data-footer="" data-pages={t.documents.pages} />
      <SessionReportView report={data} />
    </main>
  );
}

/** A session's report as one document. */
export function SessionReportView({ report }: { report: SessionReport }) {
  const s = report.session;
  return (
    <article className="print-document space-y-6" data-print-document="session">
      <section className="print-cover space-y-1">
        <h1 className="text-4xl font-semibold text-ink">{s.title}</h1>
        <p className="text-ink-muted">{t.sessions.moderatedBy(s.moderatorName)}</p>
        <p className="text-ink-muted">{t.sessions.opened(day.format(new Date(s.openedAt)))}</p>
        {s.closedAt && <p className="text-ink-muted">{t.sessions.closedOn(day.format(new Date(s.closedAt)))}</p>}
        <p className="text-ink">{t.sessions.counts(s.counts.approved, s.counts.rejected, s.counts.pending, s.counts.total)}</p>
        {report.baselineName && <p className="font-semibold text-ink">{t.sessions.baselineTaken(report.baselineName)}</p>}
      </section>
      <section className="space-y-2">
        <h2 className="text-2xl font-semibold text-ink">{t.sessions.progress}</h2>
        <table className="w-full text-sm">
          <thead>
            <tr>
              <th className="text-left">{t.sessions.person}</th>
              <th className="text-left">{t.sessions.roleColumn}</th>
              <th className="text-left">{t.sessions.decided}</th>
              <th className="text-left">{t.sessions.commented}</th>
            </tr>
          </thead>
          <tbody>
            {s.progress.map((p) => (
              <tr key={p.person.id}>
                <td>{p.person.name}</td>
                <td>{t.sessions.roles[p.role] ?? p.role}</td>
                <td>{t.sessions.of(p.decided, p.of)}</td>
                <td>{t.sessions.of(p.commented, p.of)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="space-y-2">
        <h2 className="text-2xl font-semibold text-ink">{t.sessions.rounds}</h2>
        {report.rounds.map((r) => (
          <p key={r.number} className="text-sm">
            {t.sessions.round(r.number)}, {day.format(new Date(r.startedAt))}
            {r.comment ? `: ${r.comment}` : ""}
          </p>
        ))}
      </section>
      <section className="space-y-4">
        <h2 className="text-2xl font-semibold text-ink">{t.sessions.requirements}</h2>
        {report.items.map((item) => (
          <div key={item.requirement.id} className="space-y-1" data-report-item={item.requirement.key}>
            <h3 className="text-lg font-semibold text-ink">
              {item.outline} {item.requirement.key} {item.requirement.title}
            </h3>
            {item.rounds.map((r) => (
              <p key={r.round} className="text-sm">
                {t.sessions.round(r.round)}, {t.sessions.revision} {r.revision}: {t.sessions.outcomes[r.state] ?? r.state}
                {r.decisions.length > 0 && ` (${r.decisions.map((d) => `${d.reviewerName}: ${d.decision}${d.comment ? `, ${d.comment}` : ""}`).join("; ")})`}
              </p>
            ))}
            {item.openThreads > 0 && <p className="text-sm text-ink-muted italic">{t.sessions.openThreads(item.openThreads)}</p>}
          </div>
        ))}
      </section>
    </article>
  );
}

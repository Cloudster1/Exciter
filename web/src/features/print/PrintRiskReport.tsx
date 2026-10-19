import { useEffect } from "react";
import { useRiskReport, type RiskReport } from "@/api/risks";
import { RatingView } from "@/features/risks/RatingView";
import { t } from "@/i18n";
import { useLightScheme } from "./PrintDocument";
import { usePrintReady } from "./printReady";

// A project's risk report as the render service prints it (#261): the levels and how many risks stand at each,
// then every risk with its ratings and mitigations.

export function PrintRiskReport({ projectKey, query }: { projectKey: string; query?: string }) {
  const { data, error } = useRiskReport(projectKey, query);
  useLightScheme();
  usePrintReady(Boolean(data) && !error);
  useEffect(() => {
    if (data) document.title = t.risks.printTitle(data.projectName);
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
      <div hidden data-print-meta="" data-header={data.projectKey} data-title={t.risks.reportTitle} data-footer="" data-pages={t.documents.pages} />
      <RiskReportView report={data} />
    </main>
  );
}

/** A risk report as one document. */
export function RiskReportView({ report }: { report: RiskReport }) {
  return (
    <article className="print-document space-y-6" data-print-document="risks">
      <section className="print-cover space-y-1">
        <h1 className="text-4xl font-semibold text-ink">{t.risks.reportTitle}</h1>
        <p className="text-ink-muted">
          {report.projectName} ({report.projectKey})
        </p>
        <p className="text-ink-muted">{t.risks.methods[report.scheme.method]}</p>
      </section>
      <section className="space-y-2">
        <h2 className="text-xl font-semibold text-ink">{t.risks.levels}</h2>
        <table className="w-full text-sm" data-report-levels="">
          <thead>
            <tr>
              <th className="text-left">{t.risks.level}</th>
              <th className="text-left">{t.risks.columns.before}</th>
              <th className="text-left">{t.risks.matrixSides.effective}</th>
            </tr>
          </thead>
          <tbody>
            {report.levels.map((l) => (
              <tr key={l.level.name}>
                <td>{l.level.name}</td>
                <td>{l.before}</td>
                <td>{l.effective}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {report.unrated > 0 && <p className="text-sm text-ink-muted">{t.risks.unrated(report.unrated)}</p>}
      </section>
      {report.risks.map((r) => (
        <section key={r.risk.id} className="print-entry space-y-2" data-print-risk={r.risk.key}>
          <h2 className="text-xl font-semibold text-ink">
            {r.risk.key} {r.risk.title}
          </h2>
          <p className="text-sm text-ink-muted">
            {r.risk.type.name}
            {r.rating.state && `, ${t.risks.states[r.rating.state]}`}
            {r.rating.level && `, ${r.rating.level.name}`}
          </p>
          <RatingView view={r.rating} small />
          {r.mitigations.length === 0 ? (
            <p className="text-sm text-ink-muted">{t.risks.noMitigations}</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr>
                  <th className="text-left">{t.risks.columns.mitigatedBy}</th>
                  <th className="text-left">{t.coverage.dimensions.implemented}</th>
                  <th className="text-left">{t.coverage.dimensions.verified}</th>
                  <th className="text-left">{t.risks.inEffect}</th>
                </tr>
              </thead>
              <tbody>
                {r.mitigations.map((m) => (
                  <tr key={m.linkId}>
                    <td>{m.source.hidden ? t.risks.hidden : `${m.source.key} ${m.source.title}`}</td>
                    <td>{t.coverage.levels[m.implemented]}</td>
                    <td>{t.coverage.levels[m.verified]}</td>
                    <td>{m.inEffect ? t.risks.inEffect : t.risks.notInEffect}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      ))}
    </article>
  );
}

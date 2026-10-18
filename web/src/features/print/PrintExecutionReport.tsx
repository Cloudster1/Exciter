import { useEffect } from "react";
import { useExecutionReport, type ExecutionReport } from "@/api/tests";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { progressOf } from "@/features/tests/TestsPage";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useLightScheme } from "./PrintDocument";
import { usePrintReady } from "./printReady";

// A test execution's report as the render service prints it (#251): its target and testers, then every case
// iteration with its steps and what they came to.

const day = localDateFormat({ dateStyle: "long" });

export function PrintExecutionReport({ executionId }: { executionId: string }) {
  const { data, error } = useExecutionReport(executionId);
  useLightScheme();
  usePrintReady(Boolean(data) && !error);
  useEffect(() => {
    if (data) document.title = t.tests.printTitle(data.execution.title);
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
      <div hidden data-print-meta="" data-header={data.execution.projectKey} data-title={data.execution.title} data-footer="" data-pages={t.documents.pages} />
      <ExecutionReportView report={data} />
    </main>
  );
}

/** An execution's report as one document. */
export function ExecutionReportView({ report }: { report: ExecutionReport }) {
  const e = report.execution;
  const lines = [
    e.planTitle && `${t.tests.plan}: ${e.planTitle}`,
    e.baseline && `${t.tests.baseline}: ${e.baseline.name}`,
    e.version && `${t.tests.version}: ${e.version}`,
    e.configuration && `${t.tests.configuration}: ${e.configuration}`,
    t.tests.managedBy(e.manager.name),
    `${t.tests.testers}: ${e.testers.map((s) => s.name).join(", ")}`,
    `${t.tests.state[e.state]}${e.closedAt ? `, ${day.format(new Date(e.closedAt))}` : ""}`,
  ].filter(Boolean);
  return (
    <article className="print-document space-y-6" data-print-document="execution">
      <section className="print-cover space-y-1">
        <h1 className="text-4xl font-semibold text-ink">{e.title}</h1>
        {lines.map((line) => (
          <p key={line as string} className="text-ink-muted">
            {line}
          </p>
        ))}
        <p className="font-semibold text-ink">{progressOf(e)}</p>
        {e.closeComment && <p className="text-ink">{e.closeComment}</p>}
      </section>
      {report.cases.map((c) => {
        const results = new Map(c.results.map((r) => [r.step, r]));
        const values = Object.entries(c.run.values ?? {})
          .map(([name, v]) => `${name} = ${v}`)
          .join(", ");
        return (
          <section key={`${c.run.case.key}-${c.run.iteration}`} className="print-entry space-y-2" data-print-case={`${c.run.case.key}#${c.run.iteration}`}>
            <h2 className="text-xl font-semibold text-ink">
              {c.run.case.key} {c.run.case.title} {values && `(${values})`}
            </h2>
            <p className="text-sm text-ink-muted">
              {c.run.outcome ? t.tests.outcome[c.run.outcome] : t.tests.notRun}
              {c.run.recordedBy && `, ${c.run.recordedBy.name}`}
              {c.run.ci && `, ${t.tests.fromCI}`}
            </p>
            {c.run.note && <p className="text-sm text-ink">{c.run.note}</p>}
            {c.steps.length > 0 && (
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="w-8 text-left">#</th>
                    <th className="text-left">{t.tests.action}</th>
                    <th className="text-left">{t.tests.expected}</th>
                    <th className="text-left">{t.tests.outcomeColumn}</th>
                    <th className="text-left">{t.tests.actual}</th>
                  </tr>
                </thead>
                <tbody>
                  {c.steps.map((s, i) => {
                    const r = results.get(i + 1);
                    return (
                      <tr key={i}>
                        <td className="align-top">{i + 1}</td>
                        <td className="align-top">
                          <DocView doc={s.action as Doc} />
                        </td>
                        <td className="align-top">{s.expected ? <DocView doc={s.expected as Doc} /> : null}</td>
                        <td className="align-top">{r?.outcome ? t.tests.outcome[r.outcome] : t.tests.notRun}</td>
                        <td className="align-top">{r?.note}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
            {c.files.length > 0 && (
              <p className="text-xs text-ink-muted">
                {t.tests.files}: {c.files.join(", ")}
              </p>
            )}
            {c.issues.length > 0 && (
              <p className="text-sm font-semibold text-ink">
                {t.tests.issues}: {c.issues.join(", ")}
              </p>
            )}
          </section>
        );
      })}
    </article>
  );
}

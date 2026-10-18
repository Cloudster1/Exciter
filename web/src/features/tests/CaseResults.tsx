import { Link } from "@tanstack/react-router";
import { useTestCaseResults } from "@/api/tests";
import { SectionTitle, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A test case's results in every execution, newest first, on its page. */
export function CaseResults({ projectKey, caseKey }: { projectKey: string; caseKey: string }) {
  const { data: results } = useTestCaseResults(caseKey);
  return (
    <section aria-labelledby="case-results" className="space-y-2" data-case-results>
      <SectionTitle id="case-results">{t.tests.results}</SectionTitle>
      {!results || results.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.tests.noResults}</p>
      ) : (
        <Table aria-label={t.tests.resultsLabel} dense>
          <thead>
            <tr>
              <Th>{t.tests.executionTitle}</Th>
              <Th>{t.tests.iterationColumn}</Th>
              <Th>{t.tests.outcomeColumn}</Th>
              <Th>{t.tests.version}</Th>
            </tr>
          </thead>
          <tbody>
            {results.map((r) => (
              <tr key={`${r.executionId}-${r.iteration}`}>
                <Td>
                  <Link
                    to="/p/$projectKey/tests/executions/$executionId"
                    params={{ projectKey, executionId: r.executionId }}
                    className="text-ink hover:underline"
                  >
                    {r.executionTitle}
                  </Link>{" "}
                  <span className="text-xs text-ink-muted">{t.tests.state[r.state]}</span>
                </Td>
                <Td>{r.iteration}</Td>
                <Td>
                  {r.outcome ? t.tests.outcome[r.outcome] : t.tests.notRun}
                  {r.recordedAt && <span className="block text-xs text-ink-muted">{when.format(new Date(r.recordedAt))}</span>}
                </Td>
                <Td>{[r.version, r.configuration].filter(Boolean).join(", ")}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  );
}

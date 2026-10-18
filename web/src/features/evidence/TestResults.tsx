import { useTestResults, type TestResult } from "@/api/testResults";
import type { Requirement } from "@/api/requirements";
import { Card, ErrorBanner, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { TEST_COMMIT_SHOWN } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const at = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

const outcomeTone: Record<string, string> = {
  passed: "border-success/40 text-success",
  failed: "border-danger/40 text-danger",
  errored: "border-danger/40 text-danger",
  skipped: "",
};

/** The latest CI result of each test that verifies a requirement, the counting branch first, each saying when, on
 * which commit and run, and whether a newer revision outdated it. */
export function TestResults({ requirement }: { requirement: Requirement }) {
  const section = requirement.kind === "section";
  const { data, error, refetch } = useTestResults(requirement.key, !section);
  if (section) return null;
  const l = t.evidence.results;
  const branches = data ? [...new Set(data.results.map((r) => r.branch))] : [];
  return (
    <Card className="space-y-3 p-4" data-test-results>
      <section aria-labelledby="test-results-title" className="space-y-3">
        <SectionTitle id="test-results-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && data.results.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {branches.map((branch) => (
          <div key={branch} className="space-y-1" data-test-branch={branch}>
            <h3 className="text-sm font-medium text-ink">{branch === data?.countingBranch ? l.counting(branch) : l.other(branch)}</h3>
            <ul className="space-y-2">
              {data?.results
                .filter((r) => r.branch === branch)
                .map((r) => (
                  <ResultRow key={`${r.repository} ${r.job} ${r.suite} ${r.test}`} result={r} />
                ))}
            </ul>
          </div>
        ))}
      </section>
    </Card>
  );
}

function ResultRow({ result: r }: { result: TestResult }) {
  const l = t.evidence.results;
  const sha = r.commit.slice(0, TEST_COMMIT_SHOWN);
  return (
    <li className="space-y-0.5 text-sm" data-test-result={r.test} data-outcome={r.outcome} data-outdated={r.outdated}>
      <div className="flex flex-wrap items-center gap-2">
        <Tag className={outcomeTone[r.outcome]}>{l.outcome[r.outcome]}</Tag>
        <span className="font-medium text-ink">{r.test}</span>
        {r.outdated && (
          <Tag title={l.outdatedHint} data-test-outdated>
            {l.outdated(r.revision)}
          </Tag>
        )}
      </div>
      <p className="flex flex-wrap gap-x-3 text-xs text-ink-muted">
        <span>{r.suite && r.suite !== r.test ? `${r.repository} ${r.suite}` : r.repository}</span>
        {r.job && <span>{l.job(r.job)}</span>}
        <span>
          {r.armature ? (
            <a className="underline" href={r.armature.url} target="_blank" rel="noreferrer">
              {l.commit(sha)}
            </a>
          ) : (
            l.commit(sha)
          )}
          {r.armature && <span className="sr-only"> {l.inArmature(r.armature.issueKey)}</span>}
        </span>
        {r.runUrl && (
          <a className="underline" href={r.runUrl} target="_blank" rel="noreferrer">
            {l.run}
          </a>
        )}
        <span>{l.when(at.format(new Date(r.at)))}</span>
      </p>
      {r.outcome !== "passed" && r.message && <p className="whitespace-pre-wrap text-xs text-ink-muted">{r.message}</p>}
    </li>
  );
}

import { useCodeLinks, type CodeLink } from "@/api/code";
import type { Requirement } from "@/api/requirements";
import { Card, ErrorBanner, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { TEST_COMMIT_SHOWN } from "@/config";
import { t } from "@/i18n";

/** The files and lines that implement or verify a requirement, the counting branch first, each linking to the line at
 * its commit and saying whether its tag names an older revision (#201). */
export function CodeLinks({ requirement }: { requirement: Requirement }) {
  const section = requirement.kind === "section";
  const { data, error, refetch } = useCodeLinks(requirement.key, !section);
  if (section) return null;
  const l = t.evidence;
  const branches = data ? [...new Set(data.links.map((link) => link.branch))] : [];
  return (
    <Card className="space-y-3 p-4" data-code-links>
      <section aria-labelledby="code-links-title" className="space-y-3">
        <SectionTitle id="code-links-title">{l.code.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && data.links.length === 0 && <p className="text-sm text-ink-subtle">{l.code.none}</p>}
        {branches.map((branch) => (
          <div key={branch} className="space-y-1" data-code-branch={branch}>
            <h3 className="text-sm font-medium text-ink">{branch === data?.countingBranch ? l.results.counting(branch) : l.results.other(branch)}</h3>
            <ul className="space-y-1.5">
              {data?.links
                .filter((link) => link.branch === branch)
                .map((link) => (
                  <CodeRow key={`${link.repository} ${link.path} ${link.line} ${link.word}`} link={link} />
                ))}
            </ul>
          </div>
        ))}
      </section>
    </Card>
  );
}

function CodeRow({ link }: { link: CodeLink }) {
  const l = t.evidence.code;
  const place = l.at(link.path, link.line);
  return (
    <li className="flex flex-wrap items-center gap-2 text-sm" data-code-link={place} data-outdated={link.outdated}>
      <Tag>{l.relation[link.relation] ?? link.relation}</Tag>
      {link.url ? (
        <a className="font-mono text-xs text-ink underline" href={link.url} target="_blank" rel="noreferrer">
          {place}
        </a>
      ) : (
        <span className="font-mono text-xs text-ink">{place}</span>
      )}
      <span className="text-xs text-ink-muted">
        {link.repository} {l.commit(link.commit.slice(0, TEST_COMMIT_SHOWN))}
      </span>
      {link.outdated && (
        <Tag title={l.outdatedHint} data-code-outdated>
          {l.outdated(link.revision)}
        </Tag>
      )}
    </li>
  );
}

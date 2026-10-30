import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useCodeTags, type CodeTagState } from "@/api/code";
import type { Project } from "@/api/projects";
import { useEvidenceSettings } from "@/api/testResults";
import { Button, Card, EmptyState, ErrorBanner, PageHeader, Select, Skeleton, Table, Td, Th, Page } from "@/components/ui";
import { CODE_TAGS_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

/** A project's code tags on its counting branch that name an older revision of their requirement, or one in the trash
 * or none (#201), so their code is checked and their tags set right. */
export function CodeTagsPage({ project }: { project: Project }) {
  const [state, setState] = useState<CodeTagState | "">("");
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useCodeTags(project.key, state, offset);
  const { data: settings } = useEvidenceSettings(project.key);
  const l = t.evidence.codeTags;
  return (
    <Page width="content" className="space-y-4" data-code-tags-page>
      <PageHeader title={l.title} />
      <p className="text-sm text-ink-muted">{l.intro(settings?.countingBranch ?? "main")}</p>
      <div className="max-w-60">
        <Select
          label={l.state}
          value={state}
          controlSize="sm"
          onChange={(event) => {
            setState(event.target.value as CodeTagState | "");
            setOffset(0);
          }}
        >
          {(["", "outdated", "orphaned"] as const).map((s) => (
            <option key={s} value={s}>
              {l.states[s]}
            </option>
          ))}
        </Select>
      </div>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && data.tags.length === 0 && <EmptyState title={l.none} />}
      {data && data.tags.length > 0 && (
        <Card className="overflow-x-auto">
          <Table aria-label={l.title}>
            <thead>
              <tr>
                <Th>{l.columnPlace}</Th>
                <Th>{l.columnTag}</Th>
                <Th>{l.columnRequirement}</Th>
                <Th>{l.columnState}</Th>
              </tr>
            </thead>
            <tbody>
              {data.tags.map((tag) => {
                const place = t.evidence.code.at(tag.path, tag.line);
                return (
                  <tr key={`${tag.repository} ${place} ${tag.word} ${tag.key}`} data-code-tag={place} data-state={tag.state}>
                    <Td>
                      {tag.url ? (
                        <a className="font-mono text-xs underline" href={tag.url} target="_blank" rel="noreferrer">
                          {place}
                        </a>
                      ) : (
                        <span className="font-mono text-xs">{place}</span>
                      )}
                      <span className="block text-xs text-ink-muted">{tag.repository}</span>
                    </Td>
                    <Td className="font-mono text-xs">
                      [{tag.word} {tag.key}]
                    </Td>
                    <Td>
                      {tag.requirement && !tag.requirement.trashed ? (
                        <Link
                          to="/projects/$projectKey/r/$reqKey"
                          params={{ projectKey: project.key, reqKey: tag.requirement.key }}
                          className="text-ink hover:text-accent hover:underline"
                        >
                          <span className="font-mono text-xs text-ink-muted">{tag.requirement.key}</span> {tag.requirement.title}
                        </Link>
                      ) : (
                        <span className="text-ink-muted">{l.problem[tag.problem ?? ""] ?? tag.key}</span>
                      )}
                    </Td>
                    <Td>{tag.state === "outdated" ? l.outdatedState(tag.revision, tag.requirement?.revision ?? 0) : l.orphanedState}</Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </Card>
      )}
      {data && data.total > CODE_TAGS_PAGE_SIZE && (
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - CODE_TAGS_PAGE_SIZE))}>
            {l.previous}
          </Button>
          <Button variant="secondary" size="sm" disabled={offset + CODE_TAGS_PAGE_SIZE >= data.total} onClick={() => setOffset(offset + CODE_TAGS_PAGE_SIZE)}>
            {l.next}
          </Button>
        </div>
      )}
    </Page>
  );
}

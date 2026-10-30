import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useAttributes } from "@/api/attributes";
import { useActivity, useComparison, useRestoreRevision, useRevision, useRevisions, type Comparison } from "@/api/history";
import type { Project } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { Button, Card, Checkbox, Dialog, EmptyState, ErrorBanner, Field, PageHeader, SectionTitle, Skeleton, Table, Tabs, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HISTORY_PAGE_SIZE, REVISION_COMMENT_MAX_LENGTH } from "@/config";
import { DiffTitle, DocDiffView, DocView, type DiffBlockView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { AttributeValues, type Values } from "@/features/types/AttributeFields";
import { Through } from "@/components/Through";
import { t } from "@/i18n";
import { TestCaseDiff } from "@/features/testcases/TestCaseDiff";
import { RatingDiff } from "@/features/risks/RatingDiff";
import { localDateFormat } from "@/lib/format";

/** Where in a requirement's history the reader is: the list, its activity, one revision, or two compared. */
export interface HistorySearch {
  revision?: number;
  from?: number;
  to?: number;
  offset?: number;
  tab?: "activity";
}

const writtenAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });
const when = (iso: string) => writtenAt.format(new Date(iso));
const sideLabel = (n: number) => (n === 0 ? t.history.emptyRequirement : t.history.revision(n));

interface ScreenProps {
  project: Project;
  requirement: Requirement;
  onSearch: (next: HistorySearch) => void;
}

/** A requirement's revisions: listed, read one at a time, compared and restored; and its activity. */
export function RequirementHistory({ search, ...props }: ScreenProps & { search: HistorySearch }) {
  if (search.revision !== undefined) return <RevisionScreen key={search.revision} number={search.revision} {...props} />;
  if (search.from !== undefined || search.to !== undefined) return <CompareScreen from={search.from} to={search.to} {...props} />;
  return <HistoryList offset={search.offset ?? 0} tab={search.tab} {...props} />;
}

function Header({ requirement, title }: { requirement: Requirement; title: string }) {
  return (
    <PageHeader
      crumb={
        <Link
          to="/p/$projectKey/r/$reqKey"
          params={{ projectKey: requirement.projectKey, reqKey: requirement.key }}
          className="hover:text-accent hover:underline"
        >
          {requirement.key} {requirement.title}
        </Link>
      }
      title={title}
    />
  );
}

/** Restores a revision after asking, with a comment, and says how it went. */
function RestoreButton({ requirement, number }: { requirement: Requirement; number: number }) {
  const restore = useRestoreRevision(requirement.key);
  const [asking, setAsking] = useState(false);
  const [comment, setComment] = useState("");
  const [notice, setNotice] = useState("");
  const conflict = restore.error instanceof ApiError && restore.error.code === "revision_conflict";
  if (!requirement.can.edit || number === requirement.revision) return null;
  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setAsking(true)} data-action="restore-revision">
        {t.history.restore}
      </Button>
      <span role="status" className="text-sm text-ink-muted empty:hidden" data-history-notice>
        {notice}
      </span>
      {asking && (
        <Dialog open title={t.history.restoreTitle(number)} onClose={() => setAsking(false)} attrs={{ "data-restore-dialog": "" }}>
          <div className="space-y-3">
            <p className="text-sm text-ink">{t.history.restoreBody(number)}</p>
            {restore.error && <ErrorBanner>{conflict ? t.history.restoreConflict : restore.error.message}</ErrorBanner>}
            <Field
              label={t.history.restoreComment}
              value={comment}
              maxLength={REVISION_COMMENT_MAX_LENGTH}
              onChange={(event) => setComment(event.target.value)}
            />
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setAsking(false)}>
                {t.kit.cancel}
              </Button>
              <Button
                loading={restore.isPending}
                onClick={() =>
                  restore.mutate(
                    { number, baseRevision: requirement.revision, comment: comment || undefined },
                    {
                      onSuccess: ({ revision }) => {
                        setAsking(false);
                        setNotice(t.history.restored(number, revision.number));
                      },
                    },
                  )
                }
                data-action="confirm-restore"
              >
                {t.history.restore}
              </Button>
            </div>
          </div>
        </Dialog>
      )}
    </>
  );
}

function HistoryList({ requirement, offset, tab, onSearch }: ScreenProps & { offset: number; tab?: "activity" }) {
  const { data, error, isLoading, refetch } = useRevisions(requirement.key, offset);
  const [selected, setSelected] = useState<number[]>([]);
  // A third pick lets go of the oldest pick, so two stay selected.
  const toggle = (n: number, on: boolean) =>
    setSelected((before) => (on ? [...before.filter((each) => each !== n), n].slice(-2) : before.filter((each) => each !== n)));
  const total = data?.total ?? 0;
  return (
    <div className="mx-auto max-w-4xl space-y-4" data-requirement-history={requirement.key}>
      <Header requirement={requirement} title={t.history.title(requirement.key)} />
      <p className="text-sm text-ink-muted">{t.history.intro}</p>
      <Tabs<"revisions" | "activity">
        label={t.history.title(requirement.key)}
        value={tab ?? "revisions"}
        onChange={(next) => onSearch(next === "activity" ? { tab: "activity" } : {})}
        tabs={[
          { value: "revisions", label: t.history.tabRevisions, attrs: { "data-tab": "revisions" } },
          { value: "activity", label: t.history.tabActivity, attrs: { "data-tab": "activity" } },
        ]}
      />
      {tab === "activity" ? (
        <ActivityList requirement={requirement} />
      ) : (
        <>
          {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
          {isLoading && <Skeleton />}
          {data && (
            <>
              <div className="flex flex-wrap items-center gap-3">
                <Button
                  variant="secondary"
                  disabled={selected.length !== 2}
                  onClick={() => onSearch({ from: Math.min(...selected), to: Math.max(...selected) })}
                  data-action="compare-selected"
                >
                  {t.history.compareSelected}
                </Button>
                <p className="text-sm text-ink-muted">{t.history.compareHint}</p>
              </div>
              <Table aria-label={t.history.tableLabel}>
                <thead>
                  <tr>
                    <Th className="w-8">
                      <span className="sr-only">{t.history.compareSelected}</span>
                    </Th>
                    <Th>{t.history.columnRevision}</Th>
                    <Th>{t.history.columnWritten}</Th>
                    <Th>{t.history.columnComment}</Th>
                    <Th>
                      <span className="sr-only">{t.history.columnActions}</span>
                    </Th>
                  </tr>
                </thead>
                <tbody>
                  {data.revisions.map((entry) => (
                    <tr key={entry.number} data-revision-row={entry.number}>
                      <Td>
                        <Checkbox
                          label={<span className="sr-only">{t.history.select(t.history.revision(entry.number))}</span>}
                          checked={selected.includes(entry.number)}
                          onChange={(event) => toggle(entry.number, event.target.checked)}
                        />
                      </Td>
                      <Td>
                        <Button variant="ghost" size="sm" onClick={() => onSearch({ revision: entry.number })} data-action="view-revision">
                          {t.history.revision(entry.number)}
                        </Button>
                        {entry.number === requirement.revision && <Tag className="ml-2">{t.history.latest}</Tag>}
                        {entry.title !== requirement.title && <span className="block text-xs text-ink-muted">{entry.title}</span>}
                      </Td>
                      <Td className="text-ink-muted">
                        {t.history.written(entry.authorName, when(entry.createdAt))}
                        <Through via={entry.via} className="block" />
                      </Td>
                      <Td>
                        {entry.comment ? <span className="text-ink">{entry.comment}</span> : <span className="text-ink-subtle">{t.history.noComment}</span>}
                        {entry.restoredFrom != null && <span className="block text-xs text-ink-muted">{t.history.restoredFrom(entry.restoredFrom)}</span>}
                      </Td>
                      <Td>
                        <div className="flex items-center justify-end gap-2">
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={t.history.compareWithPreviousOf(entry.number)}
                            onClick={() => onSearch({ from: entry.number - 1, to: entry.number })}
                            data-action="compare-previous"
                          >
                            {t.history.compareWithPrevious}
                          </Button>
                          <RestoreButton requirement={requirement} number={entry.number} />
                        </div>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              {total > HISTORY_PAGE_SIZE && (
                <div className="flex justify-end gap-2">
                  <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => onSearch({ offset: Math.max(0, offset - HISTORY_PAGE_SIZE) })}>
                    {t.history.previous}
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={offset + HISTORY_PAGE_SIZE >= total}
                    onClick={() => onSearch({ offset: offset + HISTORY_PAGE_SIZE })}
                  >
                    {t.history.next}
                  </Button>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

function RevisionScreen({ project, requirement, number, onSearch }: ScreenProps & { number: number }) {
  const { data, error } = useRevision(requirement.key, number);
  const { data: attributes } = useAttributes(project.key);
  return (
    <div className="mx-auto max-w-3xl space-y-4" data-revision={number}>
      <Header requirement={requirement} title={t.history.revision(number)} />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.ChevronDown className="rotate-90" />} onClick={() => onSearch({})}>
          {t.history.backToHistory}
        </Button>
        {number > 1 && (
          <Button variant="secondary" size="sm" onClick={() => onSearch({ from: number - 1, to: number })} data-action="compare-previous">
            {t.history.compareWithPrevious}
          </Button>
        )}
        <RestoreButton requirement={requirement} number={number} />
      </div>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {!data ? (
        !error && <Skeleton />
      ) : (
        <Card className="space-y-4 p-4">
          <p className="text-sm text-ink-muted">
            {t.history.written(data.authorName, when(data.createdAt))} <Through via={data.via} />
          </p>
          <h2 className="text-lg font-semibold text-ink">{data.title}</h2>
          <DocView doc={data.statement as Doc} />
          {Boolean(data.rationale) && (
            <section className="space-y-1">
              <SectionTitle>{t.history.rationale}</SectionTitle>
              <DocView doc={data.rationale as Doc} />
            </section>
          )}
          {attributes && <AttributeValues attributes={attributes} values={data.attributes as Values} />}
        </Card>
      )}
    </div>
  );
}

function CompareScreen({ requirement, from, to, onSearch }: ScreenProps & { from?: number; to?: number }) {
  const { data, error } = useComparison(requirement.key, from, to);
  return (
    <div className="mx-auto max-w-4xl space-y-4" data-comparison="">
      <Header requirement={requirement} title={data ? t.history.compareTitle(sideLabel(data.from.number), sideLabel(data.to.number)) : t.history.link} />
      <Button variant="secondary" size="sm" icon={<Icon.ChevronDown className="rotate-90" />} onClick={() => onSearch({})}>
        {t.history.backToHistory}
      </Button>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {!data ? !error && <Skeleton /> : <ComparisonView comparison={data} />}
    </div>
  );
}

/** Two revisions side by side: their titles, and what changed in the statement, the rationale and the attributes. */
export function ComparisonView({ comparison }: { comparison: Comparison }) {
  const { from, to } = comparison;
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2">
        {[from, to].map((side) => (
          <Card key={side.number} className="space-y-1 p-3 text-sm" data-compare-side={side.number}>
            <p className="font-medium text-ink">{sideLabel(side.number)}</p>
            {side.number > 0 && <p className="text-ink-muted">{t.history.written(side.authorName, when(side.createdAt))}</p>}
            <p className="text-ink">{side.title}</p>
            {side.type && (
              <p className="text-ink-muted">
                {t.history.type}: {side.type.name}
              </p>
            )}
          </Card>
        ))}
      </div>
      {comparison.title.some((b) => b.change !== "equal") && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.history.titleChanged}</SectionTitle>
          <p className="text-ink" data-diff-of="title">
            <DiffTitle blocks={comparison.title as DiffBlockView[]} />
          </p>
        </Card>
      )}
      <Card className="space-y-2 p-4">
        <SectionTitle>{t.history.statement}</SectionTitle>
        <DocDiffView blocks={comparison.statement as DiffBlockView[]} data-diff-of="statement" />
      </Card>
      {comparison.rationale.length > 0 && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.history.rationale}</SectionTitle>
          <DocDiffView blocks={comparison.rationale as DiffBlockView[]} data-diff-of="rationale" />
        </Card>
      )}
      <TestCaseDiff comparison={comparison} />
      <RatingDiff comparison={comparison} />
      <Card className="space-y-2 p-4">
        <SectionTitle>{t.history.attributes}</SectionTitle>
        <AttributeChanges attributes={comparison.attributes} />
      </Card>
    </div>
  );
}

function ActivityList({ requirement }: { requirement: Requirement }) {
  const activity = useActivity(requirement.key);
  const entries = activity.data?.pages.flatMap((page) => page.entries) ?? [];
  if (activity.error) return <ErrorBanner onRetry={() => void activity.refetch()}>{activity.error.message}</ErrorBanner>;
  if (activity.isLoading) return <Skeleton />;
  if (entries.length === 0) return <EmptyState title={t.history.activityEmpty} />;
  return (
    <div className="space-y-3">
      <ol className="space-y-2" aria-label={t.history.activityLabel} data-activity>
        {entries.map((e) => (
          <li key={e.id} className="rounded-control border border-border px-3 py-2 text-sm" data-activity-entry={e.kind}>
            <span className="font-medium text-ink">{e.actorName || "-"}</span> {e.via && <Through via={e.via} />}{" "}
            <span className="text-ink-muted">
              {e.revision
                ? t.history.activityRevision(e.revision.number)
                : e.change
                  ? t.history.activityChange(t.history.fields[e.change.field], e.change.from, e.change.to)
                  : ""}
            </span>
            <span className="ml-2 text-xs text-ink-subtle">{when(e.at)}</span>
            {e.revision?.comment && <span className="block text-ink">{e.revision.comment}</span>}
            {e.change?.reason && <span className="block text-xs text-ink-muted">{t.history.because(e.change.reason)}</span>}
          </li>
        ))}
      </ol>
      {activity.hasNextPage && (
        <Button variant="secondary" size="sm" loading={activity.isFetchingNextPage} onClick={() => void activity.fetchNextPage()}>
          {t.history.more}
        </Button>
      )}
    </div>
  );
}

/** The attributes that changed between two revisions, before and after, or a line that none did. */
export function AttributeChanges({ attributes }: { attributes: Comparison["attributes"] }) {
  if (attributes.every((a) => a.change === "equal")) return <p className="text-sm text-ink-subtle">{t.history.noAttributes}</p>;
  return (
    <Table dense aria-label={t.history.attributes}>
      <thead>
        <tr>
          <Th>{t.history.attributeColumn}</Th>
          <Th>{t.history.before}</Th>
          <Th>{t.history.after}</Th>
        </tr>
      </thead>
      <tbody>
        {attributes
          .filter((a) => a.change !== "equal")
          .map((a) => (
            <tr key={a.attributeId} data-attribute-diff={a.name} data-change={a.change}>
              <Td className="font-medium text-ink">{a.name}</Td>
              <Td>
                <del className="text-ink-muted">{a.fromText}</del>
              </Td>
              <Td>
                <ins className="text-ink no-underline">{a.toText}</ins>
              </Td>
            </tr>
          ))}
      </tbody>
    </Table>
  );
}

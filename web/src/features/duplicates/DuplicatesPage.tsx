import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useTypes } from "@/api/attributes";
import { ApiError } from "@/api/client";
import {
  isStale,
  useDismissPair,
  useDuplicatePairs,
  useLinkPair,
  useMergePair,
  useReadPairsAgain,
  type DuplicateMerge,
  type DuplicatePair,
  type PairFilter,
} from "@/api/duplicates";
import { linkWays, useProjectLinkTypes } from "@/api/links";
import type { Project } from "@/api/projects";
import { useRequirement, type Requirement } from "@/api/requirements";
import { Button, Card, Dialog, EmptyState, ErrorBanner, Field, PageHeader, Segmented, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DUPLICATE_REASON_MAX_LENGTH, DUPLICATE_REASON_ROWS } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { textOf, type DocNode } from "@/features/editor/schema";
import { wordDiff } from "@/features/glossary/diff";
import { QueryInput, queryFailure } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { percent } from "./SimilarHint";

type Deciding = { kind: "dismiss" | "link" | "merge"; pair: DuplicatePair };

const NO_FILTER: PairFilter = { typeId: "", section: "", query: "" };

/** A project's likely duplicates (#208), most alike first, each pair side by side with what differs between them, to
 * dismiss, link or merge. */
export function DuplicatesPage({ project }: { project: Project }) {
  const [filter, setFilter] = useState<PairFilter>(NO_FILTER);
  const pairs = useDuplicatePairs(project.key, filter);
  const [deciding, setDeciding] = useState<Deciding | null>(null);
  const [notice, setNotice] = useState("");
  const l = t.duplicates.page;
  const may = project.can.edit && !project.archivedAt;
  const shown = pairs.data?.pages.flatMap((page) => page.pairs) ?? [];
  const threshold = pairs.data?.pages[0]?.threshold;
  const fields = pairs.error instanceof ApiError ? pairs.error.fields : {};
  const general = pairs.error && !queryFailure(pairs.error) && Object.keys(fields).length === 0;
  function decided(message: string) {
    setNotice(message);
    setDeciding(null);
  }
  return (
    <div className="mx-auto max-w-6xl space-y-4" data-duplicates-page={project.key}>
      <PageHeader title={l.title} meta={l.intro} />
      {threshold !== undefined && (
        <p className="text-sm text-ink-muted" data-duplicates-threshold={threshold}>
          {l.threshold(percent(threshold))}
        </p>
      )}
      <Filters
        project={project}
        filter={filter}
        sectionError={fields.section}
        typeError={fields.type}
        onFilter={(next) => {
          setNotice("");
          setFilter((was) => ({ ...was, ...next }));
        }}
      />
      <QueryInput
        projectKey={project.key}
        value={filter.query}
        name="duplicates-query"
        label={l.queryLabel}
        error={pairs.error}
        onSubmit={(query) => setFilter((was) => ({ ...was, query }))}
      />
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-duplicates-notice>
        {notice}
      </p>
      {general && <ErrorBanner onRetry={() => void pairs.refetch()}>{pairs.error.message}</ErrorBanner>}
      {pairs.isPending && <Skeleton />}
      {pairs.data && shown.length === 0 && <EmptyState icon={<Icon.Check />} title={l.none} description={l.noneHint} />}
      {shown.length > 0 && (
        <>
          <p className="text-sm text-ink-muted" data-pair-count={shown.length}>
            {l.count(shown.length)}
          </p>
          <ul className="space-y-4" aria-label={l.title}>
            {shown.map((pair) => (
              <PairCard
                key={`${pair.a.id} ${pair.b.id}`}
                pair={pair}
                may={may}
                onDecide={(kind) => {
                  setNotice("");
                  setDeciding({ kind, pair });
                }}
              />
            ))}
          </ul>
        </>
      )}
      {pairs.hasNextPage && (
        <Button variant="secondary" loading={pairs.isFetchingNextPage} onClick={() => void pairs.fetchNextPage()} data-action="more-pairs">
          {l.more}
        </Button>
      )}
      {deciding?.kind === "dismiss" && <DismissDialog project={project} pair={deciding.pair} onClose={() => setDeciding(null)} onDone={decided} />}
      {deciding?.kind === "link" && <LinkDialog project={project} pair={deciding.pair} onClose={() => setDeciding(null)} onDone={decided} />}
      {deciding?.kind === "merge" && (
        <MergeDialog project={project} pair={deciding.pair} onClose={() => setDeciding(null)} onDone={(message) => setNotice(message)} />
      )}
    </div>
  );
}

function Filters({
  project,
  filter,
  sectionError,
  typeError,
  onFilter,
}: {
  project: Project;
  filter: PairFilter;
  sectionError?: string;
  typeError?: string;
  onFilter: (next: Partial<PairFilter>) => void;
}) {
  const l = t.duplicates.page;
  const { data: types } = useTypes(project.key);
  const [typeId, setTypeId] = useState(filter.typeId);
  const [section, setSection] = useState(filter.section);
  function submit(event: FormEvent) {
    event.preventDefault();
    onFilter({ typeId, section: section.trim().toUpperCase() });
  }
  return (
    <form onSubmit={submit} className="flex flex-wrap items-end gap-3" aria-label={l.filters} data-duplicates-filters>
      <div className="min-w-48">
        <Select label={l.type} value={typeId} onChange={(event) => setTypeId(event.target.value)} error={typeError} data-duplicates-type>
          <option value="">{l.anyType}</option>
          {(types ?? []).map((type) => (
            <option key={type.id} value={type.id}>
              {type.name}
            </option>
          ))}
        </Select>
      </div>
      <div className="min-w-48">
        <Field
          label={l.section}
          hint={l.sectionHint}
          value={section}
          error={sectionError}
          onChange={(event) => setSection(event.target.value)}
          data-duplicates-section
        />
      </div>
      <Button type="submit" variant="secondary" data-action="filter-pairs">
        {l.filter}
      </Button>
    </form>
  );
}

function PairCard({ pair, may, onDecide }: { pair: DuplicatePair; may: boolean; onDecide: (kind: Deciding["kind"]) => void }) {
  const l = t.duplicates;
  const { data: a } = useRequirement(pair.a.key);
  const { data: b } = useRequirement(pair.b.key);
  const names = [pair.a.key, pair.b.key] as const;
  const headingId = `pair-${pair.a.id}-${pair.b.id}`;
  return (
    <li data-pair={`${pair.a.key} ${pair.b.key}`}>
      <Card className="space-y-3 p-4">
        <section aria-labelledby={headingId} className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <h2 id={headingId} className="text-sm font-semibold text-ink">
              {l.page.pair(...names)}
            </h2>
            <Tag className="tabular-nums" data-pair-score={pair.score}>
              {l.alike(percent(pair.score))}
            </Tag>
            {may && (
              <div className="ml-auto flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" aria-label={l.dismiss.label(...names)} onClick={() => onDecide("dismiss")} data-action="dismiss-pair">
                  {l.dismiss.action}
                </Button>
                <Button variant="secondary" size="sm" aria-label={l.link.label(...names)} onClick={() => onDecide("link")} data-action="link-pair">
                  {l.link.action}
                </Button>
                <Button variant="secondary" size="sm" aria-label={l.merge.label(...names)} onClick={() => onDecide("merge")} data-action="merge-pair">
                  {l.merge.action}
                </Button>
              </div>
            )}
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <Side side={pair.a} requirement={a} />
            <Side side={pair.b} requirement={b} />
          </div>
          {a && b && <Differences a={a} b={b} />}
        </section>
      </Card>
    </li>
  );
}

function Side({ side, requirement }: { side: DuplicatePair["a"]; requirement?: Requirement }) {
  const l = t.duplicates.page;
  return (
    <div className="min-w-0 space-y-2 rounded-control border border-border p-3" data-pair-side={side.key}>
      <h3 className="text-sm font-medium text-ink">
        <Link to="/p/$projectKey/r/$reqKey" params={{ projectKey: side.projectKey, reqKey: side.key }} className="hover:text-accent hover:underline">
          <span className="font-mono text-xs text-ink-muted">{side.key}</span> {side.title}
        </Link>
      </h3>
      <div className="flex flex-wrap gap-1.5">
        <Tag>{l.revision(side.revision)}</Tag>
        {side.status && <Tag>{side.status.name}</Tag>}
      </div>
      {!requirement ? (
        <Skeleton />
      ) : requirement.statement ? (
        <DocView doc={requirement.statement as DocNode} className="text-sm" data-pair-statement={side.key} />
      ) : (
        <p className="text-sm text-ink-subtle">{l.noStatement}</p>
      )}
    </div>
  );
}

/** The two statements compared word by word, from the first to the second; each change says which side holds it, so
 * colour is never the only sign. */
function Differences({ a, b }: { a: Requirement; b: Requirement }) {
  const l = t.duplicates.page;
  const runs = wordDiff(statementText(a), statementText(b));
  const same = runs.every((run) => run.change === "same");
  return (
    <div className="space-y-1 text-sm" data-pair-differences>
      <p className="text-xs font-medium text-ink-muted uppercase">{l.differences}</p>
      {same ? (
        <p className="text-ink-subtle">{l.same}</p>
      ) : (
        <p className="whitespace-pre-line text-ink">
          {runs.map((run, i) =>
            run.change === "removed" ? (
              // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
              <del key={i} className="bg-danger-subtle" data-only-in={a.key}>
                <span className="sr-only">{l.onlyIn(a.key)} </span>
                {run.text}
              </del>
            ) : run.change === "added" ? (
              // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
              <ins key={i} className="bg-success-subtle no-underline" data-only-in={b.key}>
                <span className="sr-only">{l.onlyIn(b.key)} </span>
                {run.text}
              </ins>
            ) : (
              // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
              <span key={i}>{run.text}</span>
            ),
          )}
        </p>
      )}
    </div>
  );
}

function statementText(requirement: Requirement): string {
  return requirement.statement ? textOf(requirement.statement as DocNode) : "";
}

/** What a decision's refusal says: a side revised since offers to read the pairs again, as the API asks. */
function Refusal({ error, onClose }: { error: Error | null; onClose: () => void }) {
  const readAgain = useReadPairsAgain();
  if (!error) return null;
  const l = t.duplicates.page;
  if (isStale(error)) {
    return (
      <div className="space-y-2" data-stale-pair>
        <ErrorBanner>{l.stale}</ErrorBanner>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            void readAgain();
            onClose();
          }}
          data-action="read-pairs-again"
        >
          {l.readAgain}
        </Button>
      </div>
    );
  }
  const fields = error instanceof ApiError ? error.fields : {};
  return Object.keys(fields).length === 0 ? <ErrorBanner>{error.message}</ErrorBanner> : null;
}

function DismissDialog({ project, pair, onClose, onDone }: { project: Project; pair: DuplicatePair; onClose: () => void; onDone: (notice: string) => void }) {
  const l = t.duplicates.dismiss;
  const [reason, setReason] = useState("");
  const dismiss = useDismissPair(project.key);
  const fields = dismiss.error instanceof ApiError ? dismiss.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    dismiss.mutate(
      { a: { key: pair.a.key, revision: pair.a.revision }, b: { key: pair.b.key, revision: pair.b.revision }, reason: reason.trim() },
      { onSuccess: () => onDone(l.done(pair.a.key, pair.b.key)) },
    );
  }
  return (
    <Dialog title={l.title} onClose={onClose} data-dismiss-pair="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <p className="text-sm text-ink">{l.intro(pair.a.key, pair.b.key)}</p>
        <Refusal error={dismiss.error} onClose={onClose} />
        <Field
          label={l.reason}
          hint={l.reasonHint}
          value={reason}
          rows={DUPLICATE_REASON_ROWS}
          maxLength={DUPLICATE_REASON_MAX_LENGTH}
          error={fields.reason}
          onChange={(event) => setReason(event.target.value)}
          autoFocus
        />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" loading={dismiss.isPending} disabled={!reason.trim()} data-action="confirm-dismiss-pair">
            {l.send}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function LinkDialog({ project, pair, onClose, onDone }: { project: Project; pair: DuplicatePair; onClose: () => void; onDone: (notice: string) => void }) {
  const l = t.duplicates.link;
  const { data: types } = useProjectLinkTypes(project.key);
  const ways = linkWays(types ?? [], (phrase) => l.reads(pair.a.key, phrase, pair.b.key));
  const [chosen, setChosen] = useState("");
  const way = chosen || ways[0]?.value || "";
  const link = useLinkPair(project.key);
  const fields = link.error instanceof ApiError ? link.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    const [typeId, direction] = way.split(":");
    const type = types?.find((each) => each.id === typeId);
    if (!type) return;
    const a = { key: pair.a.key, revision: pair.a.revision };
    const b = { key: pair.b.key, revision: pair.b.revision };
    const [source, target] = direction === "incoming" ? [b, a] : [a, b];
    link.mutate({ source, target, linkTypeKey: type.key }, { onSuccess: () => onDone(l.done(pair.a.key, pair.b.key)) });
  }
  return (
    <Dialog title={l.title} onClose={onClose} data-link-pair="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <p className="text-sm text-ink">{l.intro}</p>
        <Refusal error={link.error} onClose={onClose} />
        {!types && <Skeleton />}
        {types && ways.length === 0 && <p className="text-sm text-ink-muted">{l.none}</p>}
        {ways.length > 0 && (
          <Select label={l.way} value={way} onChange={(event) => setChosen(event.target.value)} error={fields.linkTypeKey} data-link-way>
            {ways.map((each) => (
              <option key={each.value} value={each.value}>
                {each.label}
              </option>
            ))}
          </Select>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" loading={link.isPending} disabled={!way} data-action="confirm-link-pair">
            {l.send}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function MergeDialog({ project, pair, onClose, onDone }: { project: Project; pair: DuplicatePair; onClose: () => void; onDone: (notice: string) => void }) {
  const l = t.duplicates.merge;
  const [keepKey, setKeepKey] = useState(pair.a.key);
  const [comment, setComment] = useState("");
  const [result, setResult] = useState<DuplicateMerge | null>(null);
  const merge = useMergePair(project.key);
  const fields = merge.error instanceof ApiError ? merge.error.fields : {};
  const [keep, gone] = keepKey === pair.a.key ? [pair.a, pair.b] : [pair.b, pair.a];
  function submit(event: FormEvent) {
    event.preventDefault();
    merge.mutate(
      {
        keep: { key: keep.key, revision: keep.revision },
        trash: { key: gone.key, revision: gone.revision },
        ...(comment.trim() ? { comment: comment.trim() } : {}),
      },
      {
        onSuccess: (merged) => {
          setResult(merged);
          onDone(l.done(keep.key, gone.key));
        },
      },
    );
  }
  if (result) return <MergeResult result={result} kept={keep.key} trashed={gone.key} onClose={onClose} />;
  return (
    <Dialog title={l.title} onClose={onClose} data-merge-pair="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <p className="text-sm text-ink">{l.intro}</p>
        <Refusal error={merge.error} onClose={onClose} />
        <Segmented
          label={l.keep}
          value={keepKey}
          onChange={setKeepKey}
          options={[pair.a, pair.b].map((side) => ({ value: side.key, label: side.key, attrs: { "data-keep": side.key } }))}
        />
        <p className="text-sm text-ink-muted" data-merge-trashes={gone.key}>
          {l.trashed(gone.key)}
        </p>
        <Field
          label={l.comment}
          hint={l.commentHint}
          value={comment}
          rows={DUPLICATE_REASON_ROWS}
          maxLength={DUPLICATE_REASON_MAX_LENGTH}
          error={fields.comment}
          onChange={(event) => setComment(event.target.value)}
        />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" loading={merge.isPending} data-action="confirm-merge-pair">
            {l.send}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** What became of the trashed side's links: each made again on the side kept, or skipped with why. */
function MergeResult({ result, kept, trashed, onClose }: { result: DuplicateMerge; kept: string; trashed: string; onClose: () => void }) {
  const l = t.duplicates.merge;
  return (
    <Dialog title={l.title} onClose={onClose} data-merge-result="">
      <div className="space-y-4">
        <p className="text-sm text-ink">{l.done(kept, trashed)}</p>
        <section aria-labelledby="merge-links-title" className="space-y-2">
          <h3 id="merge-links-title" className="text-sm font-medium text-ink">
            {l.links}
          </h3>
          {result.links.length === 0 ? (
            <p className="text-sm text-ink-subtle">{l.noLinks}</p>
          ) : (
            <ul className="space-y-1.5">
              {result.links.map((link) => (
                <li
                  key={`${link.linkTypeKey} ${link.otherKey} ${link.outgoing}`}
                  className="text-sm"
                  data-moved-link={link.otherKey}
                  data-skipped={link.skipped ? "" : undefined}
                >
                  <span className="font-medium text-ink">{(link.outgoing ? l.outgoing : l.incoming)(link.linkTypeKey, link.otherKey)}</span>{" "}
                  <span className="text-ink-muted">{link.skipped ? `${l.skipped} ${l.reasons[link.skipped]}` : l.moved}</span>
                </li>
              ))}
            </ul>
          )}
        </section>
        <div className="flex justify-end">
          <Button onClick={onClose} data-action="close-merge" autoFocus>
            {l.close}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

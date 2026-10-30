import { useId, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { useProjects } from "@/api/projects";
import type { TermHit } from "@/api/glossary";
import { useSearch, type Hit, type SearchRequest } from "@/api/search";
import { STATUS_CATEGORIES, type StatusCategory } from "@/api/workflows";
import { Button, EmptyState, ErrorBanner, Field, Input, PageHeader, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SEARCH_PAGE_SIZE, SEARCH_QUERY_MAX_LENGTH } from "@/config";
import { LabelCombobox } from "@/features/labels/LabelCombobox";
import { LabelLink } from "@/features/labels/RequirementLabels";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { Highlight } from "./Highlight";

/** The search as the address holds it: lists joined by commas, and the page of results counted from 1. */
export interface SearchAddress {
  q?: string;
  project?: string;
  category?: string;
  label?: string;
  owner?: string;
  updatedAfter?: string;
  updatedBefore?: string;
  sort?: "updated";
  page?: number;
}

const FILTERS = ["project", "category", "label", "owner", "updatedAfter", "updatedBefore"] as const;

const list = (value: string | undefined): string[] =>
  (value ?? "")
    .split(",")
    .map((each) => each.trim())
    .filter(Boolean);
const joined = (values: string[]): string | undefined => (values.length > 0 ? values.join(",") : undefined);

function request(address: SearchAddress): SearchRequest {
  return {
    q: address.q ?? "",
    project: list(address.project),
    category: list(address.category).filter((each): each is StatusCategory => (STATUS_CATEGORIES as string[]).includes(each)),
    label: list(address.label),
    owner: list(address.owner),
    updatedAfter: address.updatedAfter,
    updatedBefore: address.updatedBefore,
    sort: address.sort,
    offset: ((address.page ?? 1) - 1) * SEARCH_PAGE_SIZE,
  };
}

// Only a refused query is the reader's to fix, and the server says how; anything else is ours.
function problem(error: Error): string {
  return error instanceof ApiError && error.status === 422 ? error.message : t.search.failed;
}

const changedOn = localDateFormat({ dateStyle: "medium" });

/** The full search: words, the filters beside them, and the hits a page at a time. */
export function SearchScreen({ address, onChange }: { address: SearchAddress; onChange: (next: SearchAddress, replace: boolean) => void }) {
  const { data, error, isLoading, isPlaceholderData, refetch } = useSearch(request(address));
  const [draft, setDraft] = useState(address.q ?? "");
  const [seenQ, setSeenQ] = useState(address.q);
  // The address can change under the form, from the palette or Back, and the box follows it.
  if (seenQ !== address.q) {
    setSeenQ(address.q);
    setDraft(address.q ?? "");
  }

  const without = (next: SearchAddress): SearchAddress =>
    Object.fromEntries(Object.entries(next).filter(([, value]) => value !== undefined && value !== "")) as SearchAddress;
  const filter = (patch: Partial<SearchAddress>) => onChange(without({ ...address, ...patch, page: undefined }), true);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onChange(without({ ...address, q: draft.trim() || undefined, page: undefined }), false);
  };

  const hits = data?.hits ?? [];
  const page = address.page ?? 1;
  const pages = data ? Math.max(1, Math.ceil(data.total / SEARCH_PAGE_SIZE)) : 1;
  const filtered = FILTERS.some((name) => address[name]);

  return (
    <>
      <PageHeader title={t.search.title} />
      {/* biome-ignore lint/a11y/useSemanticElements: jsdom, which the unit tests run in, does not know the search element yet */}
      <form role="search" onSubmit={submit} className="mb-6 flex max-w-2xl items-end gap-2" data-search-form>
        <div className="min-w-0 flex-1">
          <label htmlFor="search-query" className="mb-1 block text-sm font-medium text-ink-muted">
            {t.search.queryLabel}
          </label>
          <Input
            id="search-query"
            type="search"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t.search.queryPlaceholder}
            maxLength={SEARCH_QUERY_MAX_LENGTH}
            controlSize="lg"
            data-search-query
          />
        </div>
        <Button type="submit" size="lg" icon={<Icon.Search />} data-action="run-search">
          {t.search.submit}
        </Button>
      </form>

      <div className="grid gap-6 shell:grid-cols-[14rem_minmax(0,1fr)]">
        <Filters address={address} onFilter={filter} filtered={filtered} />
        <section aria-label={t.search.title} className="min-w-0" data-search-results>
          {!error && page === 1 && data?.terms && data.terms.length > 0 && <TermHits terms={data.terms} />}
          {error ? (
            <ErrorBanner onRetry={() => void refetch()}>
              <span data-search-error>{problem(error)}</span>
            </ErrorBanner>
          ) : isLoading || !data ? (
            <Skeleton rows={5} />
          ) : hits.length === 0 ? (
            <EmptyState icon={<Icon.Search />} title={t.search.emptyTitle} description={t.search.emptyBody} />
          ) : (
            <>
              <p className="mb-2 text-sm text-ink-subtle tabular-nums" role="status" data-search-count>
                {t.search.count(data.total, data.offset + 1, data.offset + hits.length)}
              </p>
              <ol className="divide-y divide-border" aria-busy={isPlaceholderData || undefined}>
                {hits.map((hit) => (
                  <HitRow key={hit.requirement.id} hit={hit} />
                ))}
              </ol>
              {pages > 1 && (
                <nav aria-label={t.search.paging} className="mt-4 flex items-center justify-between gap-2" data-search-paging>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={page <= 1}
                    onClick={() => onChange({ ...address, page: page - 1 > 1 ? page - 1 : undefined }, false)}
                    data-action="previous-results"
                  >
                    {t.search.previous}
                  </Button>
                  <span className="text-sm text-ink-subtle tabular-nums">{t.search.pageOf(page, pages)}</span>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={page >= pages}
                    onClick={() => onChange({ ...address, page: page + 1 }, false)}
                    data-action="next-results"
                  >
                    {t.search.next}
                  </Button>
                </nav>
              )}
            </>
          )}
        </section>
      </div>
    </>
  );
}

/** The glossary terms the words find (#210), above the requirements, each leading to its term. */
function TermHits({ terms }: { terms: TermHit[] }) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="mb-4 space-y-1 rounded-control border border-border p-3" data-search-terms>
      <h2 id={headingId} className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">
        {t.glossary.hits.title}
      </h2>
      <ul className="divide-y divide-border">
        {terms.map((term) => (
          <li key={term.id} className="py-1.5" data-search-term={term.name}>
            <div className="flex flex-wrap items-center gap-2">
              <Link
                to="/projects/$projectKey/glossary/$termId"
                params={{ projectKey: term.projectKey, termId: term.id }}
                className="font-medium text-accent hover:underline"
              >
                {term.name}
              </Link>
              <span className="font-mono text-xs text-ink-muted">{term.projectKey}</span>
              {term.status !== "approved" && <Tag>{t.glossary.statuses[term.status]}</Tag>}
            </div>
            {term.snippet && <p className="mt-0.5 text-sm text-ink-muted">{term.snippet}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}

function HitRow({ hit }: { hit: Hit }) {
  const ref = hit.requirement;
  const title = hit.title.length > 0 ? hit.title : [{ text: ref.title, match: false }];
  return (
    <li className="py-3" data-search-hit={ref.key}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs text-ink-muted">{ref.key}</span>
        <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: ref.projectKey, reqKey: ref.key }} className="font-medium text-accent hover:underline">
          <Highlight segments={title} />
        </Link>
        {ref.kind === "section" && <Tag>{t.requirements.kinds.section}</Tag>}
        {ref.status && <StatusBadge name={ref.status.name} category={ref.status.category} />}
        {hit.labels.map((label) => (
          <LabelLink key={label} name={label} projectKey={ref.projectKey} />
        ))}
      </div>
      <p className="mt-0.5 text-xs text-ink-subtle">
        {hit.projectName}
        {" / "}
        {t.search.changed(hit.updatedByName, changedOn.format(new Date(hit.updatedAt)))}
      </p>
      {hit.snippet.length > 0 && (
        <p className="mt-1 text-sm text-ink-muted" data-search-snippet>
          <Highlight segments={hit.snippet} />
        </p>
      )}
    </li>
  );
}

function Filters({ address, onFilter, filtered }: { address: SearchAddress; onFilter: (patch: Partial<SearchAddress>) => void; filtered: boolean }) {
  const headingId = useId();
  const { data: projects } = useProjects();
  const { data: me } = useMe();
  const labels = list(address.label);
  const project = list(address.project)[0] ?? "";
  const category = list(address.category)[0] ?? "";
  const owner = list(address.owner)[0] ?? "";

  return (
    <section aria-labelledby={headingId} className="space-y-4" data-search-filters>
      <div className="flex items-center justify-between gap-2">
        <SectionTitle id={headingId}>{t.search.filters}</SectionTitle>
        {filtered && (
          <Button variant="link" onClick={() => onFilter(Object.fromEntries(FILTERS.map((name) => [name, undefined])))} data-action="clear-filters">
            {t.search.clear}
          </Button>
        )}
      </div>
      <Select label={t.search.project} value={project} onChange={(e) => onFilter({ project: e.target.value || undefined })} data-filter="project">
        <option value="">{t.search.anyProject}</option>
        {(projects ?? []).map((each) => (
          <option key={each.key} value={each.key}>
            {each.name}
          </option>
        ))}
      </Select>
      <Select label={t.search.category} value={category} onChange={(e) => onFilter({ category: e.target.value || undefined })} data-filter="category">
        <option value="">{t.search.anyCategory}</option>
        {STATUS_CATEGORIES.map((each) => (
          <option key={each} value={each}>
            {t.workflows.categories[each]}
          </option>
        ))}
      </Select>
      <div data-filter="label">
        <LabelCombobox
          label={t.search.labels}
          exclude={labels}
          newOption={t.search.labelFilter}
          onPick={(name) => onFilter({ label: joined([...labels, name]) })}
          onRemoveLast={() => onFilter({ label: joined(labels.slice(0, -1)) })}
          hint={t.search.labelsHint}
        />
        {labels.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-1.5" aria-label={t.search.labels} data-label-filters>
            {labels.map((name) => (
              <li key={name}>
                <LabelLink name={name} onRemove={() => onFilter({ label: joined(labels.filter((each) => each !== name)) })} />
              </li>
            ))}
          </ul>
        )}
      </div>
      <Select label={t.search.owner} value={owner} onChange={(e) => onFilter({ owner: e.target.value || undefined })} data-filter="owner">
        <option value="">{t.search.anyone}</option>
        {me && <option value={me.user.id}>{t.search.you(me.user.name)}</option>}
      </Select>
      <Field
        label={t.search.after}
        type="date"
        value={address.updatedAfter ?? ""}
        max={address.updatedBefore}
        onChange={(e) => onFilter({ updatedAfter: e.target.value || undefined })}
        data-filter="updated-after"
      />
      <Field
        label={t.search.before}
        type="date"
        value={address.updatedBefore ?? ""}
        min={address.updatedAfter}
        onChange={(e) => onFilter({ updatedBefore: e.target.value || undefined })}
        data-filter="updated-before"
      />
      <Select
        label={t.search.sort}
        value={address.sort ?? "relevance"}
        onChange={(e) => onFilter({ sort: e.target.value === "updated" ? "updated" : undefined })}
        data-filter="sort"
      >
        <option value="relevance">{t.search.relevance}</option>
        <option value="updated">{t.search.updated}</option>
      </Select>
    </section>
  );
}

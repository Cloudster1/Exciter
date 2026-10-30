import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { TERM_STATUSES, useCreateTerm, useTerms, type TermStatus } from "@/api/glossary";
import type { Project } from "@/api/projects";
import {
  Button,
  Card,
  Checkbox,
  EmptyState,
  ErrorBanner,
  Field,
  PageHeader,
  SectionTitle,
  Select,
  Skeleton,
  Table,
  Tag,
  Td,
  Th,
  useToast,
  Page,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { GLOSSARY_PAGE_SIZE, TERM_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { TermForm } from "./TermForm";

/** A project's glossary (#210): its terms and its libraries', found by their words and narrowed by status, and a new
 * term for whoever edits the project. */
export function GlossaryPage({ project }: { project: Project }) {
  const l = t.glossary;
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<TermStatus | "">("");
  const [own, setOwn] = useState(false);
  const [offset, setOffset] = useState(0);
  const [adding, setAdding] = useState(false);
  const toast = useToast();
  const typed = useDebounced(q.trim(), TERM_SEARCH_DEBOUNCE_MS);
  const { data, error, refetch } = useTerms(project.key, { q: typed, status, own, offset });
  const create = useCreateTerm(project.key);
  const may = project.can.edit && !project.archivedAt;
  const narrowed = Boolean(typed || status || own);
  return (
    <Page width="content" className="space-y-4" data-glossary-page>
      <PageHeader
        title={l.title}
        actions={
          may && !adding ? (
            <Button
              icon={<Icon.Plus />}
              onClick={() => {
                create.reset();
                setAdding(true);
              }}
              data-action="new-term"
            >
              {l.newTerm}
            </Button>
          ) : undefined
        }
      />
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {adding && (
        <Card className="space-y-3 p-4">
          <section aria-labelledby="new-term-title" className="space-y-3">
            <SectionTitle id="new-term-title">{l.newTitle}</SectionTitle>
            <TermForm
              submitLabel={l.form.create}
              pending={create.isPending}
              error={create.error}
              onCancel={() => setAdding(false)}
              onSubmit={(values) =>
                create.mutate(
                  { name: values.name, definition: values.definition, synonyms: values.synonyms, avoid: values.avoid, status: values.status },
                  {
                    onSuccess: (term) => {
                      setAdding(false);
                      toast.success(l.created(term.name));
                    },
                  },
                )
              }
            />
          </section>
        </Card>
      )}
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-60 flex-1">
          <Field
            id="glossary-search"
            label={l.search}
            hint={l.searchHint}
            type="search"
            value={q}
            onChange={(event) => {
              setQ(event.target.value);
              setOffset(0);
            }}
          />
        </div>
        <div className="w-48">
          <Select
            id="glossary-status"
            label={l.status}
            value={status}
            onChange={(event) => {
              setStatus(event.target.value as TermStatus | "");
              setOffset(0);
            }}
          >
            <option value="">{l.anyStatus}</option>
            {TERM_STATUSES.map((each) => (
              <option key={each} value={each}>
                {l.statuses[each]}
              </option>
            ))}
          </Select>
        </div>
        <Checkbox
          label={l.ownOnly}
          checked={own}
          className="pb-2"
          onChange={(event) => {
            setOwn(event.target.checked);
            setOffset(0);
          }}
        />
      </div>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && data.terms.length === 0 && <EmptyState icon={<Icon.Book />} title={narrowed ? l.noneFound : l.none} />}
      {data && data.terms.length > 0 && (
        <Card className="overflow-x-auto">
          <Table aria-label={l.title}>
            <thead>
              <tr>
                <Th>{l.columnName}</Th>
                <Th>{l.columnStatus}</Th>
                <Th>{l.columnSynonyms}</Th>
                <Th>{l.columnProject}</Th>
                <Th>{l.columnUses}</Th>
              </tr>
            </thead>
            <tbody>
              {data.terms.map((term) => (
                <tr key={term.id} data-term-row={term.name}>
                  <Td>
                    <Link
                      to="/projects/$projectKey/glossary/$termId"
                      params={{ projectKey: term.projectKey, termId: term.id }}
                      className="font-medium text-ink hover:text-accent hover:underline"
                    >
                      {term.name}
                    </Link>
                  </Td>
                  <Td>
                    <Tag>{l.statuses[term.status]}</Tag>
                  </Td>
                  <Td className="text-sm text-ink-muted">{term.synonyms.join(", ")}</Td>
                  <Td className="text-sm">{term.projectKey === project.key ? l.ownProject : <span className="font-mono text-xs">{term.projectKey}</span>}</Td>
                  <Td className="text-sm tabular-nums">{l.uses(term.uses)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      )}
      {data && data.total > GLOSSARY_PAGE_SIZE && (
        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - GLOSSARY_PAGE_SIZE))}>
            {l.previous}
          </Button>
          <Button variant="secondary" size="sm" disabled={offset + GLOSSARY_PAGE_SIZE >= data.total} onClick={() => setOffset(offset + GLOSSARY_PAGE_SIZE)}>
            {l.next}
          </Button>
          <span className="text-sm text-ink-subtle">{l.count(data.total)}</span>
        </div>
      )}
    </Page>
  );
}

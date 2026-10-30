import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDeleteTerm, useTerm, useTermRevisions, useTermUses, useUpdateTerm, type Term, type TermRevision } from "@/api/glossary";
import type { Project } from "@/api/projects";
import { Button, Card, Dialog, ErrorBanner, PageHeader, SectionTitle, Skeleton, Table, Tag, Td, useToast, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HTTP_NOT_FOUND, TERM_USES_PAGE_SIZE } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { textOf, type Doc, type DocNode } from "@/features/editor/schema";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { wordDiff } from "./diff";
import { TermForm } from "./TermForm";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** One term (#210): its definition, synonyms and words to avoid, its revisions and what each changed, and the
 * requirements that use it; whoever edits its project changes it, and an administrator deletes it. */
export function TermPage({ project, termId }: { project: Project; termId: string }) {
  const { data: term, error, refetch } = useTerm(termId);
  if (error) {
    const gone = error instanceof ApiError && error.status === HTTP_NOT_FOUND;
    return <ErrorBanner onRetry={gone ? undefined : () => void refetch()}>{gone ? t.glossary.term.notFound : error.message}</ErrorBanner>;
  }
  if (!term) return <Skeleton />;
  return <TermView key={term.id} project={project} term={term} onReload={() => void refetch()} />;
}

function TermView({ project, term, onReload }: { project: Project; term: Term; onReload: () => void }) {
  const l = t.glossary;
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const toast = useToast();
  // Opened at this revision; a newer one refuses the save until the author loads it.
  const [base, setBase] = useState(term.revision);
  const update = useUpdateTerm(term.id);
  const own = term.projectKey === project.key;
  const may = own && project.can.edit && !project.archivedAt;
  const mayDelete = own && project.can.administer && !project.archivedAt;
  const stale = update.error instanceof ApiError && update.error.code === "stale_revision";
  return (
    <Page width="narrow" className="space-y-4" data-term-page={term.name}>
      <PageHeader
        crumb={
          <Link to="/p/$projectKey/glossary" params={{ projectKey: project.key }} className="hover:text-ink hover:underline">
            {l.term.back}
          </Link>
        }
        title={term.name}
        actions={
          <div className="flex gap-2">
            {may && !editing && (
              <Button
                variant="secondary"
                icon={<Icon.Edit />}
                onClick={() => {
                  update.reset();
                  setBase(term.revision);
                  setEditing(true);
                }}
                data-action="edit-term"
              >
                {l.term.edit}
              </Button>
            )}
            {mayDelete && !editing && (
              <Button variant="danger" icon={<Icon.Trash />} onClick={() => setDeleting(true)} data-action="delete-term">
                {l.term.delete}
              </Button>
            )}
          </div>
        }
      />
      <div className="flex flex-wrap items-center gap-2 text-sm text-ink-muted">
        <Tag data-term-status={term.status}>{l.statuses[term.status]}</Tag>
        <span>{l.term.revision(term.revision, term.updatedBy.name, when.format(new Date(term.updatedAt)))}</span>
        {!own && <span>{l.term.fromLibrary(term.projectKey)}</span>}
      </div>
      {editing ? (
        <Card className="space-y-3 p-4">
          {stale && (
            <div className="flex flex-wrap items-center gap-2" data-term-stale>
              <ErrorBanner>{l.term.stale}</ErrorBanner>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  update.reset();
                  setEditing(false);
                  onReload();
                }}
                data-action="reload-term"
              >
                {l.term.reload}
              </Button>
            </div>
          )}
          <TermForm
            term={term}
            submitLabel={l.form.save}
            pending={update.isPending}
            error={stale ? null : update.error}
            onCancel={() => setEditing(false)}
            onSubmit={(values) =>
              update.mutate(
                {
                  baseRevision: base,
                  name: values.name,
                  definition: values.definition,
                  synonyms: values.synonyms,
                  avoid: values.avoid,
                  status: values.status,
                  ...(values.comment ? { comment: values.comment } : {}),
                },
                {
                  onSuccess: (saved) => {
                    setEditing(false);
                    toast.success(l.term.saved(saved.revision));
                  },
                },
              )
            }
          />
        </Card>
      ) : (
        <Card className="space-y-4 p-4">
          <section aria-labelledby="term-definition-title" className="space-y-2">
            <SectionTitle id="term-definition-title">{l.term.definition}</SectionTitle>
            {textOf(term.definition as DocNode).trim() ? (
              <DocView doc={term.definition as Doc} data-term-definition-text="" />
            ) : (
              <p className="text-sm text-ink-subtle">{l.term.noDefinition}</p>
            )}
          </section>
          <Words id="term-synonyms-title" title={l.term.synonyms} words={term.synonyms} none={l.term.noSynonyms} />
          <Words id="term-avoid-title" title={l.term.avoid} words={term.avoid} none={l.term.noAvoid} />
        </Card>
      )}
      <Uses termId={term.id} />
      <Revisions termId={term.id} />
      {deleting && <DeleteDialog project={project} term={term} onClose={() => setDeleting(false)} />}
    </Page>
  );
}

function Words({ id, title, words, none }: { id: string; title: string; words: string[]; none: string }) {
  return (
    <section aria-labelledby={id} className="space-y-2">
      <SectionTitle id={id}>{title}</SectionTitle>
      {words.length === 0 ? (
        <p className="text-sm text-ink-subtle">{none}</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {words.map((word) => (
            <li key={word}>
              <Tag>{word}</Tag>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Uses({ termId }: { termId: string }) {
  const l = t.glossary.usesPanel;
  const [offset, setOffset] = useState(0);
  const { data, error, refetch } = useTermUses(termId, offset);
  return (
    <Card className="space-y-3 p-4" data-term-uses>
      <section aria-labelledby="term-uses-title" className="space-y-3">
        <SectionTitle id="term-uses-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && data.uses.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {data && data.uses.length > 0 && (
          <Table aria-label={l.title} dense>
            <tbody>
              {data.uses.map((use) => (
                <tr key={use.requirement.id} data-term-use-row={use.requirement.key} data-suspect={use.suspect}>
                  <Td>
                    <Link
                      to="/p/$projectKey/r/$reqKey"
                      params={{ projectKey: use.requirement.projectKey, reqKey: use.requirement.key }}
                      className="text-ink hover:text-accent hover:underline"
                    >
                      <span className="font-mono text-xs text-ink-muted">{use.requirement.key}</span> {use.requirement.title}
                    </Link>
                  </Td>
                  <Td>
                    <Tag title={use.explicit ? l.explicitHint : undefined}>{use.explicit ? l.explicit : l.byWords}</Tag>
                  </Td>
                  <Td>
                    {use.suspect && (
                      <>
                        <Tag className="border-warning bg-warning-subtle text-ink" aria-hidden="true">
                          {l.suspect}
                        </Tag>
                        <span className="sr-only">{l.suspectHint(use.seenRevision)}</span>
                      </>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
        {data && data.total > TERM_USES_PAGE_SIZE && (
          <div className="flex gap-2">
            <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - TERM_USES_PAGE_SIZE))}>
              {t.glossary.previous}
            </Button>
            <Button variant="secondary" size="sm" disabled={offset + TERM_USES_PAGE_SIZE >= data.total} onClick={() => setOffset(offset + TERM_USES_PAGE_SIZE)}>
              {t.glossary.next}
            </Button>
          </div>
        )}
      </section>
    </Card>
  );
}

function Revisions({ termId }: { termId: string }) {
  const l = t.glossary.revisions;
  const { data: revisions, error, refetch } = useTermRevisions(termId);
  return (
    <Card className="space-y-3 p-4" data-term-revisions>
      <section aria-labelledby="term-revisions-title" className="space-y-3">
        <SectionTitle id="term-revisions-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!revisions && !error && <Skeleton />}
        {revisions && revisions.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {revisions && revisions.length > 0 && (
          <ol className="flex flex-col divide-y divide-border">
            {revisions.map((revision, i) => (
              <RevisionRow key={revision.number} revision={revision} before={revisions[i + 1]} />
            ))}
          </ol>
        )}
      </section>
    </Card>
  );
}

/** A revision and how its name and definition differ from the one before, as plain words. */
function RevisionRow({ revision, before }: { revision: TermRevision; before?: TermRevision }) {
  const l = t.glossary.revisions;
  const definition = (r: TermRevision) => textOf(r.definition as DocNode).trim();
  const nameChanged = before && before.name !== revision.name;
  const definitionChanged = before && definition(before) !== definition(revision);
  return (
    <li className="space-y-1.5 py-2" data-term-revision={revision.number}>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium text-ink">{l.entry(revision.number, revision.createdBy.name, when.format(new Date(revision.createdAt)))}</span>
        <Tag>{t.glossary.statuses[revision.status]}</Tag>
      </div>
      {revision.comment && <p className="text-sm text-ink-muted">{revision.comment}</p>}
      {!before && <p className="text-xs text-ink-subtle">{l.first}</p>}
      {before && !nameChanged && !definitionChanged && <p className="text-xs text-ink-subtle">{l.unchanged}</p>}
      {nameChanged && <Change label={l.nameChanged} before={before.name} after={revision.name} />}
      {definitionChanged && <Change label={l.definitionChanged} before={definition(before)} after={definition(revision)} />}
    </li>
  );
}

function Change({ label, before, after }: { label: string; before: string; after: string }) {
  const l = t.glossary.revisions;
  return (
    <div className="text-sm" data-term-change={label}>
      <span className="mr-2 text-xs font-medium text-ink-muted uppercase">{label}</span>
      <span className="whitespace-pre-line text-ink">
        {wordDiff(before, after).map((run, i) =>
          run.change === "added" ? (
            // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
            <ins key={i} className="bg-success-subtle no-underline">
              <span className="sr-only">{l.added} </span>
              {run.text}
            </ins>
          ) : run.change === "removed" ? (
            // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
            <del key={i} className="bg-danger-subtle">
              <span className="sr-only">{l.removed} </span>
              {run.text}
            </del>
          ) : (
            // biome-ignore lint/suspicious/noArrayIndexKey: a run has no identity but its place, and a comparison never reorders
            <span key={i}>{run.text}</span>
          ),
        )}
      </span>
    </div>
  );
}

function DeleteDialog({ project, term, onClose }: { project: Project; term: Term; onClose: () => void }) {
  const l = t.glossary.term;
  const remove = useDeleteTerm(term.id);
  const navigate = useNavigate();
  return (
    <Dialog
      open
      title={l.deleteTitle}
      onClose={onClose}
      attrs={{ "data-delete-term": "" }}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate(undefined, { onSuccess: () => void navigate({ to: "/p/$projectKey/glossary", params: { projectKey: project.key } }) })}
            data-action="confirm-delete-term"
          >
            {l.deleteConfirm}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-ink">{l.deleteIntro(term.name)}</p>
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      </div>
    </Dialog>
  );
}

import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { ended, useImport, useImportActions, useUploadImport, type Import } from "@/api/imports";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { IMPORT_MAX_BYTES } from "@/config";
import { t } from "@/i18n";
import { MappingCard } from "./MappingCard";
import { PeopleCard, type PersonChoices } from "./PeopleCard";
import { WordsCard } from "./WordsCard";

/** A project's import: a file uploaded, its mapping confirmed against what the project has, then the worker's
 * progress and what it did. For the project's administrators. */
export function ImportPanel({ project }: { project: Project }) {
  const [id, setId] = useState<string>();
  if (!project.can.administer) return <p className="text-sm text-ink-muted">{t.imports.notAdmin}</p>;
  return (
    <section aria-labelledby="import-title" className="space-y-4" data-import>
      <div className="space-y-1">
        <SectionTitle id="import-title">{t.imports.title}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.imports.intro}</p>
      </div>
      {id ? <ImportView key={id} id={id} projectKey={project.key} onAnother={() => setId(undefined)} /> : <UploadForm project={project} onUploaded={setId} />}
    </section>
  );
}

function UploadForm({ project, onUploaded }: { project: Project; onUploaded: (id: string) => void }) {
  const upload = useUploadImport(project.key);
  const [file, setFile] = useState<File | null>(null);
  const refusal = upload.error instanceof ApiError ? (upload.error.fields.file ?? upload.error.message) : upload.error?.message;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (file) upload.mutate(file, { onSuccess: (made) => onUploaded(made.id) });
  }

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="space-y-3" aria-label={t.imports.title} data-import-upload>
        <div className="space-y-1">
          <label htmlFor="import-file" className="block text-sm font-medium text-ink">
            {t.imports.file}
          </label>
          <input
            id="import-file"
            type="file"
            accept=".csv,text/csv"
            aria-describedby="import-file-hint"
            className="block text-sm text-ink"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            data-import-file
          />
          <p id="import-file-hint" className="text-xs text-ink-muted">
            {t.imports.fileHint}
          </p>
        </div>
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file || file.size > IMPORT_MAX_BYTES} data-action="upload-import">
          {t.imports.upload}
        </Button>
      </form>
    </Card>
  );
}

function ImportView({ id, projectKey, onAnother }: { id: string; projectKey: string; onAnother: () => void }) {
  const { data: found, error } = useImport(id);
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!found) return <Skeleton />;
  return found.state === "uploaded" ? (
    <MappingStep found={found} projectKey={projectKey} onAnother={onAnother} />
  ) : (
    <Progress found={found} onAnother={onAnother} />
  );
}

function MappingStep({ found, projectKey, onAnother }: { found: Import; projectKey: string; onAnother: () => void }) {
  const actions = useImportActions(found.id);
  const [mapping, setMapping] = useState(found.mapping);
  const [values, setValues] = useState<Record<string, Record<string, string>>>({});
  const [people, setPeople] = useState<PersonChoices>({});
  const problems = found.preview.problems;
  const failure = actions.map.error ?? actions.start.error ?? actions.cancel.error;

  // Every answer is sent at once, so the problems shown are always the ones the file has now.
  function remap(next: { mapping?: Record<string, number[]>; values?: typeof values; people?: PersonChoices }) {
    const body = { mapping: next.mapping ?? mapping, values: next.values ?? values, people: next.people ?? people };
    if (next.mapping) setMapping(next.mapping);
    if (next.values) setValues(next.values);
    if (next.people) setPeople(next.people);
    actions.start.reset();
    actions.map.mutate(body);
  }

  return (
    <div className="space-y-4" data-import-step="mapping">
      <p role="status" className="text-sm text-ink">
        {t.imports.uploaded(found.fileName, found.preview.rows)}
      </p>
      <MappingCard columns={found.preview.columns} targets={found.targets} mapping={mapping} onChange={(next) => remap({ mapping: next })} />
      <WordsCard projectKey={projectKey} words={found.preview.words} values={values} onChange={(next) => remap({ values: next })} />
      <PeopleCard people={found.preview.people} choices={people} onChange={(next) => remap({ people: next })} />
      <Card className="space-y-2 p-4" data-import-problems={problems.length}>
        <h3 className="text-sm font-medium text-ink">{t.imports.problemsTitle}</h3>
        {problems.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.imports.noProblems}</p>
        ) : (
          <ul className="list-disc space-y-1 pl-5 text-sm text-danger">
            {problems.map((problem, i) => (
              <li key={i}>{problem.row > 0 ? t.imports.problemOnLine(problem.row, problem.message) : problem.message}</li>
            ))}
          </ul>
        )}
      </Card>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          icon={<Icon.Upload />}
          disabled={problems.length > 0 || actions.map.isPending}
          loading={actions.start.isPending}
          onClick={() => actions.start.mutate()}
          data-action="start-import"
        >
          {t.imports.start}
        </Button>
        <Button variant="secondary" onClick={() => actions.cancel.mutate(undefined, { onSuccess: onAnother })} data-action="discard-import">
          {t.imports.discard}
        </Button>
      </div>
    </div>
  );
}

function Progress({ found, onAnother }: { found: Import; onAnother: () => void }) {
  const actions = useImportActions(found.id);
  const percent = found.total > 0 ? Math.round((found.done / found.total) * 100) : 100;
  return (
    <Card className="space-y-3 p-4" data-import-step={found.state}>
      <div className="flex flex-wrap items-center gap-2">
        <Tag>{t.imports.states[found.state]}</Tag>
        <span className="text-sm text-ink-muted tabular-nums" role="status" data-import-progress>
          {t.imports.progress(found.done, found.total)}
        </span>
      </div>
      <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={t.imports.progress(found.done, found.total)} />
      {found.error && <ErrorBanner>{found.error}</ErrorBanner>}
      {actions.cancel.error && <ErrorBanner>{actions.cancel.error.message}</ErrorBanner>}
      {ended(found.state) && (
        <ul className="space-y-1 text-sm text-ink" data-import-result>
          <li>{t.imports.imported(found.imported.length)}</li>
          <li>{t.imports.updated(found.updated.length)}</li>
        </ul>
      )}
      {found.refused.length > 0 && (
        <div className="space-y-1" data-import-refused>
          <h3 className="text-sm font-medium text-ink">{t.imports.refusedTitle}</h3>
          <ul className="list-disc space-y-1 pl-5 text-sm text-ink-muted">
            {found.refused.map((each, i) => (
              <li key={i}>{each.row > 0 ? t.imports.problemOnLine(each.row, each.reason) : each.reason}</li>
            ))}
          </ul>
        </div>
      )}
      {ended(found.state) ? (
        <Button variant="secondary" onClick={onAnother} data-action="import-another">
          {t.imports.another}
        </Button>
      ) : (
        <Button variant="secondary" loading={actions.cancel.isPending} onClick={() => actions.cancel.mutate()} data-action="stop-import">
          {t.imports.stop}
        </Button>
      )}
    </Card>
  );
}

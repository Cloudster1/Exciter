import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { reqifEnded, reqifExportHref, useReqIFActions, useReqIFImport, useUploadReqIF, type ReqIFImport } from "@/api/reqif";
import { Button, ButtonLink, Card, ErrorBanner, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REQIF_MAX_BYTES } from "@/config";
import { t } from "@/i18n";
import { LossList } from "./LossList";
import { SchemaCard } from "./SchemaCard";

/** A project's ReqIF exchange: the project out as a file for whoever views it, and a file in for its administrators,
 * its schema confirmed against what the project has, then the worker's progress and what it did. */
export function ReqIFPanel({ project }: { project: Project }) {
  const [id, setId] = useState<string>();
  return (
    <section aria-labelledby="reqif-title" className="space-y-4" data-reqif>
      <div className="space-y-1">
        <SectionTitle id="reqif-title">{t.reqif.title}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.reqif.intro}</p>
      </div>
      <ExportCard project={project} />
      <h3 className="text-sm font-medium text-ink">{t.reqif.importTitle}</h3>
      {!project.can.administer ? (
        <p className="text-sm text-ink-muted">{t.reqif.notAdmin}</p>
      ) : id ? (
        <ImportView key={id} id={id} projectKey={project.key} onAnother={() => setId(undefined)} />
      ) : (
        <UploadForm project={project} onUploaded={setId} />
      )}
    </section>
  );
}

function ExportCard({ project }: { project: Project }) {
  return (
    <Card className="space-y-3 p-4" data-reqif-export>
      <div className="space-y-1">
        <h3 className="text-sm font-medium text-ink">{t.reqif.exportTitle}</h3>
        <p className="text-sm text-ink-muted">{t.reqif.exportIntro}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <ButtonLink href={reqifExportHref(project.key, true)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="export-reqifz">
          {t.reqif.exportReqifz}
        </ButtonLink>
        <ButtonLink href={reqifExportHref(project.key, false)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="export-reqif">
          {t.reqif.exportReqif}
        </ButtonLink>
      </div>
    </Card>
  );
}

function UploadForm({ project, onUploaded }: { project: Project; onUploaded: (id: string) => void }) {
  const upload = useUploadReqIF(project.key);
  const [file, setFile] = useState<File | null>(null);
  const refusal = upload.error instanceof ApiError ? (upload.error.fields.file ?? upload.error.message) : upload.error?.message;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (file) upload.mutate(file, { onSuccess: (made) => onUploaded(made.id) });
  }

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="space-y-3" aria-label={t.reqif.importTitle} data-reqif-upload>
        <div className="space-y-1">
          <label htmlFor="reqif-file" className="block text-sm font-medium text-ink">
            {t.reqif.file}
          </label>
          <input
            id="reqif-file"
            type="file"
            accept=".reqif,.reqifz,application/xml,application/zip"
            aria-describedby="reqif-file-hint"
            className="block text-sm text-ink"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            data-reqif-file
          />
          <p id="reqif-file-hint" className="text-xs text-ink-muted">
            {t.reqif.fileHint}
          </p>
        </div>
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file || file.size > REQIF_MAX_BYTES} data-action="upload-reqif">
          {t.reqif.upload}
        </Button>
      </form>
    </Card>
  );
}

function ImportView({ id, projectKey, onAnother }: { id: string; projectKey: string; onAnother: () => void }) {
  const { data: found, error } = useReqIFImport(id);
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!found) return <Skeleton />;
  return found.state === "uploaded" ? (
    <SchemaStep found={found} projectKey={projectKey} onAnother={onAnother} />
  ) : (
    <Progress found={found} onAnother={onAnother} />
  );
}

function SchemaStep({ found, projectKey, onAnother }: { found: ReqIFImport; projectKey: string; onAnother: () => void }) {
  const actions = useReqIFActions(found.id);
  const [choices, setChoices] = useState<Record<string, string>>(found.choices);
  const { preview } = found;
  const failure = actions.choose.error ?? actions.start.error ?? actions.cancel.error;

  // Every choice is sent at once, so the problems shown are always the ones the file has now.
  function choose(next: Record<string, string>) {
    setChoices(next);
    actions.start.reset();
    actions.choose.mutate(next);
  }

  return (
    <div className="space-y-4" data-reqif-step="schema">
      <div role="status" className="space-y-1 text-sm text-ink">
        <p>{t.reqif.uploaded(found.fileName, preview.objects, preview.sourceTool)}</p>
        <p data-reqif-summary>{t.reqif.summary(preview.new, preview.updates)}</p>
      </div>
      <SchemaCard projectKey={projectKey} items={preview.schema} choices={choices} onChange={choose} />
      <LossList losses={preview.losses} lostCount={preview.lostCount} />
      <Card className="space-y-2 p-4" data-reqif-problems={preview.problems.length}>
        <h3 className="text-sm font-medium text-ink">{t.reqif.problemsTitle}</h3>
        {preview.problems.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.reqif.noProblems}</p>
        ) : (
          <ul className="list-disc space-y-1 pl-5 text-sm text-danger">
            {preview.problems.map((problem, i) => (
              <li key={i}>{problem.where ? `${problem.where}: ${problem.message}` : problem.message}</li>
            ))}
          </ul>
        )}
      </Card>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          icon={<Icon.Upload />}
          disabled={preview.problems.length > 0 || actions.choose.isPending}
          loading={actions.start.isPending}
          onClick={() => actions.start.mutate()}
          data-action="start-reqif"
        >
          {t.reqif.start}
        </Button>
        <Button variant="secondary" onClick={() => actions.cancel.mutate(undefined, { onSuccess: onAnother })} data-action="discard-reqif">
          {t.reqif.discard}
        </Button>
      </div>
    </div>
  );
}

function Progress({ found, onAnother }: { found: ReqIFImport; onAnother: () => void }) {
  const actions = useReqIFActions(found.id);
  const percent = found.total > 0 ? Math.round((found.done / found.total) * 100) : 100;
  return (
    <Card className="space-y-3 p-4" data-reqif-step={found.state}>
      <div className="flex flex-wrap items-center gap-2">
        <Tag>{t.reqif.states[found.state]}</Tag>
        <span className="text-sm text-ink-muted tabular-nums" role="status" data-reqif-progress>
          {t.reqif.progress(found.done, found.total)}
        </span>
      </div>
      <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={t.reqif.progress(found.done, found.total)} />
      {found.error && <ErrorBanner>{found.error}</ErrorBanner>}
      {reqifEnded(found.state) && (
        <ul className="space-y-1 text-sm text-ink" data-reqif-result>
          <li>{t.reqif.imported(found.imported.length)}</li>
          <li>{t.reqif.updated(found.updated.length)}</li>
          <li>{t.reqif.unchanged(found.unchanged)}</li>
        </ul>
      )}
      {found.refused.length > 0 && (
        <div className="space-y-1" data-reqif-refused>
          <h3 className="text-sm font-medium text-ink">{t.reqif.refusedTitle}</h3>
          <ul className="list-disc space-y-1 pl-5 text-sm text-ink-muted">
            {found.refused.map((each, i) => (
              <li key={i}>{each.where ? `${each.where}: ${each.reason}` : each.reason}</li>
            ))}
          </ul>
        </div>
      )}
      {reqifEnded(found.state) ? (
        <Button variant="secondary" onClick={onAnother} data-action="reqif-another">
          {t.reqif.another}
        </Button>
      ) : (
        <Button variant="secondary" loading={actions.cancel.isPending} onClick={() => actions.cancel.mutate()} data-action="stop-reqif">
          {t.reqif.stop}
        </Button>
      )}
    </Card>
  );
}

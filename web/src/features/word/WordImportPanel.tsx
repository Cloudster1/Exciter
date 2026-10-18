import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useReqIFActions, useReqIFImport, type ReqIFImport } from "@/api/reqif";
import { useTree } from "@/api/tree";
import { WORD_FRONT, WORD_KEYWORDS, WORD_PATTERN, WORD_STYLES, WORD_UNIT, useUploadWord, wordTableChoice } from "@/api/word";
import { Icon } from "@/components/icons";
import { Button, Card, ErrorBanner, SectionTitle, Select, Skeleton } from "@/components/ui";
import { WORD_CHOICES_DEBOUNCE_MS, WORD_DEFAULT_KEYWORDS, WORD_MAX_BYTES } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { LossList } from "@/features/reqif/LossList";
import { ProblemList, Progress } from "@/features/reqif/ReqIFPanel";
import { SchemaCard } from "@/features/reqif/SchemaCard";
import { PreviewTree } from "./PreviewTree";
import { WordChoicesStep } from "./WordChoicesStep";

/** A Word document into the project for its administrators: below which section, how it is read, the tree it makes,
 * then the worker's progress, as a ReqIF import goes on. */
export function WordImportPanel({ project }: { project: Project }) {
  const [id, setId] = useState<string>();
  return (
    <section aria-labelledby="word-title" className="space-y-4" data-word>
      <div className="space-y-1">
        <SectionTitle id="word-title">{t.word.title}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.word.intro}</p>
      </div>
      {!project.can.administer ? (
        <p className="text-sm text-ink-muted">{t.word.notAdmin}</p>
      ) : id ? (
        <ImportView key={id} id={id} project={project} onAnother={() => setId(undefined)} />
      ) : (
        <UploadForm project={project} onUploaded={setId} />
      )}
    </section>
  );
}

function UploadForm({ project, onUploaded }: { project: Project; onUploaded: (id: string) => void }) {
  const upload = useUploadWord(project.key);
  const { data: tree = [] } = useTree(project.key);
  const sections = tree.filter((node) => node.kind === "section");
  const [file, setFile] = useState<File | null>(null);
  const [parentKey, setParentKey] = useState("");
  const tooLarge = Boolean(file && file.size > WORD_MAX_BYTES);
  const fields = upload.error instanceof ApiError ? upload.error.fields : {};
  const refusal = tooLarge ? t.word.tooLarge : upload.error ? (fields.file ?? upload.error.message) : undefined;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (file) upload.mutate({ file, parentKey }, { onSuccess: (made) => onUploaded(made.id) });
  }

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="space-y-3" aria-label={t.word.title} data-word-upload>
        <div className="space-y-1">
          <label htmlFor="word-file" className="block text-sm font-medium text-ink">
            {t.word.file}
          </label>
          <input
            id="word-file"
            type="file"
            accept=".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
            aria-describedby="word-file-hint"
            className="block text-sm text-ink"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            data-word-file
          />
          <p id="word-file-hint" className="text-xs text-ink-muted">
            {t.word.fileHint}
          </p>
        </div>
        <Select
          label={t.word.parent}
          id="word-parent"
          hint={t.word.parentHint}
          error={fields.parentKey}
          value={parentKey}
          onChange={(event) => setParentKey(event.target.value)}
          data-word-parent
        >
          <option value="">{t.word.projectTop}</option>
          {sections.map((node) => (
            <option key={node.key} value={node.key}>{`${node.outline} ${node.title}`}</option>
          ))}
        </Select>
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file || tooLarge} data-action="upload-word">
          {t.word.upload}
        </Button>
      </form>
    </Card>
  );
}

function ImportView({ id, project, onAnother }: { id: string; project: Project; onAnother: () => void }) {
  const { data: found, error } = useReqIFImport(id);
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!found) return <Skeleton />;
  return found.state === "uploaded" ? <WordStep found={found} project={project} onAnother={onAnother} /> : <Progress found={found} onAnother={onAnother} />;
}

// What the server reads a choice left out as, shown so a person sees what the preview was made with.
function wordDefaults(project: Project, tables: number): Record<string, string> {
  const language = project.settings.language ?? "en";
  const defaults: Record<string, string> = {
    [WORD_UNIT]: "paragraph",
    [WORD_STYLES]: "",
    [WORD_PATTERN]: "",
    [WORD_KEYWORDS]: WORD_DEFAULT_KEYWORDS[language] ?? WORD_DEFAULT_KEYWORDS.en ?? "",
    [WORD_FRONT]: "section",
  };
  for (let n = 1; n <= tables; n++) defaults[wordTableChoice(n)] = "one";
  return defaults;
}

function WordStep({ found, project, onAnother }: { found: ReqIFImport; project: Project; onAnother: () => void }) {
  const actions = useReqIFActions(found.id);
  const { preview } = found;
  const [choices, setChoices] = useState<Record<string, string>>(found.choices);
  const settled = useDebounced(choices, WORD_CHOICES_DEBOUNCE_MS);
  const sent = useRef(found.choices);
  const send = actions.choose.mutate;
  const resetStart = actions.start.reset;
  const failure = actions.choose.error ?? actions.start.error ?? actions.cancel.error;
  const errors = actions.choose.error instanceof ApiError ? actions.choose.error.fields : {};

  // Every choice is sent at once once typing pauses, so the tree and the problems shown are the ones the document has now.
  useEffect(() => {
    if (settled === sent.current) return;
    sent.current = settled;
    resetStart();
    send(settled);
  }, [settled, send, resetStart]);

  const waiting = choices !== settled || actions.choose.isPending;
  return (
    <div className="space-y-4" data-word-step="choices">
      <div role="status" className="space-y-1 text-sm text-ink">
        <p>{t.word.uploaded(found.fileName)}</p>
        <p data-word-summary>{t.reqif.summary(preview.new, preview.updates)}</p>
      </div>
      <WordChoicesStep
        choices={choices}
        defaults={wordDefaults(project, preview.tables)}
        styles={preview.styles}
        tables={preview.tables}
        errors={errors}
        onChange={setChoices}
      />
      <SchemaCard projectKey={project.key} items={preview.schema} choices={choices} onChange={setChoices} />
      <PreviewTree nodes={preview.tree} />
      <LossList losses={preview.losses} lostCount={preview.lostCount} />
      <ProblemList problems={preview.problems} />
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          icon={<Icon.Upload />}
          disabled={preview.problems.length > 0 || waiting}
          loading={actions.start.isPending}
          onClick={() => actions.start.mutate()}
          data-action="start-word"
        >
          {t.reqif.start}
        </Button>
        <Button variant="secondary" onClick={() => actions.cancel.mutate(undefined, { onSuccess: onAnother })} data-action="discard-word">
          {t.reqif.discard}
        </Button>
      </div>
    </div>
  );
}

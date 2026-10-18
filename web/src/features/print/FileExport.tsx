import { useEffect, useRef, useState, type FormEvent } from "react";
import { useBaselines } from "@/api/baselines";
import { fetchFile, saveFile, type DocumentFormat } from "@/api/documents";
import { useExportTemplates, withTemplate } from "@/api/templates";
import { Button, Dialog, ErrorBanner, Select, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** What an export goes through: a template, and the baseline its change marks count from. */
interface Choice {
  template?: string;
  since?: string;
}

/**
 * Makes a document from href while it is open, saves it and closes; an export that fails stays open with the reason
 * and a way to try again. With projectKey, a project with templates is first asked which one (#214). Adapted from
 * Stator's features/print/FileExport.tsx.
 */
export function FileExportDialog({
  format,
  href,
  title,
  projectKey,
  onClose,
}: {
  format: DocumentFormat;
  href: string;
  title: string;
  projectKey?: string;
  onClose: () => void;
}) {
  const templates = useExportTemplates(projectKey);
  const [choice, setChoice] = useState<Choice>();
  const offered = templates.data ?? [];
  // A server without templates answers that it has none set up, which is no reason to stop an export.
  const asking = Boolean(projectKey) && !templates.isError && (templates.isPending || offered.length > 0) && !choice;
  return (
    <Dialog title={t.documents.title(t.documents[format], title)} onClose={onClose} data-document-export={asking ? "choosing" : undefined}>
      {asking ? (
        templates.isPending ? (
          <Skeleton />
        ) : (
          <ChooseTemplate projectKey={projectKey!} templates={offered} onChoose={setChoice} onClose={onClose} />
        )
      ) : (
        <Making format={format} href={withTemplate(href, choice?.template, choice?.since)} onClose={onClose} />
      )}
    </Dialog>
  );
}

function ChooseTemplate({
  projectKey,
  templates,
  onChoose,
  onClose,
}: {
  projectKey: string;
  templates: NonNullable<ReturnType<typeof useExportTemplates>["data"]>;
  onChoose: (c: Choice) => void;
  onClose: () => void;
}) {
  const [template, setTemplate] = useState("");
  const [since, setSince] = useState("");
  const marks = templates.find((tpl) => tpl.id === template)?.include.changeMarks ?? false;
  const { data: baselines = [] } = useBaselines(marks ? projectKey : "");

  function submit(event: FormEvent) {
    event.preventDefault();
    onChoose({ template: template || undefined, since: marks && since ? since : undefined });
  }

  return (
    <form onSubmit={submit} className="space-y-3" data-export-choose>
      <Select label={t.templates.template} id="export-template" value={template} onChange={(e) => setTemplate(e.target.value)} data-export-template>
        <option value="">{t.templates.noTemplate}</option>
        {templates.map((tpl) => (
          <option key={tpl.id} value={tpl.id}>
            {tpl.projectKey.toUpperCase() === projectKey.toUpperCase() ? tpl.name : `${tpl.name} (${tpl.projectKey})`}
          </option>
        ))}
      </Select>
      {marks && (
        <Select label={t.templates.since} id="export-since" value={since} onChange={(e) => setSince(e.target.value)} data-export-since>
          <option value="">{t.templates.sinceNone}</option>
          {baselines.map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </Select>
      )}
      <div className="flex justify-end gap-2 pt-3">
        <Button type="button" variant="secondary" onClick={onClose}>
          {t.documents.cancel}
        </Button>
        <Button type="submit" icon={<Icon.Download />} data-action="export-with-template">
          {t.templates.export}
        </Button>
      </div>
    </form>
  );
}

function Making({ format, href, onClose }: { format: DocumentFormat; href: string; onClose: () => void }) {
  const [error, setError] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  const close = useRef(onClose);
  close.current = onClose;

  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt changes on each retry, which is the point
  useEffect(() => {
    // The export runs on once the dialog is closed; only its answer is dropped.
    let live = true;
    setError(undefined);
    fetchFile(href, `document.${format}`).then(
      (file) => {
        if (!live) return;
        saveFile(file);
        close.current();
      },
      (failure: unknown) => {
        if (live) setError(failure instanceof Error ? failure.message : String(failure));
      },
    );
    return () => {
      live = false;
    };
  }, [format, href, attempt]);

  return (
    <div data-document-export={error ? "failed" : "making"}>
      {error ? (
        <ErrorBanner onRetry={() => setAttempt((n) => n + 1)}>{error}</ErrorBanner>
      ) : (
        <p role="status" className="flex items-start gap-2 text-sm text-ink" data-export-making={format}>
          <Icon.Download className="mt-0.5 shrink-0 animate-pulse text-ink-muted motion-reduce:animate-none" aria-hidden="true" />
          <span>{t.documents.making}</span>
        </p>
      )}
      <p className="mt-3 text-xs text-ink-muted">{t.documents.note}</p>
      <div className="flex justify-end pt-3">
        <Button type="button" variant="secondary" onClick={onClose} data-action={`close-${format}`}>
          {error ? t.documents.close : t.documents.cancel}
        </Button>
      </div>
    </div>
  );
}

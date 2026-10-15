import { useEffect, useRef, useState } from "react";
import { fetchFile, saveFile, type DocumentFormat } from "@/api/documents";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/**
 * Makes a document from href while it is open, saves it and closes; an export that fails stays open with the reason
 * and a way to try again. Adapted from Stator's features/print/FileExport.tsx.
 */
export function FileExportDialog({ format, href, title, onClose }: { format: DocumentFormat; href: string; title: string; onClose: () => void }) {
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
    <Dialog title={t.documents.title(t.documents[format], title)} onClose={onClose} data-document-export={error ? "failed" : "making"}>
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
    </Dialog>
  );
}

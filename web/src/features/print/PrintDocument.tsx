import { useEffect, type ReactNode } from "react";
import { useBaselineDocument, useProjectDocument, type DocumentEntry, type DocumentProperty, type SpecDocument } from "@/api/documents";
import { DOCUMENT_CONTENTS_DEPTH } from "@/config";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { cx } from "@/components/ui";
import { localDateFormat } from "@/lib/format";
import { usePrintReady } from "./printReady";

// The views the render service prints a document from, as Stator prints a page: the document alone, with no shell,
// in the light scheme on paper. The running header and footer are drawn by the browser from the meta stated here.

const day = localDateFormat({ dateStyle: "long" });

/** Paper is light, so a print takes the light scheme whatever the browser prefers. */
export function useLightScheme(): void {
  useEffect(() => {
    const html = document.documentElement;
    const before = html.getAttribute("data-theme");
    html.setAttribute("data-theme", "light");
    return () => {
      if (before === null) html.removeAttribute("data-theme");
      else html.setAttribute("data-theme", before);
    };
  }, []);
}

/** A project's document, or a section's, as printed. */
export function PrintProjectDocument({ projectKey, section }: { projectKey: string; section?: string }) {
  const { data, error } = useProjectDocument(projectKey, section);
  return <PrintFrame doc={data} failed={error?.message} />;
}

/** A baseline's document, as printed. */
export function PrintBaselineDocument({ baselineId }: { baselineId: string }) {
  const { data, error } = useBaselineDocument(baselineId);
  return <PrintFrame doc={data} failed={error?.message} />;
}

/** The frame of every print view: ready once loaded and drawn, or why it cannot be printed. */
function PrintFrame({ doc, failed }: { doc?: SpecDocument; failed?: string }) {
  useLightScheme();
  usePrintReady(Boolean(doc) && !failed);
  useEffect(() => {
    if (doc) document.title = doc.title;
  }, [doc]);
  if (failed) {
    return (
      <main className="print-view">
        <p role="alert" data-print-failed="">
          {failed || t.documents.printFailed}
        </p>
      </main>
    );
  }
  if (!doc) return <main className="print-view" aria-busy="true" />;
  return (
    <main className="print-view" data-print-view="">
      <div
        hidden
        data-print-meta=""
        data-header={`${doc.projectName} (${doc.projectKey})`}
        data-title={doc.title}
        data-footer={t.documents.exported(doc.exportedBy, day.format(new Date(doc.exportedAt)))}
        data-pages={t.documents.pages}
      />
      <DocumentView doc={doc} />
    </main>
  );
}

function of(doc: SpecDocument): string {
  switch (doc.kind) {
    case "section":
      return t.documents.ofSection(doc.sectionKey, doc.projectName, doc.projectKey);
    case "baseline":
      return t.documents.ofBaseline(doc.projectName, doc.projectKey);
    default:
      return t.documents.ofProject(doc.projectName, doc.projectKey);
  }
}

/** A document: its cover, its contents, then every entry. */
export function DocumentView({ doc }: { doc: SpecDocument }) {
  const b = doc.baseline;
  return (
    <article className="print-document" data-print-document={doc.kind}>
      <section className="print-cover" data-print-cover="">
        <h1 className="text-4xl font-semibold text-ink">{doc.title}</h1>
        <p className="text-lg text-ink-muted">{of(doc)}</p>
        <div className="space-y-1 text-sm text-ink-muted">
          {b && <p>{t.documents.taken(day.format(new Date(b.takenAt)), b.takenBy)}</p>}
          {b && (b.lockedAt ? <p className="font-semibold text-ink">{t.documents.locked(day.format(new Date(b.lockedAt)))}</p> : <p>{t.documents.unlocked}</p>)}
          {b && b.signatures > 0 && (
            <p className={b.signed ? "font-semibold text-ink" : undefined} data-print-signing={b.signed ? "signed" : "signing"}>
              {b.signed ? t.control.signed(b.signatures, b.signaturesRequired) : t.control.signing(b.signatures, b.signaturesRequired)}
            </p>
          )}
          {b?.description && <p>{b.description}</p>}
          <p>{t.documents.exported(doc.exportedBy, day.format(new Date(doc.exportedAt)))}</p>
          <p>{t.documents.holds(doc.entries.length)}</p>
        </div>
      </section>
      <nav className="print-contents" aria-label={t.documents.contents} data-print-contents="">
        <h2 className="mb-3 text-xl font-semibold text-ink">{t.documents.contents}</h2>
        <ol className="space-y-1 text-sm">
          {doc.entries
            .filter((e) => e.depth < DOCUMENT_CONTENTS_DEPTH)
            .map((e) => (
              <li key={e.id} style={{ paddingLeft: `${e.depth * 1.25}rem` }}>
                <a href={`#entry-${e.key}`} className="text-ink">
                  <span className="mr-3 tabular-nums text-ink-muted">{e.outline}</span>
                  {e.title}
                </a>
              </li>
            ))}
        </ol>
      </nav>
      {doc.entries.map((e) => (
        <Entry key={e.id} entry={e} />
      ))}
      {doc.entries.length < doc.total && <p className="mt-6 text-sm italic text-ink-muted">{t.documents.truncated(doc.entries.length, doc.total)}</p>}
    </article>
  );
}

const headingSize = ["text-2xl", "text-xl", "text-lg", "text-base"];

function Heading({ depth, id, children }: { depth: number; id: string; children: ReactNode }) {
  const className = cx("print-entry-heading font-semibold text-ink", headingSize[Math.min(depth, headingSize.length - 1)]);
  switch (Math.min(depth + 2, 6)) {
    case 2:
      return (
        <h2 id={id} className={className}>
          {children}
        </h2>
      );
    case 3:
      return (
        <h3 id={id} className={className}>
          {children}
        </h3>
      );
    case 4:
      return (
        <h4 id={id} className={className}>
          {children}
        </h4>
      );
    case 5:
      return (
        <h5 id={id} className={className}>
          {children}
        </h5>
      );
    default:
      return (
        <h6 id={id} className={className}>
          {children}
        </h6>
      );
  }
}

function Entry({ entry: e }: { entry: DocumentEntry }) {
  return (
    <section className="print-entry space-y-2" data-print-entry={e.key}>
      <Heading depth={e.depth} id={`entry-${e.key}`}>
        <span className="mr-2 tabular-nums">{e.outline}</span>
        {e.title}
      </Heading>
      <p className="text-xs text-ink-muted">{t.documents.keyAndRevision(e.key, e.revision)}</p>
      <DocView doc={e.statement as Doc} />
      {e.rationale != null && (
        <div>
          <p className="text-xs font-semibold uppercase text-ink-muted">{t.documents.rationale}</p>
          <DocView doc={e.rationale as Doc} />
        </div>
      )}
      {e.properties.length > 0 && (
        <table className="print-properties w-full text-sm" data-print-properties="">
          <tbody>
            {e.properties.map((p, i) => (
              <tr key={i}>
                <th scope="row" className="w-1/3 text-left font-semibold">
                  {p.field ? (t.documents.fields[p.field] ?? p.field) : p.name}
                </th>
                <td>
                  <Value property={p} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {e.links.length > 0 && (
        <div>
          <p className="text-xs font-semibold uppercase text-ink-muted">{t.documents.links}</p>
          <ul className="list-disc pl-5 text-sm" data-print-links="">
            {e.links.map((l, i) => (
              <li key={i}>
                {l.phrase} <strong>{l.key}</strong> {l.title}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

function Value({ property: p }: { property: DocumentProperty }) {
  if (p.document) return <DocView doc={p.document as Doc} />;
  if (p.flag !== null && p.flag !== undefined) return <>{p.flag ? t.documents.yes : t.documents.no}</>;
  if (p.field === "priority") return <>{t.requirements.priorities[p.text as keyof typeof t.requirements.priorities] ?? p.text}</>;
  return <>{p.text}</>;
}

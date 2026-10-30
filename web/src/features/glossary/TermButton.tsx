import { createContext, useContext, useId, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Link } from "@tanstack/react-router";
import { useTerm } from "@/api/glossary";
import { Skeleton, Tag, WordButton } from "@/components/ui";
import { useAnchored, useEscape, useFocusReturn, useOutsidePress } from "@/components/ui/overlay";
import { textOf, type DocNode } from "@/features/editor/schema";
import { t } from "@/i18n";

/** Where a printed document's words linked to a term point: the term's entry in its glossary chapter, or nowhere
 * when the chapter does not hold it. Outside a print view words linked to a term are a button. */
export const TermAnchors = createContext<((termId: string) => string | null) | null>(null);

/** Words carrying the `term` mark as a reader meets them (#210). */
export function TermWords({ termId, children }: { termId: string; children: ReactNode }) {
  const anchor = useContext(TermAnchors);
  if (anchor) {
    const href = anchor(termId);
    return href ? (
      <a href={href} className="term-link" data-term-id={termId}>
        {children}
      </a>
    ) : (
      <>{children}</>
    );
  }
  return <TermButton termId={termId}>{children}</TermButton>;
}

/** Words linked to a term, which show its definition when pressed; the term is read only then. */
function TermButton({ termId, children }: { termId: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  return (
    <>
      <WordButton
        ref={ref}
        className="term-link"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        aria-haspopup="dialog"
        onClick={() => setOpen(!open)}
        data-term-id={termId}
      >
        {children}
      </WordButton>
      {open && <Definition id={panelId} termId={termId} anchorRef={ref} onClose={() => setOpen(false)} />}
    </>
  );
}

function Definition({ id, termId, anchorRef, onClose }: { id: string; termId: string; anchorRef: RefObject<HTMLButtonElement | null>; onClose: () => void }) {
  const { data: term, error } = useTerm(termId);
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const place = useAnchored(true, anchorRef, panelRef, { side: "bottom" });
  useEscape(true, onClose);
  useOutsidePress(true, [anchorRef, panelRef], onClose);
  useFocusReturn(Boolean(term) || Boolean(error), panelRef, false);
  const l = t.glossary;
  const definition = term ? textOf(term.definition as DocNode).trim() : "";
  return createPortal(
    <div
      ref={panelRef}
      id={id}
      role="dialog"
      aria-labelledby={titleId}
      tabIndex={-1}
      style={place}
      className="fixed z-40 w-80 max-w-[90vw] space-y-2 rounded-overlay border border-border bg-surface-overlay p-3 text-sm shadow-2"
      data-term-definition={termId}
    >
      {error ? (
        <p id={titleId} className="text-danger">
          {l.reader.failed}
        </p>
      ) : !term ? (
        <>
          <p id={titleId} className="sr-only">
            {l.reader.loading}
          </p>
          <Skeleton />
        </>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-1.5">
            <p id={titleId} className="font-semibold text-ink">
              {term.name}
            </p>
            {term.status !== "approved" && <Tag>{l.statuses[term.status]}</Tag>}
          </div>
          <p className="whitespace-pre-line text-ink-muted">{definition || l.term.noDefinition}</p>
          {term.synonyms.length > 0 && <p className="text-xs text-ink-subtle">{l.print.synonyms(term.synonyms.join(", "))}</p>}
          <Link
            to="/projects/$projectKey/glossary/$termId"
            params={{ projectKey: term.projectKey, termId: term.id }}
            className="inline-block text-sm text-accent hover:underline"
            onClick={onClose}
          >
            {l.reader.openTerm}
          </Link>
        </>
      )}
    </div>,
    document.body,
  );
}

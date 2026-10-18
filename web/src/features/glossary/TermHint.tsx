import { useRef } from "react";
import { createPortal } from "react-dom";
import { useAnchored } from "@/components/ui/overlay";
import type { TermMark } from "@/features/editor/terms";
import { t } from "@/i18n";

/** A term's name and the start of its definition beside its words in the editor (#210), while a pointer rests on
 * them or the caret is in them; the editor's live region says the same to a screen reader. */
export function TermHint({ use, anchor }: { use: TermMark; anchor: HTMLElement }) {
  const anchorRef = useRef<HTMLElement | null>(anchor);
  anchorRef.current = anchor;
  const panelRef = useRef<HTMLDivElement>(null);
  const place = useAnchored(true, anchorRef, panelRef, { side: "bottom" });
  return createPortal(
    <div
      ref={panelRef}
      role="tooltip"
      style={place}
      className="fixed z-40 max-w-sm space-y-1 rounded-overlay border border-border bg-surface-overlay p-3 text-sm shadow-2"
      data-term-popover={use.termId}
    >
      <p className="font-medium text-ink">{use.name}</p>
      {use.preview && <p className="text-ink-muted">{use.preview}</p>}
      {use.explicit && <p className="text-xs text-ink-subtle">{t.glossary.editor.explicit}</p>}
    </div>,
    document.body,
  );
}

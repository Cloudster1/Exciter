import { useCallback, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cx } from "./cx";
import { useAnchored, useEscape, useFocusReturn, useOutsidePress } from "./overlay";

// A panel anchored to its trigger and not modal: the page stays live, Escape
// or a press outside closes it, and focus goes in on open and back on close.
// Drawn at the end of the document, fixed beside the trigger, so a scrolling
// panel or table cannot cut it off.
export function Popover({
  open,
  onClose,
  trigger,
  children,
  align = "start",
  label,
  className,
}: {
  open: boolean;
  onClose: () => void;
  trigger: ReactNode;
  children: ReactNode;
  align?: "start" | "end";
  label: string;
  className?: string;
}) {
  const triggerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close);
  useOutsidePress(open, [triggerRef, panelRef], close);
  useFocusReturn(open, panelRef, false);
  const place = useAnchored(open, triggerRef, panelRef, { align });
  return (
    <div className="relative inline-flex">
      <div ref={triggerRef} className="inline-flex">
        {trigger}
      </div>
      {open &&
        createPortal(
          <div
            ref={panelRef}
            role="dialog"
            aria-label={label}
            data-popover
            style={place}
            className={cx("fixed z-30 min-w-56 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-3 shadow-2", className)}
          >
            {children}
          </div>,
          document.body,
        )}
    </div>
  );
}

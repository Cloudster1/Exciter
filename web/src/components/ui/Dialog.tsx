import { useCallback, useId, useLayoutEffect, useRef, type CSSProperties, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { DIALOG_BOTTOM_PX, DIALOG_TOP_VH, DRAWER_WIDTH } from "@/config";
import { Button, IconButton } from "./Button";
import { cx } from "./cx";
import { useEscape, useFocusReturn, useScrollLock } from "./overlay";
import { Icon } from "../icons";
import { t } from "@/i18n";

type DialogProps = {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: "sm" | "md" | "lg";
  /** Forwarded to the dialog element, for the attributes tests read. */
  attrs?: Record<string, string>;
};

const dialogWidth = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-3xl" };

/** Where a dialog sits on its scrolling layer: a margin, since pinned parts stop at the layer's padding. */
const dialogPlace: CSSProperties = { marginTop: `${DIALOG_TOP_VH}vh`, marginBottom: DIALOG_BOTTOM_PX };
/** Where a modal that does not scroll as a whole sits in the window. */
export const modalBackdropStyle: CSSProperties = { paddingTop: `${DIALOG_TOP_VH}vh`, paddingBottom: DIALOG_BOTTOM_PX };
/** For a modal that scrolls a list of its own, like the command palette: never past the bottom. */
export const modalPanelStyle: CSSProperties = { maxHeight: `calc(100dvh - ${DIALOG_TOP_VH}vh - ${DIALOG_BOTTOM_PX}px)` };

/** Whether a press on a scrolling box landed on its scrollbar rather than inside it. */
function onScrollbar(event: { clientX: number; currentTarget: HTMLElement }): boolean {
  const box = event.currentTarget;
  return event.clientX >= box.getBoundingClientRect().left + box.clientLeft + box.clientWidth;
}

// The pinned title and buttons cover the edges of the scrolling layer, so the
// browser is told to keep what it scrolls to (a focused field, an error) clear of them.
function useClearOfPinned(
  open: boolean,
  scroller: RefObject<HTMLElement | null>,
  header: RefObject<HTMLElement | null>,
  footer: RefObject<HTMLElement | null>,
) {
  useLayoutEffect(() => {
    if (!open) return;
    const pad = () => {
      const box = scroller.current;
      if (!box) return;
      box.style.scrollPaddingTop = `${pinnedHeight(header.current)}px`;
      box.style.scrollPaddingBottom = `${pinnedHeight(footer.current)}px`;
    };
    pad();
    const resized = new ResizeObserver(pad);
    for (const part of [header.current, footer.current]) if (part) resized.observe(part);
    window.addEventListener("resize", pad);
    return () => {
      resized.disconnect();
      window.removeEventListener("resize", pad);
    };
  }, [open, scroller, header, footer]);
}

/** How much of the window a part covers while it is pinned; nothing when it scrolls along. */
function pinnedHeight(part: HTMLElement | null): number {
  return part && getComputedStyle(part).position === "sticky" ? part.offsetHeight : 0;
}

// Modal: the page behind is inert, focus is trapped and Escape closes. Its layer
// scrolls like a page, so wheel, touch and keys move a tall dialog wherever they land.
export function Dialog({ open, onClose, title, description, children, footer, size = "md", attrs }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close);
  useScrollLock(open);
  useFocusReturn(open, ref, true);
  useClearOfPinned(open, backdropRef, headerRef, footerRef);
  if (!open) return null;
  return createPortal(
    <div
      ref={backdropRef}
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto overscroll-contain bg-ink/40 px-4"
      onPointerDown={(e) => e.target === e.currentTarget && !onScrollbar(e) && close()}
      data-dialog-backdrop
    >
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        {...attrs}
        style={dialogPlace}
        className={cx("relative flex w-full flex-col rounded-overlay border border-border bg-surface-overlay shadow-2", dialogWidth[size])}
      >
        <div
          ref={headerRef}
          className="tall:sticky tall:top-0 z-10 flex shrink-0 items-start gap-3 rounded-t-overlay bg-surface-overlay px-5 pt-4 pb-2"
          data-dialog-header
        >
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-base font-semibold text-ink">
              {title}
            </h2>
            {description && (
              <p id={descriptionId} className="mt-1 text-sm text-ink-muted">
                {description}
              </p>
            )}
          </div>
          <IconButton icon={<Icon.X />} label={t.kit.close} size="sm" onClick={close} data-focus-last="" />
        </div>
        {children && (
          <div className="px-5 pt-2 pb-4" data-dialog-body>
            {children}
          </div>
        )}
        {footer && (
          <div
            ref={footerRef}
            className="tall:sticky tall:bottom-0 z-10 flex shrink-0 justify-end gap-2 rounded-b-overlay border-t border-border bg-surface-overlay px-5 py-3"
            data-dialog-footer
          >
            {footer}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}

// The one dialog that stands between a click and a deletion. The button says
// the verb and the noun, so the reader knows what goes before it goes.
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  noun,
  verb = t.kit.confirm.verbs.delete,
  body,
  loading,
  error,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  noun: string;
  verb?: string;
  body: ReactNode;
  loading?: boolean;
  error?: ReactNode;
}) {
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t.kit.confirm.title(verb, noun)}
      size="sm"
      attrs={{ "data-confirm": noun }}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button variant="danger" onClick={onConfirm} loading={loading} data-action="confirm">
            {t.kit.confirm.action(verb, noun)}
          </Button>
        </>
      }
    >
      <div className="space-y-2 text-sm text-ink">
        {body}
        {error && <p className="text-danger">{error}</p>}
      </div>
    </Dialog>
  );
}

type PanelProps = {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  width?: number;
  /** Controls beside the close button: previous, next, open the page. */
  actions?: ReactNode;
  attrs?: Record<string, string>;
};

/** The header and scrolling body the drawer and the docked panel share. */
function PanelHeader({ titleId, title, actions, onClose }: { titleId: string; title: ReactNode; actions?: ReactNode; onClose: () => void }) {
  return (
    <div className="flex items-center gap-2 border-b border-border px-5 py-3">
      <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">
        {title}
      </h2>
      {actions}
      <IconButton icon={<Icon.X />} label={t.kit.close} size="sm" onClick={onClose} data-focus-last="" />
    </div>
  );
}

// A panel from the right that keeps the page behind it in view but not in
// reach. Escape in a field it holds is left to the field, so a draft survives.
export function Drawer({ open, onClose, title, children, width = DRAWER_WIDTH, actions, attrs }: PanelProps) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close, { whileEditing: false });
  useFocusReturn(open, ref, true);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-50 flex justify-end bg-ink/30" onPointerDown={(e) => e.target === e.currentTarget && close()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        {...attrs}
        style={{ width: `min(${width}px, 100vw)` }}
        className="flex h-full flex-col border-l border-border bg-surface shadow-2"
      >
        <PanelHeader titleId={titleId} title={title} actions={actions} onClose={close} />
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>,
    document.body,
  );
}

const INTERACTIVE = "a, button, input, select, label";

/** Whether a click landed on something of its own, so a row around it should not also act. */
export function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(INTERACTIVE) !== null;
}

// A column beside the page rather than a layer over it: no scrim, no focus
// trap, and Escape closes it only when nothing is being typed and nothing
// opened above it, so the list stays in reach while the panel is up.
export function DockedPanel({ open, onClose, title, children, width = DRAWER_WIDTH, actions, attrs }: PanelProps) {
  const ref = useRef<HTMLElement>(null);
  const titleId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close, { whileEditing: false });
  useFocusReturn(open, ref, false);
  if (!open) return null;
  return (
    <aside
      ref={ref}
      role="complementary"
      aria-labelledby={titleId}
      {...attrs}
      style={{ width }}
      className="flex h-full shrink-0 flex-col border-l border-border bg-surface"
    >
      <PanelHeader titleId={titleId} title={title} actions={actions} onClose={close} />
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
    </aside>
  );
}

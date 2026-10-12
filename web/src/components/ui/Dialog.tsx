import { useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "@/i18n";
import { IconButton } from "./Button";
import { Icon } from "@/components/icons";
import { cx } from "./cx";
import { useEscape, useFocusReturn } from "./overlay";

/**
 * A modal over the page: focus moves in and is held there, Escape and the
 * close button end it, and focus goes back to what opened it.
 */
export function Dialog({
  title,
  wide = false,
  fill = false,
  onClose,
  children,
  ...rest
}: {
  title: string;
  /** Room for a form with lists side by side. */
  wide?: boolean;
  /** Room for a document shown in place, as wide as the window allows. */
  fill?: boolean;
  onClose: () => void;
  children: ReactNode;
  [attr: `data-${string}`]: string | undefined;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // A press that began inside the dialog and ended on the scrim, because the
  // dialog shrank under the pointer, is no click outside it.
  const pressedOnScrim = useRef(false);
  const titleId = useId();
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);
  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the dialog with Escape or its close button
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-ink/30 p-4 pt-[10vh]"
      onMouseDown={(e) => {
        pressedOnScrim.current = e.target === e.currentTarget;
      }}
      onClick={(e) => e.target === e.currentTarget && pressedOnScrim.current && onClose()}
    >
      <div
        {...rest}
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={cx(
          "max-h-[85vh] w-full overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-4 shadow-2",
          fill ? "max-w-5xl" : wide ? "max-w-2xl" : "max-w-md",
        )}
      >
        <div className="mb-3 flex items-center gap-2">
          <h2 id={titleId} className="min-w-0 flex-1 truncate text-base font-semibold text-ink">
            {title}
          </h2>
          <IconButton icon={<Icon.X />} label={t.common.close} size="sm" onClick={onClose} data-focus-last="" />
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}

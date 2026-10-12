import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Icon } from "@/components/icons";
import { cx } from "./cx";
import { focusables, useAnchored, useEscape, useOutsidePress } from "./overlay";

export interface MenuItem {
  label: ReactNode;
  onSelect: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  /** Makes the item one of a set to pick from, such as a language, marked when it is the one picked. */
  checked?: boolean;
  /** Forwarded to the item, for the attributes tests read. */
  attrs?: Record<string, string>;
}

// Where a list is drawn: at the end of the landmark its trigger sits in, so a
// screen reader finds it in the same region, and a modal dialog keeps it.
const LANDMARK =
  'main, header, nav, aside, footer, [role="banner"], [role="main"], [role="navigation"], [role="complementary"], [role="contentinfo"], [role="region"], [role="dialog"]';

// Arrows move, Home and End jump, a letter finds, Escape returns focus to the trigger. The list is
// fixed beside the trigger at the end of its landmark, so a scrolling panel cannot cut it off.
export function Menu({
  trigger,
  items,
  label,
  align = "start",
  className,
}: {
  trigger: (props: { open: boolean; toggle: () => void; "aria-haspopup": "menu"; "aria-expanded": boolean; "aria-controls": string | undefined }) => ReactNode;
  items: MenuItem[];
  label: string;
  align?: "start" | "end";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [host, setHost] = useState<HTMLElement | null>(null);
  const listId = useId();
  const wrapRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  // A press elsewhere moved the reader on; only a keyboard close or a pick
  // brings focus back to the trigger.
  const leaveFocus = useRef(false);
  const close = useCallback(() => setOpen(false), []);
  const closeFromOutside = useCallback(() => {
    leaveFocus.current = true;
    setOpen(false);
  }, []);
  useEscape(open, close);
  const outside = useRef([wrapRef, listRef]);
  useOutsidePress(open, outside.current, closeFromOutside);

  const place = useAnchored(open, wrapRef, listRef, { align });

  function toggle() {
    setHost(wrapRef.current?.closest<HTMLElement>(LANDMARK) ?? document.body);
    setOpen((o) => !o);
  }

  // The list is hidden until it has been placed, and a browser will not focus
  // a hidden element, so the first item takes focus only once it is placed.
  const placed = open && place.visibility !== "hidden";
  useEffect(() => {
    if (placed && listRef.current) focusables(listRef.current)[0]?.focus();
  }, [placed]);

  const wasOpen = useRef(false);
  useEffect(() => {
    if (!open && wasOpen.current && !leaveFocus.current) {
      focusables(wrapRef.current!)[0]?.focus();
    }
    wasOpen.current = open;
    leaveFocus.current = false;
  }, [open]);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const list = listRef.current;
    if (!list) return;
    const options = focusables(list);
    const index = options.indexOf(document.activeElement as HTMLElement);
    const go = (i: number) => options[(i + options.length) % options.length]?.focus();
    switch (event.key) {
      // The list sits at the end of its landmark, so a Tab out of it would
      // land nowhere near the trigger; it closes and focus goes home instead.
      case "Tab":
        event.preventDefault();
        close();
        break;
      case "ArrowDown":
        event.preventDefault();
        go(index + 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        go(index - 1);
        break;
      case "Home":
        event.preventDefault();
        go(0);
        break;
      case "End":
        event.preventDefault();
        go(options.length - 1);
        break;
      default:
        if (event.key.length === 1 && /\S/.test(event.key)) {
          const found =
            options.find((el, i) => i > index && el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase())) ??
            options.find((el) => el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase()));
          found?.focus();
        }
    }
  }

  return (
    <div ref={wrapRef} className={cx("relative inline-flex", className)}>
      {trigger({ open, toggle, "aria-haspopup": "menu", "aria-expanded": open, "aria-controls": open ? listId : undefined })}
      {open &&
        host &&
        createPortal(
          <div
            ref={listRef}
            id={listId}
            role="menu"
            aria-label={label}
            onKeyDown={onKeyDown}
            style={place}
            className="fixed z-30 min-w-44 overflow-y-auto overscroll-contain rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
          >
            {items.map((item, i) => (
              <button
                key={i}
                type="button"
                role={item.checked === undefined ? "menuitem" : "menuitemradio"}
                aria-checked={item.checked}
                // aria-disabled rather than disabled keeps the item in the
                // arrow order, so the reader learns it exists.
                aria-disabled={item.disabled || undefined}
                tabIndex={-1}
                {...item.attrs}
                onClick={() => {
                  if (item.disabled) return;
                  setOpen(false);
                  item.onSelect();
                }}
                className={cx(
                  "flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-sm",
                  item.danger ? "text-danger hover:bg-danger-subtle" : "text-ink hover:bg-surface-raised",
                  "focus:bg-surface-raised focus-visible:-outline-offset-2 aria-disabled:cursor-not-allowed aria-disabled:opacity-50",
                )}
              >
                {item.checked !== undefined && (
                  <span className="size-4 text-accent" aria-hidden="true">
                    {item.checked && <Icon.Check />}
                  </span>
                )}
                {item.icon && <span className="text-ink-muted [&_svg]:size-4">{item.icon}</span>}
                {item.label}
              </button>
            ))}
          </div>,
          host,
        )}
    </div>
  );
}

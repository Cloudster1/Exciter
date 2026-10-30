import { Fragment, useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Icon } from "../icons";
import { cx } from "./cx";
import { focusables, useAnchored, useEscape, useOutsidePress, type AnchoredSide } from "./overlay";

export interface MenuItem {
  label: ReactNode;
  /** Runs on a pick; a link may leave it out. */
  onSelect?: () => void;
  /** Makes the item a link, so the browser's own open in a new tab works on it. */
  href?: string;
  /** Sets the item apart from those above it by a line, without a heading. */
  separated?: boolean;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  /** Makes the item one of a set to pick from, such as a language, marked when it is the one picked. */
  checked?: boolean;
  /** Forwarded to the item, for the attributes tests read. */
  attrs?: Record<string, string>;
}

/** Items under a heading, which a screen reader announces as a group. */
export interface MenuGroup {
  group: string;
  items: MenuItem[];
  attrs?: Record<string, string>;
}

export type MenuEntry = MenuItem | MenuGroup;

/** Whether an entry is a group of items rather than one item. */
export function isMenuGroup(entry: MenuEntry): entry is MenuGroup {
  return "group" in entry;
}

const ITEM_CLASS =
  "flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-sm focus:bg-surface-raised focus-visible:-outline-offset-2 aria-disabled:cursor-not-allowed aria-disabled:opacity-50";

// Where a list is drawn: at the end of the landmark its trigger sits in, so a
// screen reader finds it in the same region, and a modal dialog keeps it.
const LANDMARK =
  'main, header, nav, aside, footer, [role="banner"], [role="main"], [role="navigation"], [role="complementary"], [role="contentinfo"], [role="region"], [role="dialog"]';

// A list of actions behind one button: arrows move, Home and End jump, typing
// a letter finds, Escape returns focus to the trigger. The list is drawn at
// the end of the trigger's landmark, fixed beside the trigger, so a table or
// a panel that scrolls cannot cut it off; it opens upward when the bottom is near.
export function Menu({
  trigger,
  items,
  label,
  header,
  align = "start",
  side = "bottom",
  className,
}: {
  trigger: (props: { open: boolean; toggle: () => void; "aria-haspopup": "menu"; "aria-expanded": boolean; "aria-controls": string | undefined }) => ReactNode;
  items: MenuEntry[];
  label: string;
  /** Drawn above the items and outside them, such as who the menu is about. */
  header?: ReactNode;
  align?: "start" | "end";
  /** Where the list opens: below the trigger, flipping above near the bottom, or to its right, as from the rail. */
  side?: AnchoredSide;
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

  const place = useAnchored(open, wrapRef, listRef, { align, side });

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
    // A field in the header, such as a filter, keeps its own keys for the text.
    const typing = event.target instanceof HTMLInputElement;
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
        if (typing) break;
        event.preventDefault();
        go(0);
        break;
      case "End":
        if (typing) break;
        event.preventDefault();
        go(options.length - 1);
        break;
      default:
        if (!typing && event.key.length === 1 && /\S/.test(event.key)) {
          const found =
            options.find((el, i) => i > index && el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase())) ??
            options.find((el) => el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase()));
          found?.focus();
        }
    }
  }

  function renderItem(item: MenuItem, key: number) {
    const className = cx(ITEM_CLASS, item.danger ? "text-danger hover:bg-danger-subtle" : "text-ink hover:bg-surface-raised");
    const content = (
      <>
        {item.checked !== undefined && (
          <span className="size-4 text-accent [&_svg]:size-4" aria-hidden="true">
            {item.checked && <Icon.Check />}
          </span>
        )}
        {item.icon && <span className="text-ink-muted [&_svg]:size-4 [&_img]:size-4">{item.icon}</span>}
        {item.label}
      </>
    );
    if (item.href) {
      return (
        <a
          key={key}
          href={item.href}
          role="menuitem"
          tabIndex={-1}
          {...item.attrs}
          onClick={() => {
            setOpen(false);
            item.onSelect?.();
          }}
          className={cx(className, "no-underline")}
        >
          {content}
        </a>
      );
    }
    return (
      <button
        key={key}
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
          item.onSelect?.();
        }}
        className={className}
      >
        {content}
      </button>
    );
  }

  return (
    <div ref={wrapRef} className={cx("relative inline-flex", className)}>
      {trigger({ open, toggle, "aria-haspopup": "menu", "aria-expanded": open, "aria-controls": open ? listId : undefined })}
      {open &&
        host &&
        createPortal(
          // The header sits beside the menu rather than in it, since a menu
          // holds nothing but its items and their groups.
          <div
            ref={listRef}
            onKeyDown={onKeyDown}
            style={place}
            className="fixed z-30 min-w-44 overflow-y-auto overscroll-contain rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
          >
            {header}
            <div id={listId} role="menu" aria-label={label}>
              {items.map((entry, i) =>
                isMenuGroup(entry) ? (
                  <div key={i} role="group" aria-labelledby={`${listId}-${i}`} className={cx(i > 0 && "mt-1 border-t border-border pt-1")} {...entry.attrs}>
                    <div id={`${listId}-${i}`} className="px-2 pt-1 pb-0.5 text-2xs font-semibold text-ink-subtle" data-menu-group={entry.group}>
                      {entry.group}
                    </div>
                    {entry.items.map((item, j) => renderItem(item, j))}
                  </div>
                ) : (
                  <Fragment key={i}>
                    {entry.separated && i > 0 && <div role="separator" className="my-1 border-t border-border" />}
                    {renderItem(entry, i)}
                  </Fragment>
                ),
              )}
            </div>
          </div>,
          host,
        )}
    </div>
  );
}

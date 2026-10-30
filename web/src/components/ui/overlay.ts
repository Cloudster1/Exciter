import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { MENU_GAP_PX, POPOVER_EDGE_PX, POPOVER_MAX_HEIGHT_PX } from "@/config";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
}

const EDITABLE = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

/** Whether a key pressed on this element was meant for typing, not for the page. */
export function isEditing(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(EDITABLE) !== null;
}

interface EscapeEntry {
  onClose: () => void;
  whileEditing: boolean;
}

// Every open overlay, in the order it opened. One listener hands Escape to the
// newest alone, so a menu in a drawer closes and the drawer stays.
const escapeStack: Array<RefObject<EscapeEntry>> = [];

function onEscape(event: KeyboardEvent) {
  if (event.key !== "Escape" || event.defaultPrevented) return;
  const top = escapeStack[escapeStack.length - 1]?.current;
  if (!top || (!top.whileEditing && isEditing(event.target))) return;
  event.preventDefault();
  top.onClose();
}

/**
 * Calls onClose on Escape while open and nothing opened since is still open.
 * Without whileEditing, Escape in a field is left to the field.
 */
export function useEscape(open: boolean, onClose: () => void, { whileEditing = true }: { whileEditing?: boolean } = {}) {
  const entry = useRef<EscapeEntry>({ onClose, whileEditing });
  useLayoutEffect(() => {
    entry.current = { onClose, whileEditing };
  });
  // Keyed on open alone: a new onClose must not lift this overlay above one
  // that opened after it.
  useEffect(() => {
    if (!open) return;
    escapeStack.push(entry);
    if (escapeStack.length === 1) document.addEventListener("keydown", onEscape);
    return () => {
      escapeStack.splice(escapeStack.indexOf(entry), 1);
      if (escapeStack.length === 0) document.removeEventListener("keydown", onEscape);
    };
  }, [open]);
}

/** Calls onClose on a pointer press outside every ref given. */
export function useOutsidePress(open: boolean, refs: Array<RefObject<HTMLElement | null>>, onClose: () => void) {
  useEffect(() => {
    if (!open) return;
    function onPress(event: PointerEvent) {
      const target = event.target as Node | null;
      if (refs.some((ref) => ref.current?.contains(target))) return;
      onClose();
    }
    document.addEventListener("pointerdown", onPress);
    return () => document.removeEventListener("pointerdown", onPress);
  }, [open, refs, onClose]);
}

// Focus goes into the overlay when it opens and back to where it was when it
// closes, so a keyboard user is never left on an element that has gone.
export function useFocusReturn(open: boolean, into: RefObject<HTMLElement | null>, trap: boolean) {
  const previous = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!open) return;
    previous.current = document.activeElement as HTMLElement | null;
    const root = into.current;
    // The close button is first in the DOM but last in intent: focus lands on
    // the first thing the reader came to do.
    const candidates = root ? focusables(root) : [];
    const first = candidates.find((el) => !el.hasAttribute("data-focus-last")) ?? candidates[0] ?? root;
    first?.focus({ preventScroll: true });
    function onKey(event: KeyboardEvent) {
      if (!trap || event.key !== "Tab" || !root) return;
      const items = focusables(root);
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const firstItem = items[0]!;
      const lastItem = items[items.length - 1]!;
      if (event.shiftKey && document.activeElement === firstItem) {
        event.preventDefault();
        lastItem.focus();
      } else if (!event.shiftKey && document.activeElement === lastItem) {
        event.preventDefault();
        firstItem.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      previous.current?.focus({ preventScroll: true });
    };
  }, [open, into, trap]);
}

/** Stops the page behind a modal from scrolling while it is open. */
export function useScrollLock(open: boolean) {
  useEffect(() => {
    if (!open) return;
    const before = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = before;
    };
  }, [open]);
}

let counter = 0;
/** A stable id for aria wiring; React's useId is fine too, this one reads better in tests. */
export function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

export type AnchoredSide = "top" | "bottom" | "right";

interface Size {
  width: number;
  height: number;
}

/** Where an anchored panel goes, and how tall it may be there before it scrolls. */
export interface AnchoredBox {
  top: number;
  left?: number;
  right?: number;
  maxHeight: number;
}

/**
 * Below the anchor by default, above when only that side has room, and on
 * whichever side has more when neither does; the panel scrolls past that.
 */
export function anchoredBox(
  anchor: Pick<DOMRect, "top" | "bottom" | "left" | "right" | "height">,
  panel: Size,
  view: Size,
  { align = "start", side = "bottom" }: { align?: "start" | "end"; side?: AnchoredSide } = {},
): AnchoredBox {
  const gap = MENU_GAP_PX;
  const edge = POPOVER_EDGE_PX;
  if (side === "right") {
    const maxHeight = Math.min(POPOVER_MAX_HEIGHT_PX, view.height - 2 * edge);
    const height = Math.min(panel.height, maxHeight);
    return {
      left: Math.min(anchor.right + gap, view.width - panel.width),
      top: Math.max(edge, Math.min(anchor.top + anchor.height / 2 - height / 2, view.height - edge - height)),
      maxHeight,
    };
  }
  const below = anchor.bottom + gap;
  const roomBelow = view.height - edge - below;
  const roomAbove = anchor.top - gap - edge;
  const fitsBelow = panel.height <= roomBelow;
  const fitsAbove = panel.height <= roomAbove;
  const goesBelow = side === "bottom" ? fitsBelow || (!fitsAbove && roomBelow >= roomAbove) : !fitsAbove && (fitsBelow || roomBelow > roomAbove);
  const maxHeight = Math.max(0, Math.min(POPOVER_MAX_HEIGHT_PX, goesBelow ? roomBelow : roomAbove));
  const box: AnchoredBox = { top: goesBelow ? below : anchor.top - gap - Math.min(panel.height, maxHeight), maxHeight };
  if (align === "end") box.right = Math.max(0, view.width - anchor.right);
  else box.left = Math.max(0, Math.min(anchor.left, view.width - panel.width));
  return box;
}

/** The height a panel's content asks for, even while a max-height holds it shorter. */
function naturalHeight(panel: HTMLElement): number {
  return Math.max(panel.offsetHeight, panel.scrollHeight + panel.offsetHeight - panel.clientHeight);
}

// Hidden at the origin, so the first placing reads where the origin is.
const unplaced: CSSProperties = { visibility: "hidden", top: 0, left: 0 };

/**
 * Where a panel goes beside the element it belongs to, as fixed viewport
 * coordinates, so it can be drawn at the end of the document where no
 * scrolling box can cut it off. Recomputed on scroll, resize and a change of
 * its own size while open; the panel applies the maxHeight and scrolls.
 * Hidden until the first measurement so nothing flashes.
 */
export function useAnchored(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  panelRef: RefObject<HTMLElement | null>,
  { align = "start", side = "bottom" }: { align?: "start" | "end"; side?: AnchoredSide } = {},
): CSSProperties {
  const [style, setStyle] = useState<CSSProperties>(unplaced);
  const place = useCallback(() => {
    const anchor = anchorRef.current?.getBoundingClientRect();
    const panel = panelRef.current;
    if (!anchor || !panel) return;
    const size = { width: panel.offsetWidth, height: naturalHeight(panel) };
    const view = { width: window.innerWidth, height: window.innerHeight };
    const box = anchoredBox(anchor, size, view, { align, side });
    // A fixed panel is placed from its containing block, which a landmark with
    // a transform or a filter moves off the window's corner; its origin is
    // where the panel is less where its style puts it.
    const at = panel.getBoundingClientRect();
    const computed = getComputedStyle(panel);
    const originTop = at.top - (parseFloat(computed.top) || 0);
    const originLeft = at.left - (parseFloat(computed.left) || 0);
    const left = box.left ?? view.width - (box.right ?? 0) - size.width;
    setStyle({ top: box.top - originTop, left: left - originLeft, maxHeight: box.maxHeight });
  }, [anchorRef, panelRef, align, side]);
  useLayoutEffect(() => {
    if (!open) {
      setStyle(unplaced);
      return;
    }
    place();
    // Scrolling the panel's own list moves nothing it is placed by.
    const onScroll = (event: Event) => {
      if (!panelRef.current?.contains(event.target as Node)) place();
    };
    const resized = new ResizeObserver(place);
    if (panelRef.current) resized.observe(panelRef.current);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      resized.disconnect();
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open, place, panelRef]);
  return style;
}

/** Scrolls an element into view once it is drawn, so a message lands where the reader is. */
export function useRevealed(ref: RefObject<HTMLElement | null>, active: boolean) {
  useEffect(() => {
    // jsdom has no scrollIntoView.
    if (active) ref.current?.scrollIntoView?.({ block: "nearest" });
  }, [ref, active]);
}

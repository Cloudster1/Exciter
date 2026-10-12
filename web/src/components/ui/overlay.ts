import { useCallback, useEffect, useLayoutEffect, useState, type CSSProperties, type RefObject } from "react";
import { MENU_GAP_PX } from "@/config";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
}

/** Calls onClose on Escape while open. */
export function useEscape(open: boolean, onClose: () => void) {
  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
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
  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement as HTMLElement | null;
    const root = into.current;
    // The close button is first in the DOM but last in intent: focus lands on
    // the first thing the reader came to do.
    const candidates = root ? focusables(root) : [];
    const first = candidates.find((el) => !el.hasAttribute("data-focus-last")) ?? candidates[0] ?? root;
    first?.focus({ preventScroll: true });
    function onKey(event: KeyboardEvent) {
      if (!trap || event.key !== "Tab" || !root) return;
      const items = focusables(root);
      const firstItem = items[0];
      const lastItem = items[items.length - 1];
      if (!firstItem || !lastItem) {
        event.preventDefault();
        return;
      }
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
      previous?.focus({ preventScroll: true });
    };
  }, [open, into, trap]);
}

export type AnchoredSide = "top" | "bottom" | "right";

// Hidden at the origin, so the first placing reads where the origin is.
const hidden: CSSProperties = { visibility: "hidden", top: 0, left: 0 };

/** Fixed viewport coordinates for a panel beside its anchor, so no scrolling box cuts it off: below,
 * above when the bottom is near, or to the right; hidden until measured, so nothing flashes. */
export function useAnchored(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  panelRef: RefObject<HTMLElement | null>,
  { align = "start", side = "bottom" }: { align?: "start" | "end"; side?: AnchoredSide } = {},
): CSSProperties {
  const [style, setStyle] = useState<CSSProperties>(hidden);
  const place = useCallback(() => {
    const anchor = anchorRef.current?.getBoundingClientRect();
    const panel = panelRef.current;
    if (!anchor || !panel) return;
    const gap = MENU_GAP_PX;
    const width = panel.offsetWidth;
    // The whole list, not the part a height cap lets show, decides the side.
    const height = panel.scrollHeight;
    // An ancestor with a transform or a filter moves a fixed panel's containing block off the
    // window's corner; its origin is where the panel is less where its style puts it.
    const at = panel.getBoundingClientRect();
    const computed = getComputedStyle(panel);
    const originTop = at.top - (parseFloat(computed.top) || 0);
    const originLeft = at.left - (parseFloat(computed.left) || 0);
    let top: number;
    let left: number;
    const next: CSSProperties = {};
    if (side === "right") {
      left = Math.min(anchor.right + gap, window.innerWidth - width);
      top = Math.max(0, Math.min(anchor.top + anchor.height / 2 - height / 2, window.innerHeight - height));
    } else {
      const below = anchor.bottom + gap;
      const above = anchor.top - gap - height;
      const goesBelow = side === "bottom" ? below + height <= window.innerHeight || above < 0 : above < 0;
      top = goesBelow ? below : Math.max(0, above);
      // A list taller than the room on its side scrolls within it, or what
      // is past the edge of a phone's screen could never be reached.
      next.maxHeight = goesBelow ? window.innerHeight - below - gap : anchor.top - gap;
      left = align === "end" ? Math.max(0, anchor.right - width) : Math.max(0, Math.min(anchor.left, window.innerWidth - width));
    }
    next.top = top - originTop;
    next.left = left - originLeft;
    setStyle(next);
  }, [anchorRef, panelRef, align, side]);
  useLayoutEffect(() => {
    if (!open) {
      setStyle(hidden);
      return;
    }
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, place]);
  return style;
}

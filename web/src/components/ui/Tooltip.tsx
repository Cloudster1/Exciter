import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { TOOLTIP_DELAY_MS } from "@/config";
import { useAnchored } from "./overlay";

// A tooltip explains, it never names: the thing it sits on has its own label.
// Drawn at the end of the document, so a table's edge cannot cut it off.
export function Tooltip({ text, children, side = "bottom" }: { text: string; children: ReactNode; side?: "top" | "bottom" | "right" }) {
  const [shown, setShown] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const id = useId();
  const anchorRef = useRef<HTMLSpanElement>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  const place = useAnchored(shown, anchorRef, tipRef, { side });
  useEffect(() => () => window.clearTimeout(timer.current), []);
  function show() {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(true), TOOLTIP_DELAY_MS);
  }
  function hide() {
    window.clearTimeout(timer.current);
    setShown(false);
  }
  return (
    <span
      ref={anchorRef}
      className="relative inline-flex"
      onPointerEnter={show}
      onPointerLeave={hide}
      onFocus={show}
      onBlur={hide}
      aria-describedby={shown ? id : undefined}
    >
      {children}
      {shown &&
        createPortal(
          <span
            ref={tipRef}
            role="tooltip"
            id={id}
            style={place}
            className="fixed z-40 rounded-control bg-primary px-2 py-1 text-xs whitespace-nowrap text-on-primary shadow-1"
          >
            {text}
          </span>,
          document.body,
        )}
    </span>
  );
}

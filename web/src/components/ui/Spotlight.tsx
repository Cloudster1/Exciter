import { useEffect, useLayoutEffect, useState } from "react";
import { createPortal } from "react-dom";
import { SPOTLIGHT_GAP_PX, SPOTLIGHT_HEIGHT_PX, SPOTLIGHT_RING_PX, SPOTLIGHT_WAIT_MS, SPOTLIGHT_WIDTH_PX } from "@/config";
import { Button } from "./Button";
import { useEscape } from "./overlay";
import { t } from "@/i18n";

interface Spot {
  top: number;
  left: number;
  ring: { top: number; left: number; width: number; height: number };
}

/** Where the callout sits: under the element, clamped to the viewport. */
function place(element: Element): Spot {
  const box = element.getBoundingClientRect();
  const width = Math.min(SPOTLIGHT_WIDTH_PX, window.innerWidth - SPOTLIGHT_GAP_PX * 2);
  const left = Math.max(SPOTLIGHT_GAP_PX, Math.min(box.left, window.innerWidth - width - SPOTLIGHT_GAP_PX));
  const below = box.bottom + SPOTLIGHT_GAP_PX;
  const fitsBelow = below + SPOTLIGHT_HEIGHT_PX <= window.innerHeight;
  const top = fitsBelow ? below : Math.max(SPOTLIGHT_GAP_PX, box.top - SPOTLIGHT_GAP_PX - SPOTLIGHT_HEIGHT_PX);
  const ring = SPOTLIGHT_RING_PX;
  return { top, left, ring: { top: box.top - ring, left: box.left - ring, width: box.width + ring * 2, height: box.height + ring * 2 } };
}

// A callout that points at one element on the page and says a sentence about
// it; when the element never shows up, onMissing lets the caller say so instead.
export function Spotlight({ target, text, onDone, onMissing }: { target: string; text: string; onDone: () => void; onMissing: () => void }) {
  const [element, setElement] = useState<Element | null>(null);
  const [spot, setSpot] = useState<Spot | null>(null);
  useEscape(true, onDone);

  // The element arrives after a navigation, so the search is patient and bounded.
  useEffect(() => {
    const selector = `[data-guide="${target}"]`;
    const started = Date.now();
    let frame = 0;
    function look() {
      const found = document.querySelector(selector);
      if (found) {
        setElement(found);
        found.scrollIntoView({ block: "nearest" });
        return;
      }
      if (Date.now() - started > SPOTLIGHT_WAIT_MS) {
        onMissing();
        return;
      }
      frame = window.requestAnimationFrame(look);
    }
    look();
    return () => window.cancelAnimationFrame(frame);
  }, [target, onMissing]);

  useLayoutEffect(() => {
    if (!element) return;
    const update = () => setSpot(place(element));
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [element]);

  if (!spot) return null;
  return createPortal(
    <>
      <div aria-hidden className="pointer-events-none fixed z-50 rounded-control ring-2 ring-accent" style={spot.ring} />
      <div
        role="dialog"
        aria-label={t.kit.spotlight}
        className="fixed z-50 rounded-overlay border border-border bg-surface-overlay p-3 shadow-2"
        style={{ top: spot.top, left: spot.left, width: Math.min(SPOTLIGHT_WIDTH_PX, window.innerWidth - SPOTLIGHT_GAP_PX * 2) }}
        data-guide-callout
      >
        <p className="text-sm text-ink">{text}</p>
        <div className="mt-2 flex justify-end">
          <Button size="sm" onClick={onDone} autoFocus>
            {t.kit.gotIt}
          </Button>
        </div>
      </div>
    </>,
    document.body,
  );
}

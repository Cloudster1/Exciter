// Adapted from Stator's features/print/printReady.ts.
import { useEffect } from "react";
import { useIsFetching } from "@tanstack/react-query";
import { PRINT_PENDING_SELECTOR, PRINT_SETTLE_MS } from "@/config";

/** The mark on the root element the render service waits for before it prints. */
export const PRINT_READY_ATTRIBUTE = "data-print-ready";

/** Whether anything under root is still loading or drawing, a picture included. */
export function pendingIn(root: ParentNode): boolean {
  if (root.querySelector(PRINT_PENDING_SELECTOR)) return true;
  return [...root.querySelectorAll("img")].some((img) => !img.complete);
}

/**
 * Pictures below the fold load only when scrolled to, which a print never
 * does, so a print view asks for every one at once.
 */
export function loadEveryPicture(root: ParentNode): void {
  for (const img of root.querySelectorAll('img[loading="lazy"]')) img.setAttribute("loading", "eager");
}

/**
 * Marks the document ready to print once the page has loaded, no query is
 * fetching and nothing under the body has been pending for PRINT_SETTLE_MS.
 */
export function usePrintReady(loaded: boolean): void {
  const fetching = useIsFetching();
  useEffect(() => {
    const html = document.documentElement;
    html.removeAttribute(PRINT_READY_ATTRIBUTE);
    if (!loaded || fetching > 0) return;
    let timer: number | undefined;
    // A change while nothing is pending does not start the wait again, so a
    // page that keeps moving, a clock or an animation, still gets printed.
    const look = () => {
      loadEveryPicture(document.body);
      if (pendingIn(document.body)) {
        window.clearTimeout(timer);
        timer = undefined;
        return;
      }
      timer ??= window.setTimeout(() => {
        timer = undefined;
        if (pendingIn(document.body)) return;
        html.setAttribute(PRINT_READY_ATTRIBUTE, "");
      }, PRINT_SETTLE_MS);
    };
    const observer = new MutationObserver(look);
    observer.observe(document.body, { subtree: true, childList: true, attributes: true });
    // A picture finishing changes no attribute, so its load is heard apart.
    document.addEventListener("load", look, true);
    document.addEventListener("error", look, true);
    look();
    return () => {
      observer.disconnect();
      document.removeEventListener("load", look, true);
      document.removeEventListener("error", look, true);
      window.clearTimeout(timer);
    };
  }, [loaded, fetching]);
}

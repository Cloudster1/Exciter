import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import axe from "axe-core";

// jsdom starts from an empty document, a browser from index.html: its language
// and title are copied in so axe judges the page the reader actually gets.
const shell = new DOMParser().parseFromString(readFileSync(resolve(process.cwd(), "index.html"), "utf8"), "text/html");

/**
 * Runs axe over the whole document, overlays included, and names each violation and where it is.
 * `popupOpen` allows content outside the landmarks, for a popup a test draws without the shell around it.
 */
export async function axeViolations({ popupOpen = false }: { popupOpen?: boolean } = {}): Promise<string[]> {
  document.documentElement.lang = shell.documentElement.lang;
  document.title = shell.title;
  // A modal locks the page's scrolling with overflow: hidden on the body. jsdom
  // lays nothing out, so to axe that clips every overlay to nothing and the
  // open dialog reads as hidden; the lock is lifted while axe looks.
  const locked = document.body.style.overflow;
  document.body.style.overflow = "";
  let result: axe.AxeResults;
  try {
    result = await axe.run(document, {
      rules: {
        // jsdom loads no stylesheet and computes no layout, so contrast cannot
        // be judged here; the browser suite checks it against the real page.
        "color-contrast": { enabled: false },
        region: { enabled: !popupOpen },
      },
    });
  } finally {
    document.body.style.overflow = locked;
  }
  return result.violations.flatMap((violation) => violation.nodes.map((node) => `${violation.id}: ${node.target.join(" ")}`));
}

import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type { BrowserContext } from "@playwright/test";

/** Where each worker notes the addresses its pages showed and the data-action controls they clicked (#334). */
export const REACH_DIR = join(__dirname, "..", "..", "reports", "e2e-reach");

const CLICKED_BINDING = "__exciterReached";

/** Notes, as lines of JSON in one file per worker, what a context's pages reach, for scripts/e2e-reach.mjs. */
export async function noteReach(context: BrowserContext, worker: number): Promise<void> {
  mkdirSync(REACH_DIR, { recursive: true });
  const file = join(REACH_DIR, `worker-${worker}-${process.pid}.jsonl`);
  const note = (entry: { path: string } | { action: string }) => appendFileSync(file, `${JSON.stringify(entry)}\n`);
  context.on("page", (page) => {
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) note({ path: new URL(frame.url()).pathname });
    });
  });
  await context.exposeBinding(CLICKED_BINDING, (_source, action: string) => note({ action }));
  // Whatever locator a spec clicks by, its label or its role, the click lands on the element and bubbles to here.
  await context.addInitScript((binding) => {
    document.addEventListener(
      "click",
      (event) => {
        const control = event.target instanceof Element ? event.target.closest("[data-action]") : null;
        const action = control?.getAttribute("data-action");
        const report = (window as unknown as Record<string, ((action: string) => void) | undefined>)[binding];
        if (action && report) report(action);
      },
      true,
    );
  }, CLICKED_BINDING);
}

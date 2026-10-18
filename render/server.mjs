// Prints a page of the web application as a PDF, for the api to hand back as
// a file. Adapted from Stator's render/server.mjs. It opens only paths under
// its own base address: the api names a path, and the credential the page's
// API calls carry, which never goes into the address.
import { createServer } from "node:http";
import puppeteer from "puppeteer";

const PORT = Number(process.env.RENDER_PORT ?? 8090);
const APP_URL = (process.env.RENDER_APP_URL ?? "http://web").replace(/\/$/, "");
const APP_ORIGIN = new URL(APP_URL).origin;
/**
 * The whole print's longest budget. The api sends what is left of its own
 * deadline, which is shorter, so the api hears a reason, not a cut line.
 */
const RENDER_BUDGET_MS = Number(process.env.RENDER_BUDGET_MS ?? 18_000);
/** Prints at once; one more is refused as busy rather than queued behind a stuck page. */
const RENDER_CONCURRENCY = Number(process.env.RENDER_CONCURRENCY ?? 4);
/** A path and a token are a line each; anything bigger is not one of ours. */
const MAX_BODY_BYTES = 4096;
/** The paper: portrait A4, with room above and below for the running header and footer. */
const PAGE = { format: "A4", landscape: false, printBackground: true, margin: { top: "20mm", right: "14mm", bottom: "18mm", left: "14mm" } };
/** Wide enough for the page's sheet, so the layout is the desktop one. */
const VIEWPORT = { width: 1024, height: 1400, deviceScaleFactor: 1 };
/** The running header and footer's type, in CSS pixels; Chromium's own default is too small to read. */
const MARGIN_FONT_PX = 8;
/** The logo's height in the running header, in CSS pixels. */
const MARGIN_LOGO_PX = 20;
/** Only a picture the page made itself is drawn in a margin; anything else is left out. */
const LOGO_DATA_URL = /^data:image\/png;base64,[A-Za-z0-9+/=]+$/;

/** How long Chromium may take to start; a busy host has taken longer than puppeteer's thirty seconds. */
const LAUNCH_TIMEOUT_MS = Number(process.env.RENDER_LAUNCH_TIMEOUT_MS ?? 120_000);

const browser = await puppeteer.launch({
  timeout: LAUNCH_TIMEOUT_MS,
  executablePath: process.env.RENDER_CHROME ?? "/usr/bin/chromium-browser",
  args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-crash-reporter", "--disable-crashpad", `--user-data-dir=/tmp/chrome-render-${process.pid}`],
});

/** A request the caller can put right, answered with 400 rather than 502. */
class BadRequest extends Error {}
/** A print that ran out of its budget, answered with 504. */
class TooSlow extends Error {}

function requestOf(body) {
  let asked;
  try {
    asked = JSON.parse(body);
  } catch {
    throw new BadRequest("the body is not JSON");
  }
  const path = String(asked?.path ?? "");
  if (!path.startsWith("/") || path.startsWith("//")) throw new BadRequest("a path starts with one slash");
  const token = asked?.token == null ? "" : String(asked.token);
  if (/[\r\n]/.test(token)) throw new BadRequest("a token is one line");
  const language = asked?.language == null ? "" : String(asked.language);
  if (language && !/^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$/.test(language)) throw new BadRequest("a language is a tag such as de or en-GB");
  const budget = Number(asked?.budgetMs);
  return {
    language,
    path,
    token,
    budget: Number.isFinite(budget) && budget > 0 ? Math.min(budget, RENDER_BUDGET_MS) : RENDER_BUDGET_MS,
  };
}

function escapeHTML(text) {
  return String(text ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

/**
 * The running header and footer, from what the page put in its print meta:
 * Chromium draws them outside the page, so they carry their own styles.
 */
function margins(meta) {
  const style = `font-family:${escapeHTML(meta.font || "sans-serif")};font-size:${MARGIN_FONT_PX}px;color:${escapeHTML(meta.color || "#555")};width:100%;margin:0 14mm;display:flex;justify-content:space-between;gap:8mm;`;
  const cell = "white-space:nowrap;overflow:hidden;text-overflow:ellipsis;";
  const pages = escapeHTML(meta.pages || "{page} / {pages}")
    .replace("{page}", '<span class="pageNumber"></span>')
    .replace("{pages}", '<span class="totalPages"></span>');
  const logo = LOGO_DATA_URL.test(meta.logo || "") ? `<img src="${meta.logo}" style="height:${MARGIN_LOGO_PX}px;margin-right:2mm;vertical-align:middle">` : "";
  const brand = meta.brandFooter ? `<span style="${cell}text-align:center;flex:1">${escapeHTML(meta.brandFooter)}</span>` : "";
  return {
    headerTemplate: `<div style="${style}align-items:center"><span style="${cell}">${logo}${escapeHTML(meta.header)}</span><span style="${cell}">${escapeHTML(meta.title)}</span></div>`,
    footerTemplate: `<div style="${style}"><span style="${cell}">${escapeHTML(meta.footer)}</span>${brand}<span style="white-space:nowrap">${pages}</span></div>`,
  };
}

async function print({ path, token, language, budget }, as = "pdf") {
  const deadline = Date.now() + budget;
  // What is left of the budget, so no one step can spend it all.
  const left = () => {
    const remaining = deadline - Date.now();
    if (remaining <= 0) throw new TooSlow(`the page took longer than ${budget}ms to print`);
    return remaining;
  };
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await page.setViewport(VIEWPORT);
    page.setDefaultTimeout(left());
    // The page follows the browser's language when its reader chose none,
    // so the browser prefers the reader's.
    if (language) {
      await page.evaluateOnNewDocument((tag) => {
        Object.defineProperty(navigator, "language", { get: () => tag });
        Object.defineProperty(navigator, "languages", { get: () => [tag] });
      }, language);
    }
    // The page reaches its own origin and nothing else, and only its API
    // calls carry the credential, so no other host ever sees it.
    await page.setRequestInterception(true);
    page.on("request", (request) => {
      const url = request.url();
      if (url.startsWith("data:") || url.startsWith("blob:")) return void request.continue();
      const target = new URL(url);
      if (target.origin !== APP_ORIGIN) return void request.abort("blockedbyclient");
      if (!token || !target.pathname.startsWith("/api/")) return void request.continue();
      return void request.continue({ headers: { ...request.headers(), authorization: `Bearer ${token}` } });
    });
    await page.goto(APP_URL + path, { waitUntil: "domcontentloaded", timeout: left() });
    // The page says when every block has drawn, or why it cannot be printed.
    const settled = await page.waitForSelector("html[data-print-ready], [data-print-failed]", { timeout: left() });
    const failed = await settled.evaluate((el) => (el.hasAttribute("data-print-failed") ? el.textContent || "the page could not be shown" : null));
    if (failed) throw new Error(failed.trim());
    await page.evaluate(() => document.fonts.ready);
    if (as === "html") return Buffer.from(await snapshot(page), "utf8");
    const meta = await page.evaluate(() => {
      const el = document.querySelector("[data-print-meta]");
      const style = getComputedStyle(document.documentElement);
      return {
        ...(el instanceof HTMLElement ? el.dataset : {}),
        color: style.getPropertyValue("--color-ink-muted").trim(),
        font: getComputedStyle(document.body).fontFamily,
      };
    });
    return await page.pdf({ ...PAGE, displayHeaderFooter: true, ...margins(meta), timeout: left() });
  } catch (error) {
    if (error instanceof TooSlow || error?.name === "TimeoutError") throw new TooSlow(error.message);
    throw error;
  } finally {
    await context.close();
  }
}

/**
 * The page as a file that opens without the web client: its stylesheets inlined, its scripts and fonts left out, so
 * nothing it holds reaches back to the server (#214).
 */
async function snapshot(page) {
  return page.evaluate(() => {
    const rules = [];
    for (const sheet of Array.from(document.styleSheets)) {
      try {
        for (const rule of Array.from(sheet.cssRules)) {
          if (!(rule instanceof CSSFontFaceRule)) rules.push(rule.cssText);
        }
      } catch {
        // A sheet of another origin cannot be read, and the page draws nothing from one.
      }
    }
    for (const el of Array.from(document.querySelectorAll("script, link, style, noscript, [data-print-meta]"))) el.remove();
    const style = document.createElement("style");
    style.textContent = rules.join("\n");
    document.head.appendChild(style);
    document.documentElement.removeAttribute("data-print-ready");
    return "<!doctype html>\n" + document.documentElement.outerHTML;
  });
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    let over = false;
    req.on("data", (chunk) => {
      size += chunk.length;
      // Past the cap nothing more is kept, but the rest is read off the socket
      // so the caller hears the refusal rather than a dropped connection.
      if (size > MAX_BODY_BYTES) over = true;
      if (!over) chunks.push(chunk);
    });
    req.on("end", () =>
      over ? reject(new BadRequest(`a body longer than ${MAX_BODY_BYTES} bytes is not a print`)) : resolve(Buffer.concat(chunks).toString("utf8")),
    );
    req.on("error", reject);
  });
}

function answer(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

let printing = 0;

const server = createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/healthz") {
    // The compose healthcheck reads this: without a browser there is nothing to
    // print, so the container says so rather than taking work it cannot do.
    const connected = browser.connected ?? browser.isConnected();
    answer(res, connected ? 200 : 503, { status: connected ? "ok" : "the browser is gone" });
    return;
  }
  if (req.method !== "POST" || (req.url !== "/pdf" && req.url !== "/html")) {
    answer(res, 404, { error: "not found" });
    return;
  }
  let asked;
  try {
    asked = requestOf(await readBody(req));
  } catch (error) {
    const bad = error instanceof BadRequest;
    answer(res, bad ? 400 : 502, { error: error.message });
    return;
  }
  if (printing >= RENDER_CONCURRENCY) {
    answer(res, 503, { error: `${RENDER_CONCURRENCY} pages are being printed already` });
    return;
  }
  printing++;
  try {
    const html = req.url === "/html";
    const answered = await print(asked, html ? "html" : "pdf");
    res.writeHead(200, { "Content-Type": html ? "text/html; charset=utf-8" : "application/pdf", "Content-Length": answered.length });
    res.end(answered);
  } catch (error) {
    // The path may hold a public link's token, so only the reason is logged.
    process.stderr.write(`render failed: ${error.message}\n`);
    answer(res, error instanceof TooSlow ? 504 : 502, { error: error.message });
  } finally {
    printing--;
  }
});

server.listen(PORT, "0.0.0.0", () => process.stdout.write(`render service on :${PORT}, printing ${APP_URL}\n`));
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, async () => {
    server.close();
    await browser.close();
    process.exit(0);
  });
}

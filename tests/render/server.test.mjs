// Tests for render/server.mjs, run by make test-render with node --test in the render image: the real server and
// its Chromium against a page server of the test's own, and a second origin the page must never reach.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { createServer } from "node:http";
import { after, before, describe, it } from "node:test";

const SERVER = process.env.RENDER_SERVER ?? "/usr/src/app/render/server.mjs";
/** How long Chromium may take to start on a busy runner. */
const START_TIMEOUT_MS = 120_000;
/** The budget of a print that is meant to run out. */
const SHORT_BUDGET_MS = 1_500;
/** How long the browser's going may take to show on /healthz. */
const GONE_WAIT_MS = 15_000;
/** How long a print may take to open its page. */
const PAGE_WAIT_MS = 15_000;
const POLL_MS = 100;
const TOKEN = "render-test-token";

/** A server on a free port of the loopback, which keeps what it was asked. */
async function listen(handler) {
  const asked = [];
  const server = createServer((req, res) => {
    asked.push({ path: req.url, authorization: req.headers.authorization });
    handler(req, res);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { server, asked, origin: `http://127.0.0.1:${server.address().port}` };
}

const freePort = async () => {
  const { server } = await listen(() => {});
  const { port } = server.address();
  await new Promise((resolve) => server.close(resolve));
  return port;
};

let app;
let elsewhere;
let render;
let base;
let output = "";

/** The page that prints: it calls its own API, a path outside it, and another origin, then says it is ready. */
const readyPage = (other) => `<!doctype html><html><head><title>Ready</title></head><body><p>Printed</p>
<img src="${other}/pixel.png">
<script>
Promise.allSettled([fetch("/api/data"), fetch("/assets/app.json"), fetch("${other}/api/data")])
  .then(() => document.documentElement.setAttribute("data-print-ready", ""));
</script></body></html>`;

before(async () => {
  elsewhere = await listen((_req, res) => res.end("{}"));
  app = await listen((req, res) => {
    if (req.url === "/ready") return void res.end(readyPage(elsewhere.origin));
    // A page that never says it is ready, so its print runs until its budget does.
    if (req.url === "/never") return void res.end("<!doctype html><html><body>Waiting</body></html>");
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end("{}");
  });
  const port = await freePort();
  base = `http://127.0.0.1:${port}`;
  render = spawn(process.execPath, [SERVER], {
    env: { ...process.env, HOME: "/tmp", RENDER_PORT: String(port), RENDER_APP_URL: app.origin, RENDER_CONCURRENCY: "1" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const started = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`the render service did not start within ${START_TIMEOUT_MS}ms:\n${output}`)), START_TIMEOUT_MS);
    const seen = (chunk) => {
      output += chunk;
      if (output.includes("render service on")) {
        clearTimeout(timer);
        resolve();
      }
    };
    render.stdout.on("data", seen);
    render.stderr.on("data", (chunk) => (output += chunk));
    render.on("exit", (code) => reject(new Error(`the render service exited with ${code}:\n${output}`)));
  });
  await started;
});

after(() => {
  render?.kill("SIGKILL");
  app?.server.close();
  elsewhere?.server.close();
});

const ask = (path, body) =>
  fetch(base + path, { method: "POST", headers: { "Content-Type": "application/json" }, body: typeof body === "string" ? body : JSON.stringify(body) });

const refusal = async (response) => ({ status: response.status, error: (await response.json()).error });

describe("what the render service refuses", () => {
  for (const [what, body, error] of [
    ["a body that is not JSON", "not json", "the body is not JSON"],
    ["a path to another host", { path: "//evil.example/x" }, "a path starts with one slash"],
    ["a path without its slash", { path: "ready" }, "a path starts with one slash"],
    ["a token of two lines", { path: "/ready", token: "a\nAuthorization: Bearer other" }, "a token is one line"],
    ["a language that is no tag", { path: "/ready", language: "en_GB; drop" }, "a language is a tag such as de or en-GB"],
    ["a body past its cap", { path: `/${"x".repeat(5000)}` }, "a body longer than 4096 bytes is not a print"],
  ]) {
    it(`answers 400 to ${what}`, async () => {
      assert.deepEqual(await refusal(await ask("/pdf", body)), { status: 400, error });
    });
  }

  it("asks the page server nothing for a refused print", () => {
    assert.deepEqual(app.asked, []);
  });

  it("answers 404 to anything but a print or its health", async () => {
    assert.equal((await fetch(`${base}/pdf`)).status, 404);
    assert.equal((await ask("/png", { path: "/ready" })).status, 404);
  });
});

describe("a print", () => {
  it("is a PDF of the page, whose API calls alone carry the token, and which reaches no other origin", async () => {
    app.asked.length = 0;
    const response = await ask("/pdf", { path: "/ready", token: TOKEN });
    assert.equal(response.status, 200, output);
    assert.equal(response.headers.get("content-type"), "application/pdf");
    assert.equal(Buffer.from(await response.arrayBuffer()).subarray(0, 4).toString(), "%PDF");

    const by = (path) => app.asked.filter((r) => r.path === path);
    assert.equal(by("/ready").length, 1, "the page itself was not opened once");
    assert.equal(by("/ready")[0].authorization, undefined, "the page itself was sent the token");
    assert.deepEqual(
      by("/api/data").map((r) => r.authorization),
      [`Bearer ${TOKEN}`],
    );
    assert.deepEqual(
      by("/assets/app.json").map((r) => r.authorization),
      [undefined],
    );
    assert.deepEqual(elsewhere.asked, [], "the page reached another origin");
  });

  it("past its budget answers 504", async () => {
    const { status, error } = await refusal(await ask("/pdf", { path: "/never", budgetMs: SHORT_BUDGET_MS }));
    assert.equal(status, 504);
    assert.match(error, /\S/);
  });

  it("beyond RENDER_CONCURRENCY answers 503 at once, while the first goes on", async () => {
    app.asked.length = 0;
    const first = ask("/pdf", { path: "/never", budgetMs: SHORT_BUDGET_MS * 2 });
    const opened = Date.now() + PAGE_WAIT_MS;
    while (!app.asked.some((r) => r.path === "/never")) {
      assert.ok(Date.now() < opened, `the first print did not open its page within ${PAGE_WAIT_MS}ms`);
      await new Promise((resolve) => setTimeout(resolve, POLL_MS));
    }
    assert.deepEqual(await refusal(await ask("/pdf", { path: "/ready" })), { status: 503, error: "1 pages are being printed already" });
    assert.equal((await first).status, 504);
    assert.equal((await ask("/pdf", { path: "/ready" })).status, 200, "the slot was not given back");
  });
});

describe("its health", () => {
  it("is 200 with the browser, and 503 once the browser has gone", async () => {
    const health = async () => {
      const response = await fetch(`${base}/healthz`);
      return { status: response.status, body: await response.json() };
    };
    assert.deepEqual(await health(), { status: 200, body: { status: "ok" } });

    // Chromium runs with a profile named after the server's process, which picks out its processes alone.
    const profile = `chrome-render-${render.pid}`;
    let killed = 0;
    for (const pid of readdirSync("/proc").filter((p) => /^\d+$/.test(p))) {
      let cmdline = "";
      try {
        cmdline = readFileSync(`/proc/${pid}/cmdline`, "utf8");
      } catch {
        continue;
      }
      if (!cmdline.includes(profile)) continue;
      try {
        process.kill(Number(pid), "SIGKILL");
        killed++;
      } catch {
        // Gone already, with the rest of its tree.
      }
    }
    assert.ok(killed > 0, `no Chromium process ran with ${profile}`);

    const deadline = Date.now() + GONE_WAIT_MS;
    let last = await health();
    while (last.status !== 503 && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, POLL_MS));
      last = await health();
    }
    assert.deepEqual(last, { status: 503, body: { status: "the browser is gone" } });
  });
});

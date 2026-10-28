// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { readFileSync } from "node:fs";
import { availableParallelism, loadavg } from "node:os";
import { fileURLToPath, URL } from "node:url";

// The root VERSION file is the one release.sh bumps, so the client reads it
// rather than keeping a second copy in package.json.
const version = readFileSync(fileURLToPath(new URL("../VERSION", import.meta.url)), "utf8").trim();

// The port inside the container; make web-dev publishes it on a port of the checkout's own.
const DEV_SERVER_PORT = 5173;

// Gates in parallel worktrees share one machine, so a run takes at most a
// share of the cores, and only those not already busy.
const TEST_CORE_SHARE = 0.5;
const TEST_MIN_WORKERS = 2;
const cores = availableParallelism();
const [busyCores = 0] = loadavg();
const idleCores = cores - Math.round(busyCores);
const testWorkers = Math.max(TEST_MIN_WORKERS, Math.min(Math.floor(cores * TEST_CORE_SHARE), idleCores));
// Measuring coverage slows every test, and gates beside each other slow them again; vitest's five seconds then
// ended tests that pass in one.
const TEST_TIMEOUT_MS = 15_000;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "0.0.0.0",
    port: DEV_SERVER_PORT,
    // Vite rejects requests whose Host header it does not recognise, which
    // blocks anything reaching the dev server by its compose service name.
    allowedHosts: ["localhost", "127.0.0.1", "web"],
    // One origin for the browser, so the session cookie is first-party and
    // no CORS preflight is involved.
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY || "http://localhost:8080",
        changeOrigin: true,
      },
      "/.well-known/oauth-": {
        target: process.env.VITE_API_PROXY || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    maxWorkers: testWorkers,
    testTimeout: TEST_TIMEOUT_MS,
    // Measured when asked with --coverage, as check-web does. The string tables and the constants hold no logic
    // and would lift the figure without anything being tested; the schema is generated.
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/i18n/**", "src/config.ts", "src/api/schema.d.ts", "src/test/**", "src/**/*.test.{ts,tsx}", "src/**/*.d.ts"],
      reporter: ["text-summary", "json-summary"],
      reportsDirectory: "../reports/vitest-coverage",
    },
  },
});

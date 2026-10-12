import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";

import { LocalizedRouter } from "./features/shell/LocalizedRouter";
import { createQueryClient } from "./lib/session";
import { applyCustomTheme, applyTheme, readCachedTheme, readTheme } from "./lib/theme";
import { buildRouter, sendToLogin } from "./routes";
import "./styles/index.css";

// The stored theme, and the custom one this browser saw last, go on before the first paint, so there is no flash
// of the wrong palette; the loader replaces the custom one once the server has answered.
applyTheme(readTheme());
const cached = readCachedTheme();
if (cached) applyCustomTheme(cached.css);

// The client and the router need each other: a 401 anywhere navigates, which
// only ever happens once both exist.
const queryClient = createQueryClient((client) => sendToLogin(router, client));
const router = buildRouter(queryClient);

const container = document.getElementById("root");
if (!container) throw new Error("root element is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <LocalizedRouter router={router} />
    </QueryClientProvider>
  </StrictMode>,
);

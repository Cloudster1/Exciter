import { describe, expect, it } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { ApiError } from "@/api/client";
import { useMutation } from "@/api/mutation";
import { ToastProvider } from "@/components/ui";
import { en } from "@/i18n/en";
import { createQueryClient } from "@/lib/session";
import { MutationErrorToasts } from "./MutationErrorToasts";

const refused = "Archive the project's requirements before archiving the project.";
const refuse = () => Promise.reject(new ApiError(409, { code: "conflict", message: refused }));

/** Under the same query client and toasts the router's root gives every page. */
function renderInShell(ui: ReactElement) {
  return render(
    <QueryClientProvider client={createQueryClient(() => {})}>
      <ToastProvider>
        <MutationErrorToasts />
        {ui}
      </ToastProvider>
    </QueryClientProvider>,
  );
}

/** A change that fails, and shows nothing about it unless told to. */
function Change({ run = refuse, handlesError = false, onError }: { run?: () => Promise<unknown>; handlesError?: boolean; onError?: () => void }) {
  const change = useMutation({ mutationFn: run, meta: handlesError ? { handlesError } : undefined });
  return (
    <>
      <button type="button" onClick={() => change.mutate(undefined, onError ? { onError } : undefined)}>
        Change
      </button>
      <span data-testid="status">{change.status}</span>
    </>
  );
}

/** The same change, drawing its own error the way a form's banner does. */
function ChangeShowingError() {
  const change = useMutation({ mutationFn: refuse });
  return (
    <>
      <button type="button" onClick={() => change.mutate()}>
        Change
      </button>
      {change.error && <p>{change.error.message}</p>}
    </>
  );
}

async function failOnce() {
  fireEvent.click(screen.getByRole("button", { name: "Change" }));
  await screen.findByText("error");
  await act(() => new Promise((resolve) => setTimeout(resolve)));
}

const toasts = () => document.querySelectorAll("[data-toast]");

describe("a failed change nobody else reports", () => {
  it("is said in a toast, in the server's words", async () => {
    renderInShell(<Change />);
    await failOnce();
    expect(screen.getByRole("alert")).toHaveTextContent(refused);
    expect(toasts()).toHaveLength(1);
  });

  it("says what to do when the server gave no sentence", async () => {
    renderInShell(<Change run={() => Promise.reject(new TypeError("Failed to fetch"))} />);
    await failOnce();
    expect(screen.getByRole("alert")).toHaveTextContent(en.api.unsaved);
  });

  it("is left to signing in again when the session is gone", async () => {
    renderInShell(<Change run={() => Promise.reject(new ApiError(401, { code: "unauthorized", message: "Sign in to continue." }))} />);
    await failOnce();
    expect(toasts()).toHaveLength(0);
  });
});

describe("a failed change that reports itself", () => {
  it("is left to a mutation that says it handles its errors", async () => {
    renderInShell(<Change handlesError />);
    await failOnce();
    expect(toasts()).toHaveLength(0);
  });

  it("is left to a caller that passes its own onError", async () => {
    let told = 0;
    renderInShell(<Change onError={() => (told += 1)} />);
    await failOnce();
    expect(told).toBe(1);
    expect(toasts()).toHaveLength(0);
  });

  it("is said once by a component that draws the error itself", async () => {
    renderInShell(<ChangeShowingError />);
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    await screen.findByText(refused);
    await act(() => new Promise((resolve) => setTimeout(resolve)));
    expect(screen.getAllByText(refused)).toHaveLength(1);
    expect(toasts()).toHaveLength(0);
  });
});

// A mutation built straight on TanStack's hook would never tell the toast that
// its caller shows the error, and would be said twice.
describe("every mutation", () => {
  it("is built on the hook that knows whether its caller shows the error", () => {
    const sources = import.meta.glob<string>(["/src/**/*.{ts,tsx}", "!/src/**/*.test.{ts,tsx}"], { query: "?raw", import: "default", eager: true });
    expect(Object.keys(sources)).toContain("/src/api/requirements.ts");
    const direct = Object.entries(sources)
      .filter(([path]) => path !== "/src/api/mutation.ts")
      .filter(([, source]) => /import\s*\{[^}]*\buseMutation\b[^}]*\}\s*from\s*"@tanstack\/react-query"/.test(source))
      .map(([path]) => path);
    expect(direct).toEqual([]);
  });
});

describe("the browser's confirm", () => {
  it("is used nowhere; a deletion asks in the kit's ConfirmDialog", () => {
    const sources = import.meta.glob<string>(["/src/**/*.{ts,tsx}", "!/src/**/*.test.{ts,tsx}"], { query: "?raw", import: "default", eager: true });
    const asking = Object.entries(sources)
      .filter(([, source]) => /\bwindow\.confirm\s*\(/.test(source))
      .map(([path]) => path);
    expect(asking).toEqual([]);
  });
});

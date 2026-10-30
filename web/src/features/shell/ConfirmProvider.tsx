import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { ConfirmDialog } from "@/components/ui";

export interface ConfirmRequest {
  /** The thing that goes, as the button will name it: "team", "label Billing". */
  noun: string;
  verb?: string;
  body: ReactNode;
}

type Ask = (request: ConfirmRequest) => Promise<boolean>;

const ConfirmContext = createContext<Ask | null>(null);

/** Asks before something is deleted; resolves true when the reader said so. */
export function useConfirm(): Ask {
  const ask = useContext(ConfirmContext);
  if (!ask) throw new Error("useConfirm needs a ConfirmProvider above it");
  return ask;
}

// One dialog for the whole shell, handed out as a promise, so a delete button
// reads "if (await confirm(...)) remove.mutate(id)" and nothing else changes.
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [request, setRequest] = useState<ConfirmRequest | null>(null);
  const resolver = useRef<((ok: boolean) => void) | null>(null);

  const ask = useCallback<Ask>((next) => {
    resolver.current?.(false);
    setRequest(next);
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const answer = useCallback((ok: boolean) => {
    resolver.current?.(ok);
    resolver.current = null;
    setRequest(null);
  }, []);

  const value = useMemo(() => ask, [ask]);
  return (
    <ConfirmContext.Provider value={value}>
      {children}
      <ConfirmDialog
        open={request !== null}
        onClose={() => answer(false)}
        onConfirm={() => answer(true)}
        noun={request?.noun ?? ""}
        verb={request?.verb}
        body={request?.body}
      />
    </ConfirmContext.Provider>
  );
}

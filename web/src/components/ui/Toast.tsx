import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type RefObject,
  type ReactNode,
  type SetStateAction,
} from "react";
import { TOAST_MAX, TOAST_MS } from "@/config";
import { Icon } from "../icons";
import { IconButton } from "./Button";
import { cx } from "./cx";
import { t } from "@/i18n";

export type ToastKind = "success" | "error" | "info";

export interface ToastOptions {
  /** A link to what was made; rendered by the caller so the router's typing is kept. */
  link?: ReactNode;
  /** An undo or a retry, one button. */
  action?: { label: string; onClick: () => void };
}

interface ToastEntry extends ToastOptions {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastApi {
  success: (message: string, options?: ToastOptions) => void;
  error: (message: string, options?: ToastOptions) => void;
  info: (message: string, options?: ToastOptions) => void;
  dismiss: (id: number) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

/** Says what just happened without stopping anything; a toast never asks a question. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) throw new Error("useToast needs a ToastProvider above it");
  return api;
}

interface Shelf {
  toasts: ToastEntry[];
  setToasts: Dispatch<SetStateAction<ToastEntry[]>>;
  counter: RefObject<number>;
}

const ShelfContext = createContext<Shelf | null>(null);

/**
 * Holds the toasts above the router, so a page drawn again in another language
 * keeps what it just said. The provider inside the router still draws them.
 */
export function ToastShelf({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const counter = useRef(0);
  const shelf = useMemo<Shelf>(() => ({ toasts, setToasts, counter }), [toasts]);
  return <ShelfContext.Provider value={shelf}>{children}</ShelfContext.Provider>;
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const shelf = useContext(ShelfContext);
  const [ownToasts, setOwnToasts] = useState<ToastEntry[]>([]);
  const ownCounter = useRef(0);
  const toasts = shelf?.toasts ?? ownToasts;
  const setToasts = shelf?.setToasts ?? setOwnToasts;
  const counter = shelf?.counter ?? ownCounter;

  const dismiss = useCallback((id: number) => setToasts((all) => all.filter((t) => t.id !== id)), [setToasts]);
  const push = useCallback(
    (kind: ToastKind, message: string, options?: ToastOptions) => {
      const id = ++counter.current;
      setToasts((all) => [...all, { id, kind, message, ...options }].slice(-TOAST_MAX));
      window.setTimeout(() => dismiss(id), TOAST_MS);
    },
    [dismiss, counter, setToasts],
  );
  const api = useMemo<ToastApi>(
    () => ({
      success: (m, o) => push("success", m, o),
      error: (m, o) => push("error", m, o),
      info: (m, o) => push("info", m, o),
      dismiss,
    }),
    [push, dismiss],
  );

  // Escape dismisses the newest, so a keyboard user can clear the corner.
  useEffect(() => {
    if (toasts.length === 0) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") dismiss(toasts[toasts.length - 1]!.id);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [toasts, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-80 flex-col gap-2" data-toasts>
        {toasts.map((toast) => (
          <div
            key={toast.id}
            role={toast.kind === "error" ? "alert" : "status"}
            aria-live={toast.kind === "error" ? "assertive" : "polite"}
            data-toast={toast.kind}
            className={cx(
              "pointer-events-auto flex items-start gap-2 rounded-overlay border bg-surface-overlay px-3 py-2 text-sm shadow-2",
              toast.kind === "error" ? "border-danger/40" : toast.kind === "success" ? "border-success/40" : "border-border",
            )}
          >
            <span className={cx("mt-0.5 shrink-0", toast.kind === "error" ? "text-danger" : toast.kind === "success" ? "text-success" : "text-ink-muted")}>
              {toast.kind === "error" ? <Icon.Warning /> : toast.kind === "success" ? <Icon.Check /> : <Icon.Info />}
            </span>
            <span className="min-w-0 flex-1 text-ink" data-toast-message>
              {toast.message}
              {toast.link && <span className="ml-1.5">{toast.link}</span>}
            </span>
            {toast.action && (
              <button
                type="button"
                onClick={() => {
                  toast.action?.onClick();
                  dismiss(toast.id);
                }}
                className="shrink-0 font-medium text-accent hover:underline"
              >
                {toast.action.label}
              </button>
            )}
            <IconButton
              icon={<Icon.X />}
              label={t.kit.dismiss}
              size="sm"
              onClick={() => dismiss(toast.id)}
              className="-my-1 -mr-1"
              data-action="dismiss-toast"
            />
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

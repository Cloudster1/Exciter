import { useRef, type ReactNode } from "react";
import { Button } from "./Button";
import { useRevealed } from "./overlay";
import { t } from "@/i18n";

/**
 * A non-blocking error banner, used for whole-form failures. With reveal it
 * scrolls itself into view when it appears, for a form whose body scrolls.
 */
export function ErrorBanner({ children, onRetry, reveal = false }: { children: ReactNode; onRetry?: () => void; reveal?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useRevealed(ref, reveal);
  return (
    <div ref={ref} role="alert" className="flex items-start gap-3 rounded-control border border-danger/30 bg-danger-subtle px-3 py-2 text-sm text-danger">
      <span className="min-w-0 flex-1">{children}</span>
      {onRetry && (
        <Button size="sm" variant="ghost" onClick={onRetry} className="-my-1 text-danger hover:text-danger">
          {t.kit.retry}
        </Button>
      )}
    </div>
  );
}

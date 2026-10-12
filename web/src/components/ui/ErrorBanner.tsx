import type { ReactNode } from "react";
import { t } from "@/i18n";
import { Button } from "./Button";

/** A non-blocking error banner, used for whole-form failures. */
export function ErrorBanner({ children, onRetry }: { children: ReactNode; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex items-start gap-3 rounded-control border border-danger/30 bg-danger-subtle px-3 py-2 text-sm text-danger">
      <span className="min-w-0 flex-1">{children}</span>
      {onRetry && (
        <Button size="sm" variant="ghost" onClick={onRetry} className="-my-1 text-danger hover:text-danger">
          {t.common.retry}
        </Button>
      )}
    </div>
  );
}

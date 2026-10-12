import type { ReactNode } from "react";

/** Where a first-time visitor reads what a page is for: the noun, a sentence, one action. */
export function EmptyState({ title, description, action, icon }: { title: string; description?: string; action?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-1.5 rounded-overlay border border-dashed border-border-strong/60 px-6 py-14 text-center">
      {icon && <div className="mb-2 text-ink-subtle [&_svg]:size-6">{icon}</div>}
      <p className="font-medium text-ink">{title}</p>
      {description && <p className="max-w-sm text-sm text-ink-muted">{description}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

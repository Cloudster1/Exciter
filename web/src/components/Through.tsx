import type { components } from "@/api/schema";
import { cx } from "@/components/ui";
import { t } from "@/i18n";

/** What something was written through: a personal access token or a connected app, by its name then (#190). */
export type Via = components["schemas"]["Via"];

/** Says beside the person that it came through a token or a connected app; nothing for what they wrote themselves. */
export function Through({ via, className }: { via: Via | null | undefined; className?: string }) {
  if (!via) return null;
  return (
    <span className={cx("text-xs text-ink-muted", className)} title={t.via.kinds[via.kind]} data-via={via.kind}>
      {t.via.through(via.name)}
      <span className="sr-only"> ({t.via.kinds[via.kind]})</span>
    </span>
  );
}

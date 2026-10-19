import type { RiskLevelRef } from "@/api/risks";
import { t } from "@/i18n";

/** A risk level by its name and colour, or that there is none yet. */
export function LevelTag({ level }: { level: Pick<RiskLevelRef, "name" | "colour"> | null | undefined }) {
  if (!level) return <span className="text-sm text-ink-muted">{t.risks.noLevel}</span>;
  return (
    <span className="inline-flex items-center gap-1.5 text-sm font-medium text-ink" data-risk-level-tag={level.name}>
      <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: level.colour }} />
      {level.name}
    </span>
  );
}

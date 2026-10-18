import { Tag, cx } from "@/components/ui";
import { QUALITY_FAIR_FROM, QUALITY_GOOD_FROM } from "@/config";
import { t } from "@/i18n";

/** The band a score falls in, which picks its colour; the number is always written beside it. */
export function scoreBand(score: number): "good" | "fair" | "poor" {
  return score >= QUALITY_GOOD_FROM ? "good" : score >= QUALITY_FAIR_FROM ? "fair" : "poor";
}

const tone = {
  good: "border-success bg-success-subtle text-ink",
  fair: "border-warning bg-warning-subtle text-ink",
  poor: "border-danger bg-danger-subtle text-ink",
};

/** A requirement's score against the writing rules (#207): not checked yet when it has none, and said so when stale. */
export function QualityBadge({ score, stale = false, className }: { score: number | null; stale?: boolean; className?: string }) {
  const l = t.quality;
  if (score === null) {
    return (
      <Tag className={className} data-quality-badge="none">
        {l.notChecked}
      </Tag>
    );
  }
  return (
    <Tag
      className={cx(tone[scoreBand(score)], "tabular-nums", className)}
      title={stale ? l.staleHint : l.badgeHint(score)}
      data-quality-badge={score}
      data-stale={stale || undefined}
    >
      {l.badge(score)}
      {stale && <span className="ml-1 font-normal">({l.stale})</span>}
    </Tag>
  );
}

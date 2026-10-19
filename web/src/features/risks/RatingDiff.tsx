import type { components } from "@/api/schema";
import type { Dimension } from "@/api/risks";
import { Card, SectionTitle } from "@/components/ui";
import { t } from "@/i18n";

type Comparison = components["schemas"]["Comparison"];

/** What changed in a risk's rating between two revisions, value by value. */
export function RatingDiff({ comparison }: { comparison: Comparison }) {
  const changes = comparison.risk ?? [];
  if (changes.length === 0) return null;
  return (
    <Card className="space-y-2 p-4" data-diff-of="risk">
      <SectionTitle>{t.risks.diffTitle}</SectionTitle>
      <ul className="space-y-1 text-sm text-ink">
        {changes.map((c, i) => (
          <li key={i} data-risk-change={`${c.side}.${c.dimension}`}>
            <span className="font-medium">{t.risks.value(c.dimension as Dimension, c.side as "before" | "after")}:</span> {t.risks.diff(c.from, c.to)}
          </li>
        ))}
      </ul>
    </Card>
  );
}

import { useId } from "react";
import type { RiskView } from "@/api/risks";
import { SectionTitle, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { LevelTag } from "./LevelTag";

/** A risk's rating, read: each side's values, priority number and level. In a document it takes a label rather than
 * a heading. */
export function RatingView({ view, small = false }: { view: RiskView; small?: boolean }) {
  const id = useId();
  const detection = view.before.detection != null || view.after.detection != null;
  const dimensions = detection ? (["severity", "probability", "detection"] as const) : (["severity", "probability"] as const);
  const sides = [
    { side: "before", values: view.before, rpn: view.beforeRpn, level: view.beforeLevel },
    { side: "after", values: view.after, rpn: view.afterRpn, level: view.afterLevel },
  ] as const;
  return (
    <section aria-labelledby={id} className="space-y-1" data-risk-view="">
      {small ? (
        <p id={id} className="text-xs font-semibold text-ink-muted uppercase">
          {t.risks.rating}
        </p>
      ) : (
        <SectionTitle id={id}>{t.risks.rating}</SectionTitle>
      )}
      <Table dense aria-labelledby={id}>
        <thead>
          <tr>
            <Th>
              <span className="sr-only">{t.risks.rating}</span>
            </Th>
            {dimensions.map((d) => (
              <Th key={d}>{t.risks.dimensions[d]}</Th>
            ))}
            <Th>{t.risks.rpn}</Th>
            <Th>{t.risks.level}</Th>
          </tr>
        </thead>
        <tbody>
          {sides.map(({ side, values, rpn, level }) => (
            <tr key={side} data-rating-side={side}>
              <Th scope="row">{t.risks.sides[side]}</Th>
              {dimensions.map((d) => (
                <Td key={d}>{values[d] ?? "-"}</Td>
              ))}
              <Td>{rpn ?? "-"}</Td>
              <Td>
                <LevelTag level={level} />
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </section>
  );
}

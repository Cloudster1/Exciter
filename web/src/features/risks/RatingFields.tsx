import { useId } from "react";
import { useRiskScheme, type Dimension, type Rating, type RatingSide } from "@/api/risks";
import { SelectInput, Skeleton, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { LevelTag } from "./LevelTag";
import { levelOf, rpnOf } from "./level";

const SIDES = ["before", "after"] as const;

/** A risk's rating as the editor writes it (#259): a value of each scale before and after mitigation, with the
 * priority number and level each side gives as it is picked. */
export function RatingFields({ projectKey, value, onChange, error }: { projectKey: string; value: Rating; onChange: (next: Rating) => void; error?: string }) {
  const { data: scheme } = useRiskScheme(projectKey);
  const id = useId();
  if (scheme === undefined) return <Skeleton />;
  if (scheme === null) {
    return (
      <p role="status" className="text-sm text-ink-muted" data-risk-needs-scheme="">
        {t.risks.needsScheme}
      </p>
    );
  }
  const scales = scheme.scales;
  const set = (side: (typeof SIDES)[number], dimension: Dimension, picked: string) => {
    const next: RatingSide = { ...value[side], [dimension]: picked === "" ? null : Number(picked) };
    onChange({ ...value, [side]: next });
  };
  return (
    <fieldset className="space-y-2" aria-describedby={`${id}-hint`} data-risk-rating="">
      <legend className="text-sm font-semibold text-ink">{t.risks.rating}</legend>
      <p id={`${id}-hint`} className="text-xs text-ink-muted">
        {t.risks.ratingHint}
      </p>
      <Table dense aria-label={t.risks.rating}>
        <thead>
          <tr>
            <Th>
              <span className="sr-only">{t.risks.rating}</span>
            </Th>
            {scales.map((s) => (
              <Th key={s.dimension}>{s.label}</Th>
            ))}
            <Th>{t.risks.rpn}</Th>
            <Th>{t.risks.level}</Th>
          </tr>
        </thead>
        <tbody>
          {SIDES.map((side) => (
            <tr key={side} data-rating-side={side}>
              <Th scope="row">{t.risks.sides[side]}</Th>
              {scales.map((s) => (
                <Td key={s.dimension}>
                  <SelectInput
                    aria-label={t.risks.value(s.dimension, side)}
                    controlSize="sm"
                    value={value[side][s.dimension] ?? ""}
                    onChange={(event) => set(side, s.dimension, event.target.value)}
                  >
                    <option value="">{t.risks.unknown}</option>
                    {s.values.map((v) => (
                      <option key={v.value} value={v.value}>
                        {v.value} {v.name}
                      </option>
                    ))}
                  </SelectInput>
                </Td>
              ))}
              <Td>{rpnOf(scheme, value[side]) ?? "-"}</Td>
              <Td>
                <LevelTag level={levelOf(scheme, value[side])} />
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
      {error && (
        <p className="text-sm text-danger" role="alert">
          {error}
        </p>
      )}
    </fieldset>
  );
}

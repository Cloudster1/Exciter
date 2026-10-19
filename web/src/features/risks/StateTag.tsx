import type { RiskState } from "@/api/risks";
import { Tag } from "@/components/ui";
import { t } from "@/i18n";

/** How far a risk is mitigated, with what that means in its title. */
export function StateTag({ state }: { state: RiskState | null | undefined }) {
  if (!state) return null;
  return (
    <Tag title={t.risks.stateHints[state]} data-risk-state={state}>
      {t.risks.states[state]}
    </Tag>
  );
}

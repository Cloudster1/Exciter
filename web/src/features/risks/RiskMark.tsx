import type { components } from "@/api/schema";
import { t } from "@/i18n";
import { StateTag } from "./StateTag";

type Mark = components["schemas"]["RiskMark"];

/** A risk's mark in the trace graph and the chain report (#261): the level it stands at, in its colour, and its state. */
export function RiskMark({ mark }: { mark: Mark | null | undefined }) {
  if (!mark) return null;
  return (
    <span className="inline-flex items-center gap-1.5" data-risk-mark={mark.level?.name ?? ""}>
      {mark.level ? (
        <span className="inline-flex items-center gap-1 text-xs text-ink" title={t.risks.level}>
          <span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: mark.level.colour }} />
          {mark.level.name}
        </span>
      ) : (
        <span className="text-xs text-ink-muted">{t.risks.noLevel}</span>
      )}
      <StateTag state={mark.state} />
    </span>
  );
}

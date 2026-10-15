import type { ReqIFLoss } from "@/api/reqif";
import { Card } from "@/components/ui";
import { t } from "@/i18n";

/** What the file holds that the project cannot keep, which does not keep the import from starting. */
export function LossList({ losses, lostCount }: { losses: ReqIFLoss[]; lostCount: number }) {
  return (
    <Card className="space-y-2 p-4" data-reqif-losses={lostCount}>
      <h3 className="text-sm font-medium text-ink">{t.reqif.lossesTitle}</h3>
      {losses.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.reqif.noLosses}</p>
      ) : (
        <ul className="space-y-1 text-sm text-ink">
          {losses.map((loss, i) => (
            <li key={i} className="flex flex-wrap gap-x-2">
              <span className="font-medium text-ink-muted">{t.reqif.lossKinds[loss.kind]}</span>
              {loss.where && <span>{loss.where}:</span>}
              <span>{loss.message}</span>
            </li>
          ))}
        </ul>
      )}
      {lostCount > losses.length && <p className="text-sm text-ink-muted">{t.reqif.moreLosses(lostCount - losses.length)}</p>}
    </Card>
  );
}

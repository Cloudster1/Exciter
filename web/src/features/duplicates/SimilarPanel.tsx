import { useSimilar } from "@/api/duplicates";
import type { Requirement } from "@/api/requirements";
import { Card, ErrorBanner, SectionTitle, Skeleton } from "@/components/ui";
import { t } from "@/i18n";
import { SimilarItem } from "./SimilarHint";

/** The requirements a saved one is most like (#208), leaving out pairs that stay decided. */
export function SimilarPanel({ requirement }: { requirement: Requirement }) {
  const shown = requirement.kind === "requirement" && !requirement.trashedAt;
  const { data, error, refetch } = useSimilar(requirement.key, shown);
  if (!shown) return null;
  const l = t.duplicates.panel;
  return (
    <Card className="space-y-3 p-4" data-similar-panel>
      <section aria-labelledby="similar-panel-title" className="space-y-3">
        <SectionTitle id="similar-panel-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && data.similar.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {data && data.similar.length > 0 && (
          <ul className="flex flex-col divide-y divide-border">
            {data.similar.map((hit) => (
              <SimilarItem key={hit.requirement.id} hit={hit} newTab={false} />
            ))}
          </ul>
        )}
      </section>
    </Card>
  );
}

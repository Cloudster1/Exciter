import { Link } from "@tanstack/react-router";
import type { Requirement } from "@/api/requirements";
import { useMitigations, type Mitigation } from "@/api/risks";
import { Card, ErrorBanner, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { t } from "@/i18n";
import { StateTag } from "./StateTag";

/** A risk's mitigations (#260): each requirement mitigating it, whether it is in effect, and the state they put it in. */
export function MitigationPanel({ requirement }: { requirement: Requirement }) {
  const isRisk = requirement.risk != null;
  const { data, error, refetch } = useMitigations(requirement.key, isRisk);
  if (!isRisk) return null;
  return (
    <Card className="space-y-3 p-4" data-mitigations="">
      <section aria-labelledby="mitigations-title" className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <SectionTitle id="mitigations-title">{t.risks.mitigations}</SectionTitle>
          {data && <StateTag state={data.state} />}
        </div>
        <p className="text-xs text-ink-muted">{t.risks.mitigationsHint}</p>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!data && !error && <Skeleton />}
        {data && data.mitigations.length === 0 && <p className="text-sm text-ink-muted">{t.risks.noMitigations}</p>}
        {data && data.mitigations.length > 0 && (
          <ul className="space-y-2">
            {data.mitigations.map((m) => (
              <MitigationItem key={m.linkId} mitigation={m} />
            ))}
          </ul>
        )}
      </section>
    </Card>
  );
}

function MitigationItem({ mitigation: m }: { mitigation: Mitigation }) {
  const levels = t.coverage.levels;
  return (
    <li className="flex flex-wrap items-center gap-2 text-sm" data-mitigation={m.source.key || "hidden"}>
      {m.source.hidden ? (
        <span className="text-ink-muted">{t.risks.hidden}</span>
      ) : (
        <Link to="/projects/$projectKey/r/$reqKey" params={{ projectKey: m.source.projectKey, reqKey: m.source.key }} className="font-medium text-accent">
          {m.source.key} {m.source.title}
        </Link>
      )}
      <Tag data-in-effect={m.inEffect ? "" : undefined}>{m.inEffect ? t.risks.inEffect : t.risks.notInEffect}</Tag>
      {m.suspect && <Tag>{t.risks.suspect}</Tag>}
      <span className="text-xs text-ink-muted">{t.risks.coverage(levels[m.implemented], levels[m.verified])}</span>
    </li>
  );
}

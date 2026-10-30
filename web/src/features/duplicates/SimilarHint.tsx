import { useId } from "react";
import { Link } from "@tanstack/react-router";
import { useSimilarDraft, type SimilarRequirement } from "@/api/duplicates";
import type { Project } from "@/api/projects";
import { Button, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { formatNumber } from "@/lib/format";
import { t } from "@/i18n";

/** A score of 0 to 1 as the percentage a reader takes in. */
export function percent(score: number): string {
  return formatNumber(score, { style: "percent", maximumFractionDigits: 0 });
}

/** The requirements a draft is most like (#208), below the statement while it is written, each opening in a new tab so
 * the draft stays; a library's offers to be reused instead. Nothing shows while there are none. */
export function SimilarHint({
  project,
  title,
  statement,
  excludeKey,
  enabled,
  onReuse,
}: {
  project: Project;
  title: string;
  statement: unknown;
  /** The requirement being revised, which is not like itself. */
  excludeKey?: string;
  enabled: boolean;
  /** Offered for a library's requirement: reuse it rather than write this one. */
  onReuse?: (hit: SimilarRequirement) => void;
}) {
  const on = enabled && project.can.edit && !project.archivedAt;
  const { found } = useSimilarDraft(project.key, { title, statement, ...(excludeKey ? { excludeKey } : {}) }, on);
  const titleId = useId();
  if (!on) return null;
  // A hint that could not be asked says nothing, as one with no requirements does; saving is not held up by it.
  const hits = found?.similar ?? [];
  const l = t.duplicates.hint;
  return (
    <div data-similar-hint>
      <p role="status" className="sr-only" data-similar-count={hits.length}>
        {l.count(hits.length)}
      </p>
      {hits.length > 0 && (
        <section aria-labelledby={titleId} className="space-y-2 rounded-control border border-border p-3">
          <div>
            {/* A heading would sit inside the entry being edited, among that entry's own headings. */}
            <p id={titleId} className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">
              {l.title}
            </p>
            <p className="text-sm text-ink-muted">{l.intro}</p>
          </div>
          <ul className="flex flex-col divide-y divide-border">
            {hits.map((hit) => (
              <SimilarItem key={hit.requirement.id} hit={hit} onReuse={onReuse} />
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/** One requirement found alike: its key and title opening it, how alike, and whether a library holds it. */
export function SimilarItem({ hit, newTab = true, onReuse }: { hit: SimilarRequirement; newTab?: boolean; onReuse?: (hit: SimilarRequirement) => void }) {
  const l = t.duplicates;
  const { requirement } = hit;
  return (
    <li className="flex flex-wrap items-center gap-2 py-1.5 text-sm" data-similar={requirement.key} data-library={hit.library ? "" : undefined}>
      <Link
        to="/projects/$projectKey/r/$reqKey"
        params={{ projectKey: requirement.projectKey, reqKey: requirement.key }}
        {...(newTab ? { target: "_blank", rel: "noopener" } : {})}
        className="inline-flex min-w-0 items-center gap-1 text-ink hover:text-accent hover:underline"
      >
        <span className="font-mono text-xs text-ink-muted">{requirement.key}</span>
        <span className="min-w-0">{requirement.title}</span>
        {newTab && <Icon.External />}
        {newTab && <span className="sr-only">{l.newTab}</span>}
      </Link>
      <Tag className="tabular-nums" data-similar-score={hit.score}>
        {l.alike(percent(hit.score))}
      </Tag>
      {hit.library && <Tag title={l.libraryOf(requirement.projectKey)}>{l.library}</Tag>}
      {hit.library && onReuse && (
        <Button variant="secondary" size="sm" aria-label={l.hint.reuseLabel(requirement.key)} onClick={() => onReuse(hit)} data-action="reuse-similar">
          {l.hint.reuse}
        </Button>
      )}
    </li>
  );
}

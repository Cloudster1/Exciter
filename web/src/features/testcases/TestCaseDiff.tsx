import type { components } from "@/api/schema";
import { Card, SectionTitle, Tag } from "@/components/ui";
import { DocDiffView, type DiffBlockView } from "@/features/editor/DocView";
import { t } from "@/i18n";

type Comparison = components["schemas"]["Comparison"];

/** What changed in a test case between two revisions: its preconditions, each step, and its parameters. */
export function TestCaseDiff({ comparison }: { comparison: Comparison }) {
  const steps = comparison.steps ?? [];
  const parameters = comparison.parameters ?? [];
  const preconditions = comparison.preconditions ?? [];
  if (steps.length === 0 && parameters.length === 0 && preconditions.length === 0) return null;
  return (
    <>
      {preconditions.length > 0 && (
        <Card className="space-y-2 p-4">
          <SectionTitle>{t.testcases.preconditions}</SectionTitle>
          <DocDiffView blocks={preconditions as DiffBlockView[]} data-diff-of="preconditions" />
        </Card>
      )}
      {steps.length > 0 && (
        <Card className="space-y-3 p-4" data-diff-of="steps">
          <SectionTitle>{t.testcases.steps}</SectionTitle>
          <ol className="space-y-3">
            {steps.map((s, i) => (
              <li key={i} className="space-y-1 rounded-control border border-border p-3" data-step-change={s.change}>
                <p className="flex flex-wrap items-center gap-2 text-sm font-semibold text-ink">
                  {t.testcases.step(s.to ?? s.from ?? i + 1)}
                  {s.change !== "equal" && <Tag>{changeLabel(s.change, s.from, s.to)}</Tag>}
                </p>
                <p className="text-xs font-semibold uppercase text-ink-muted">{t.testcases.action}</p>
                <DocDiffView blocks={s.action as DiffBlockView[]} />
                {s.expected.length > 0 && (
                  <>
                    <p className="text-xs font-semibold uppercase text-ink-muted">{t.testcases.expected}</p>
                    <DocDiffView blocks={s.expected as DiffBlockView[]} />
                  </>
                )}
              </li>
            ))}
          </ol>
        </Card>
      )}
      {parameters.some((p) => p.change !== "equal") && (
        <Card className="space-y-2 p-4" data-diff-of="parameters">
          <SectionTitle>{t.testcases.parameters}</SectionTitle>
          <ul className="space-y-1 text-sm">
            {parameters
              .filter((p) => p.change !== "equal")
              .map((p) => (
                <li key={p.name}>
                  <span className="font-medium text-ink">{p.name}</span> <span className="text-ink-muted line-through">{p.from.join("; ")}</span>{" "}
                  <span className="text-ink">{p.to.join("; ")}</span>
                </li>
              ))}
          </ul>
        </Card>
      )}
    </>
  );
}

function changeLabel(change: string, from?: number | null, to?: number | null): string {
  switch (change) {
    case "moved":
      return t.testcases.stepMoved(from ?? 0, to ?? 0);
    case "inserted":
      return t.testcases.stepAdded;
    case "deleted":
      return t.testcases.stepRemoved;
    default:
      return t.testcases.stepChanged;
  }
}

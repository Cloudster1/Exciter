import { Link } from "@tanstack/react-router";
import { useBaselines } from "@/api/baselines";
import type { ChangeBase, SpecAddress, SpecChange, SpecChanges, SpecEntry } from "@/api/tree";
import { Button, Card, Chip, SectionTitle, SelectInput, Switch, Tag } from "@/components/ui";
import { DocDiffView, type DiffBlockView } from "@/features/editor/DocView";
import { AttributeChanges } from "@/features/history/RequirementHistory";
import { t } from "@/i18n";

const BASES: ChangeBase[] = ["previous", "approved", "baseline"];

/** Whether an entry differs from its base, which next and previous change step between. */
export function differs(entry: SpecEntry): boolean {
  return Boolean(entry.change && entry.change.kind !== "unchanged");
}

/** The changed entry after, or before, the one given among those loaded; the first or last when none is given. */
export function nextChange(entries: SpecEntry[], from: string | null, step: 1 | -1): string | undefined {
  const changed = entries.filter(differs).map((entry) => entry.node.key);
  if (changed.length === 0) return undefined;
  const keys = entries.map((entry) => entry.node.key);
  const at = from ? keys.indexOf(from) : -1;
  if (at < 0) return step === 1 ? changed[0] : changed[changed.length - 1];
  const ahead = step === 1 ? keys.slice(at + 1) : keys.slice(0, at).reverse();
  return ahead.find((key) => changed.includes(key));
}

/** Switches the changes on and picks what they are against, a baseline among them; it says how many there are. */
export function ChangesBar({
  projectKey,
  address,
  summary,
  onChange,
  onStep,
}: {
  projectKey: string;
  address: SpecAddress;
  summary: SpecChanges;
  onChange: (next: SpecAddress) => void;
  onStep: (step: 1 | -1) => void;
}) {
  const { data: baselines } = useBaselines(projectKey);
  const on = Boolean(address.changes);
  const set = (patch: Partial<SpecAddress>) => onChange({ ...address, ...patch });
  return (
    <section aria-label={t.tree.changesLabel} className="flex flex-wrap items-center gap-2" data-spec-changes>
      <Switch
        label={t.tree.showChanges}
        checked={on}
        onChange={(checked) => set({ changes: checked ? "previous" : undefined, baseline: undefined, onlyChanged: undefined })}
        data-action="show-changes"
      />
      {on && (
        <>
          <SelectInput
            aria-label={t.tree.changesBase}
            value={address.changes}
            onChange={(event) => set({ changes: event.target.value as ChangeBase, baseline: undefined })}
            data-changes-base
          >
            {BASES.map((base) => (
              <option key={base} value={base}>
                {t.tree.bases[base]}
              </option>
            ))}
          </SelectInput>
          {address.changes === "baseline" && (
            <SelectInput
              aria-label={t.tree.changesBaseline}
              value={address.baseline ?? ""}
              onChange={(event) => set({ baseline: event.target.value || undefined })}
              data-changes-baseline
            >
              <option value="">{t.tree.pickBaseline}</option>
              {(baselines ?? []).map((baseline) => (
                <option key={baseline.id} value={baseline.id}>
                  {baseline.name}
                </option>
              ))}
            </SelectInput>
          )}
          <Chip pressed={Boolean(address.onlyChanged)} onClick={() => set({ onlyChanged: address.onlyChanged ? undefined : true })} data-filter="only-changed">
            {t.tree.onlyChanged}
          </Chip>
          {summary && (
            <span className="text-sm text-ink-muted" data-changes-count={summary.changed}>
              {t.tree.changedCount(summary.changed)}
            </span>
          )}
          <Button variant="ghost" size="sm" onClick={() => onStep(-1)} data-action="previous-change">
            {t.tree.previousChange}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => onStep(1)} data-action="next-change">
            {t.tree.nextChange}
          </Button>
        </>
      )}
    </section>
  );
}

/** What an entry's change is called beside its heading: changed, added, moved from where, or unchanged. */
export function ChangeTag({ change }: { change: SpecChange }) {
  const label = change.movedFrom ? `${t.tree.kinds[change.kind]}, ${t.tree.wasAt(change.movedFrom)}` : t.tree.kinds[change.kind];
  return <Tag data-spec-change={change.kind}>{label}</Tag>;
}

/** What changed in an entry's statement, rationale and attributes, and the way to its whole comparison. */
export function EntryChanges({ projectKey, entry, change }: { projectKey: string; entry: SpecEntry; change: SpecChange }) {
  const comparison = change.comparison;
  if (!comparison) return null;
  return (
    <div className="space-y-2" data-spec-diff={entry.node.key}>
      <DocDiffView blocks={comparison.statement as DiffBlockView[]} data-diff-of="statement" />
      {comparison.rationale.some((block) => block.change !== "equal") && (
        <div className="text-sm text-ink-muted">
          <span className="font-medium">{t.requirements.rationale}: </span>
          <DocDiffView blocks={comparison.rationale as DiffBlockView[]} data-diff-of="rationale" />
        </div>
      )}
      {comparison.attributes.some((attribute) => attribute.change !== "equal") && <AttributeChanges attributes={comparison.attributes} />}
      <Link
        to="/p/$projectKey/r/$reqKey/history"
        params={{ projectKey, reqKey: entry.node.key }}
        search={{ from: change.from, to: change.to }}
        className="text-xs text-ink-muted hover:text-accent hover:underline"
        data-full-comparison=""
      >
        {t.tree.fullComparison(change.from, change.to)}
      </Link>
    </div>
  );
}

/** What a baseline held that the document no longer has, as the baseline held it. */
export function RemovedSince({ removed }: { removed: NonNullable<SpecChanges>["removed"] }) {
  if (removed.length === 0) return null;
  return (
    <Card className="space-y-2 p-4" data-spec-removed>
      <SectionTitle>{t.tree.removedTitle}</SectionTitle>
      <ul className="space-y-1 text-sm">
        {removed.map((each) => (
          <li key={each.key} data-removed={each.key}>
            <del className="text-ink-muted">
              <span className="font-mono">{each.outline}</span> {each.title}
            </del>{" "}
            <span className="font-mono text-xs text-ink-subtle">{each.key}</span>
          </li>
        ))}
      </ul>
    </Card>
  );
}

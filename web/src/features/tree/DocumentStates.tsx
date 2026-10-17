import { useState, type MouseEvent } from "react";
import { ApiError } from "@/api/client";
import type { SpecAddress, SpecEntry } from "@/api/tree";
import {
  STATUS_CATEGORIES,
  useBulkTransitions,
  useStatuses,
  useTakeBulkTransition,
  useTakeTransition,
  useTransitions,
  type AvailableTransition,
  type BulkTransitionResult,
  type RequirementTransitions,
  type StatusCategory,
} from "@/api/workflows";
import { Button, Checkbox, Chip, ErrorBanner, Menu, SelectInput } from "@/components/ui";
import type { Picked, Selection } from "@/features/armature/CreateIssues";
import { CommentDialog } from "@/features/workflows/TransitionBar";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { t } from "@/i18n";

/** The transitions every requirement answered has, by id, in the first one's order; none when one has none. */
export function sharedTransitions(answers: RequirementTransitions[], keys: string[]): AvailableTransition[] {
  if (answers.length === 0 || answers.length < keys.length) return [];
  const [first, ...rest] = answers;
  return (first?.transitions ?? []).filter((transition) => rest.every((each) => each.transitions.some((other) => other.id === transition.id)));
}

/** The keys from one to the other, both included, in the order given, whichever comes first. */
export function rangeOf(keys: string[], from: string, to: string): string[] {
  const a = keys.indexOf(from);
  const b = keys.indexOf(to);
  if (a < 0 || b < 0) return [to];
  return keys.slice(Math.min(a, b), Math.max(a, b) + 1);
}

/** What a picked entry is to the selection. */
export function pickedOf(entry: SpecEntry): Picked {
  return { key: entry.node.key, title: entry.node.title, outline: entry.node.outline };
}

/** A checkbox picking one entry; with Shift it picks or drops everything since the last one picked. */
export function EntryPick({
  entry,
  selection,
  onPick,
}: {
  entry: SpecEntry;
  selection: Selection;
  onPick: (key: string, on: boolean, range: boolean) => void;
}) {
  return (
    <Checkbox
      label={<span className="sr-only">{t.tree.select(entry.node.key)}</span>}
      className="shrink-0"
      checked={selection.has(entry.node.key)}
      onClick={(event: MouseEvent<HTMLInputElement>) => onPick(entry.node.key, event.currentTarget.checked, event.shiftKey)}
      onChange={() => {}}
      data-pick={entry.node.key}
    />
  );
}

/** A section's checkbox, which picks or drops every entry below it that can be picked. */
export function SectionPick({ entry, below, selection }: { entry: SpecEntry; below: Picked[]; selection: Selection }) {
  if (below.length === 0) return null;
  const all = below.every((each) => selection.has(each.key));
  return (
    <Checkbox
      label={<span className="sr-only">{t.tree.selectBelow(entry.node.title)}</span>}
      className="shrink-0"
      checked={all}
      onChange={(event) => selection.set(below, event.target.checked)}
      data-pick-below={entry.node.key}
    />
  );
}

/** A requirement's status, which opens what the reader may take from it now, read when it opens. */
export function StatusMenu({ entry, onMoved }: { entry: SpecEntry; onMoved: (notice: string) => void }) {
  const { node } = entry;
  const [wanted, setWanted] = useState(false);
  const { data: transitions, isLoading } = useTransitions(node.key, wanted);
  const take = useTakeTransition(node.key);
  const [asking, setAsking] = useState<AvailableTransition | null>(null);
  if (!node.status) return null;
  const badge = <StatusBadge name={node.status.name} category={node.status.category} />;
  if (!entry.can.transition) return badge;
  const fields = take.error instanceof ApiError ? take.error.fields : {};
  const refusal = take.error ? [take.error.message, ...Object.values(fields)].join(" ") : "";

  function go(transition: AvailableTransition, comment?: string) {
    take.mutate(
      { transitionId: transition.id, comment },
      {
        onSuccess: (moved) => {
          setAsking(null);
          onMoved(t.tree.movedTo(node.key, moved.status?.name ?? transition.toStatus.name));
        },
      },
    );
  }

  const items = isLoading
    ? [{ label: t.tree.loadingTransitions, disabled: true, onSelect: () => {} }]
    : (transitions ?? []).length === 0
      ? [{ label: t.tree.noTransitions, disabled: true, onSelect: () => {} }]
      : (transitions ?? []).map((transition) => ({
          label: `${transition.name}: ${transition.toStatus.name}`,
          onSelect: () => (transition.needsComment ? setAsking(transition) : go(transition)),
          attrs: { "data-transition": transition.name },
        }));
  return (
    <>
      <Menu
        label={t.tree.statusOf(node.key)}
        trigger={(props) => (
          <Button
            variant="link"
            className="rounded-full hover:no-underline"
            onClick={() => {
              setWanted(true);
              props.toggle();
            }}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            aria-label={t.tree.statusMenu(node.key, node.status!.name)}
            data-action="status-menu"
          >
            {badge}
          </Button>
        )}
        items={items}
      />
      {refusal && !asking && (
        <span className="w-full">
          <ErrorBanner>{refusal}</ErrorBanner>
        </span>
      )}
      {asking && (
        <CommentDialog
          transition={asking}
          pending={take.isPending}
          error={take.error ? (fields.comment ?? refusal) : undefined}
          onSubmit={(comment) => go(asking, comment)}
          onClose={() => {
            setAsking(null);
            take.reset();
          }}
        />
      )}
    </>
  );
}

/** The transitions every picked requirement has, taken on all of them, each on its own; it says how each went. */
export function BulkTransitions({ projectKey, selection, onDone }: { projectKey: string; selection: Selection; onDone: (notice: string) => void }) {
  const keys = selection.picked.map((each) => each.key);
  const answers = useBulkTransitions(projectKey, keys);
  const take = useTakeBulkTransition(projectKey);
  const [asking, setAsking] = useState<AvailableTransition | null>(null);
  const [results, setResults] = useState<BulkTransitionResult[]>([]);
  const shared = answers.data ? sharedTransitions(answers.data, keys) : [];
  const refused = results.filter((each) => each.outcome === "refused");

  function go(transition: AvailableTransition, comment?: string) {
    take.mutate(
      { transitionId: transition.id, comment, keys },
      {
        onSuccess: (done) => {
          setAsking(null);
          setResults(done);
          const taken = done.filter((each) => each.outcome === "taken");
          // What moved is done with; what was refused stays picked, to try again.
          selection.set(
            selection.picked.filter((each) => taken.some((result) => result.key === each.key)),
            false,
          );
          onDone(t.tree.bulkMoved(taken.length, done.length - taken.length, transition.toStatus.name));
        },
      },
    );
  }

  return (
    <div className="flex w-full flex-wrap items-center gap-2" data-bulk-transitions>
      <span className="text-sm text-ink-muted">{t.tree.bulkLabel}</span>
      {answers.isLoading && <span className="text-sm text-ink-subtle">{t.tree.loadingTransitions}</span>}
      {answers.data && shared.length === 0 && (
        <span className="text-sm text-ink-subtle" data-bulk-none>
          {t.tree.noSharedTransitions}
        </span>
      )}
      {shared.map((transition) => (
        <Button
          key={transition.id}
          size="sm"
          variant="secondary"
          loading={take.isPending && take.variables?.transitionId === transition.id}
          onClick={() => (transition.needsComment ? setAsking(transition) : go(transition))}
          data-bulk-transition={transition.name}
          title={t.workflows.leadsTo(transition.toStatus.name)}
        >
          {transition.name}
        </Button>
      ))}
      {take.error && !asking && (
        <span className="w-full">
          <ErrorBanner>{take.error.message}</ErrorBanner>
        </span>
      )}
      {refused.length > 0 && (
        <ul className="w-full space-y-1 text-sm text-danger" aria-label={t.tree.refusedLabel} data-bulk-refused>
          {refused.map((each) => (
            <li key={each.key} data-refused={each.key}>
              <span className="font-mono">{each.key}</span>: {each.error?.message}
            </li>
          ))}
        </ul>
      )}
      {asking && (
        <CommentDialog
          transition={asking}
          pending={take.isPending}
          error={take.error?.message}
          onSubmit={(comment) => go(asking, comment)}
          onClose={() => {
            setAsking(null);
            take.reset();
          }}
        />
      )}
    </div>
  );
}

/** Narrows the document to status categories and to one status, changing its address. */
export function StatusFilter({ projectKey, address, onChange }: { projectKey: string; address: SpecAddress; onChange: (next: SpecAddress) => void }) {
  const { data: statuses } = useStatuses(projectKey);
  const categories = address.category ?? [];
  const toggle = (category: StatusCategory) => {
    const next = categories.includes(category) ? categories.filter((each) => each !== category) : [...categories, category];
    onChange({ ...address, category: next.length ? next : undefined });
  };
  return (
    <section aria-label={t.tree.filterLabel} className="flex flex-wrap items-center gap-2" data-spec-filter>
      {STATUS_CATEGORIES.map((category) => (
        <Chip key={category} pressed={categories.includes(category)} onClick={() => toggle(category)} data-filter-category={category}>
          {t.workflows.categories[category]}
        </Chip>
      ))}
      <SelectInput
        aria-label={t.tree.filterStatus}
        value={address.status?.[0] ?? ""}
        onChange={(event) => onChange({ ...address, status: event.target.value ? [event.target.value] : undefined })}
        data-filter-status
      >
        <option value="">{t.tree.anyStatus}</option>
        {(statuses ?? []).map((status) => (
          <option key={status.id} value={status.id}>
            {status.name}
          </option>
        ))}
      </SelectInput>
      {(categories.length > 0 || address.status?.length) && (
        <Button variant="ghost" size="sm" onClick={() => onChange({})} data-action="clear-status-filter">
          {t.tree.clearFilter}
        </Button>
      )}
    </section>
  );
}

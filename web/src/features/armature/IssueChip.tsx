import type { ArmatureIssue, ArmatureIssues } from "@/api/armature";
import { cx } from "@/components/ui";
import { ARMATURE_ACCOUNT_PATH } from "@/config";
import { t } from "@/i18n";

// Stator's chip, for a trace: the issue's key, summary, status and assignee as the viewer may see them, or what
// everybody may know of it without a token.

/** What a chip can show this viewer: the issue itself, or the key with why there is no more. */
export type ChipState = { kind: "issue"; issue: ArmatureIssue } | { kind: "connect" } | { kind: "loading" } | { kind: "unreachable" } | { kind: "hidden" };

/** Decides a chip's state from what the lookup found for its key. */
export function chipState(key: string, lookup: ArmatureIssues, connected: boolean): ChipState {
  if (!connected) return { kind: "connect" };
  switch (lookup.status) {
    case "not_configured":
    case "not_connected":
    case "rejected":
      return { kind: "connect" };
    case "unreachable":
      return { kind: "unreachable" };
  }
  if (!lookup.issues.has(key)) return { kind: "loading" };
  const issue = lookup.issues.get(key);
  return issue ? { kind: "issue", issue } : { kind: "hidden" };
}

const CATEGORY_STYLES: Record<string, string> = {
  todo: "bg-status-todo-subtle",
  in_progress: "bg-status-progress-subtle",
  done: "bg-status-done-subtle",
};

/** A status as a lozenge coloured by its category; every Armature workflow's states are to do, in progress or done. */
export function StatusLozenge({ name, category }: { name: string; category: string }) {
  return (
    <span
      className={cx(
        "rounded px-1 py-px text-2xs font-semibold tracking-wide whitespace-nowrap text-ink uppercase",
        CATEGORY_STYLES[category] ?? CATEGORY_STYLES.todo,
      )}
      data-status-category={category}
    >
      <span className="sr-only">{t.armature.chip.status} </span>
      {name}
    </span>
  );
}

/** An issue a requirement is traced to, drawn from the lookup, falling back on the stored status category. */
export function IssueChip({
  issueKey,
  href,
  state,
  storedCategory,
  stale = false,
}: {
  issueKey: string;
  href: string;
  state: ChipState;
  storedCategory: string;
  /** Armature moved the issue since without saying where, so the stored category may be out of date. */
  stale?: boolean;
}) {
  if (state.kind === "issue") {
    const issue = state.issue;
    return (
      <a
        href={issue.url}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex min-w-0 max-w-full items-baseline gap-2 text-ink hover:underline"
        data-armature-issue={issueKey}
        data-state="issue"
      >
        {/* An issue that moved shows the key it has now; the link keeps the one it was made with. */}
        <span className="shrink-0 font-mono text-xs text-ink-muted">{issue.key}</span>
        <span className="min-w-0 truncate font-medium">{issue.summary}</span>
        <StatusLozenge name={issue.status.name} category={issue.status.category} />
        <span className="shrink-0 text-xs text-ink-muted">{issue.assigneeName || t.armature.chip.unassigned}</span>
      </a>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-baseline gap-2" data-armature-issue={issueKey} data-state={state.kind}>
      <a href={href} target="_blank" rel="noopener noreferrer" className="font-mono text-xs text-ink hover:underline">
        {issueKey}
      </a>
      {storedCategory && <StatusLozenge name={t.armature.chip.categories[storedCategory] ?? storedCategory} category={storedCategory} />}
      {storedCategory && stale && (
        <span className="text-xs text-ink-muted" data-status-stale>
          {t.armature.chip.stale}
        </span>
      )}
      {state.kind === "connect" && (
        <a href={ARMATURE_ACCOUNT_PATH} className="text-xs text-ink-muted underline" data-action="connect-armature-hint">
          {t.armature.chip.connect}
        </a>
      )}
      {state.kind === "hidden" && <span className="text-xs text-ink-muted">{t.armature.chip.notAvailable}</span>}
      {state.kind === "unreachable" && <span className="text-xs text-ink-muted">{t.armature.chip.unreachable}</span>}
    </span>
  );
}

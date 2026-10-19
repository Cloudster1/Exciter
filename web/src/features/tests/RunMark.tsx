import type { LastRun } from "@/api/tests";
import { Icon } from "@/components/icons";
import { Tag } from "@/components/ui";
import { t } from "@/i18n";

/** A last result in words. */
export function runLabel(run: LastRun): string {
  return run.outcome === "none" ? t.tests.notRun : (t.tests.outcome[run.outcome] ?? run.outcome);
}

/** A test case's last result in a test execution, marked when it ran an older revision (#252). */
export function RunMark({ run }: { run: LastRun | null | undefined }) {
  if (!run) return null;
  const label = runLabel(run);
  return (
    <span className="inline-flex items-center gap-1" data-last-run={run.outcome}>
      <Tag title={t.tests.lastRun}>{label}</Tag>
      {run.outdated && <Icon.Warning className="size-3.5 text-warning" label={t.tests.runOutdated} />}
    </span>
  );
}

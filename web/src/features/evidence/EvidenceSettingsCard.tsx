import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useEvidenceSettings, useSaveEvidenceSettings } from "@/api/testResults";
import { Button, Card, ErrorBanner, Field, SectionTitle, Skeleton } from "@/components/ui";
import { TEST_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** The branch whose test results count for a project's coverage, matrix and queries; its administrators name it. */
export function EvidenceSettingsCard({ project }: { project: Project }) {
  const { data, error, refetch } = useEvidenceSettings(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return <SettingsForm key={data.countingBranch} project={project} branch={data.countingBranch} />;
}

function SettingsForm({ project, branch: saved }: { project: Project; branch: string }) {
  const [branch, setBranch] = useState(saved);
  const [notice, setNotice] = useState("");
  const save = useSaveEvidenceSettings(project.key);
  const may = project.can.administer && !project.archivedAt;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const l = t.evidence.settings;
  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    save.mutate(branch.trim(), { onSuccess: () => setNotice(l.saved) });
  }
  return (
    <Card className="space-y-3 p-4" data-evidence-settings>
      <SectionTitle>{l.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3" aria-label={l.title}>
        <div className="min-w-56 flex-1">
          <Field
            id="evidence-counting-branch"
            label={l.branch}
            value={branch}
            maxLength={TEST_NAME_MAX_LENGTH}
            disabled={!may}
            error={fields.countingBranch}
            onChange={(event) => setBranch(event.target.value)}
          />
        </div>
        {may && (
          <Button type="submit" loading={save.isPending} disabled={branch.trim() === saved} data-action="save-evidence-settings">
            {l.save}
          </Button>
        )}
      </form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
    </Card>
  );
}

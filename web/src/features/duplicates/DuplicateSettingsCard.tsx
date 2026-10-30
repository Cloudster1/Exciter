import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useDuplicateSettings, useSaveDuplicateSettings } from "@/api/duplicates";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, Field, SectionTitle, Skeleton, useToast } from "@/components/ui";
import { DUPLICATE_THRESHOLD_MAX, DUPLICATE_THRESHOLD_MIN, DUPLICATE_THRESHOLD_STEP } from "@/config";
import { t } from "@/i18n";
import { percent } from "./SimilarHint";

const PERCENT = 100;
// Dividing by a hundred leaves binary fractions; the setting is kept to the hundredth the API speaks in.
const rounded = (value: number) => Math.round(value * PERCENT) / PERCENT;

/** How alike two requirements must be for the project to list them as likely duplicates (#208); its administrators
 * set it. */
export function DuplicateSettingsCard({ project }: { project: Project }) {
  const { data, error, refetch } = useDuplicateSettings(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return <ThresholdForm key={data.threshold} project={project} saved={data.threshold} />;
}

function ThresholdForm({ project, saved }: { project: Project; saved: number }) {
  const toast = useToast();
  // Typed in whole percent, which is how the rest of the client shows a similarity; sent as the fraction it is.
  const [typed, setTyped] = useState(String(Math.round(saved * PERCENT)));
  const save = useSaveDuplicateSettings(project.key);
  const may = project.can.administer && !project.archivedAt;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const l = t.duplicates.settings;
  const threshold = typed.trim() === "" ? Number.NaN : rounded(Number(typed) / PERCENT);
  function submit(event: FormEvent) {
    event.preventDefault();
    // The saved threshold remounts this form, which drops a mutate call's own onSuccess; the promise resolves regardless.
    void save
      .mutateAsync(threshold)
      .then(() => toast.success(l.saved))
      .catch(() => {});
  }
  return (
    <Card className="space-y-3 p-4" data-duplicate-settings>
      <SectionTitle>{l.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3" aria-label={l.title}>
        <div className="w-56">
          <Field
            id="duplicate-threshold"
            type="number"
            inputMode="numeric"
            label={l.threshold}
            hint={l.range(percent(DUPLICATE_THRESHOLD_MIN), percent(DUPLICATE_THRESHOLD_MAX))}
            min={DUPLICATE_THRESHOLD_MIN * PERCENT}
            max={DUPLICATE_THRESHOLD_MAX * PERCENT}
            step={DUPLICATE_THRESHOLD_STEP * PERCENT}
            value={typed}
            disabled={!may}
            error={fields.threshold}
            onChange={(event) => setTyped(event.target.value)}
            data-duplicate-threshold
          />
        </div>
        {may && (
          <Button
            type="submit"
            loading={save.isPending}
            disabled={!Number.isFinite(threshold) || threshold === rounded(saved)}
            data-action="save-duplicate-settings"
          >
            {l.save}
          </Button>
        )}
      </form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
    </Card>
  );
}

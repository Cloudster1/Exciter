import { useId, useState, type FormEvent } from "react";
import { useArmatureProjects } from "@/api/armature";
import { ApiError } from "@/api/client";
import { useCaseActions } from "@/api/tests";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { ARMATURE_ACCOUNT_PATH } from "@/config";
import { t } from "@/i18n";

/** Files a failed case iteration as an Armature issue with the tester's own token, linked with relates. */
export function FileIssueDialog({
  execution,
  caseKey,
  iteration,
  steps,
  failedStep,
  onClose,
}: {
  execution: string;
  caseKey: string;
  iteration: number;
  steps: number;
  failedStep?: number;
  onClose: () => void;
}) {
  const id = useId();
  const projects = useArmatureProjects(true);
  const { fileIssue } = useCaseActions(execution, caseKey);
  const status = projects.data?.status;
  const creatable = status === "ok" ? projects.data!.projects.filter((p) => p.canCreate) : [];
  const [project, setProject] = useState("");
  const [step, setStep] = useState(failedStep ? String(failedStep) : "");
  const [summary, setSummary] = useState("");
  const chosen = creatable.some((p) => p.key === project) ? project : (creatable[0]?.key ?? "");
  const fields = fileIssue.error instanceof ApiError ? fileIssue.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    fileIssue.mutate(
      { iteration, armatureProject: chosen, ...(step ? { step: Number(step) } : {}), summary: summary.trim() || undefined },
      { onSuccess: onClose },
    );
  }

  return (
    <Dialog title={t.tests.fileIssueTitle} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3" noValidate data-file-issue>
        <p className="text-sm text-ink-muted">{t.tests.fileIssueHint}</p>
        {status === "not_connected" && (
          <p className="text-sm text-ink-muted">
            {t.armature.create.connect}{" "}
            <a href={ARMATURE_ACCOUNT_PATH} className="underline">
              {t.armature.create.connectLink}
            </a>
          </p>
        )}
        {status === "not_configured" && <p className="text-sm text-ink-muted">{t.armature.account.notConfigured}</p>}
        {fileIssue.error && <ErrorBanner>{Object.values(fields)[0] ?? fileIssue.error.message}</ErrorBanner>}
        {creatable.length > 0 && (
          <Select label={t.tests.armatureProject} id={`${id}-project`} value={chosen} onChange={(e) => setProject(e.target.value)}>
            {creatable.map((p) => (
              <option key={p.key} value={p.key}>{`${p.key} ${p.name}`}</option>
            ))}
          </Select>
        )}
        <Select label={t.tests.failedStep} id={`${id}-step`} value={step} onChange={(e) => setStep(e.target.value)}>
          <option value="">{t.tests.noStep}</option>
          {Array.from({ length: steps }, (_, i) => (
            <option key={i} value={String(i + 1)}>
              {t.tests.step(i + 1)}
            </option>
          ))}
        </Select>
        <Field label={t.tests.summary} hint={t.tests.summaryHint} value={summary} onChange={(e) => setSummary(e.target.value)} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.tests.cancel}
          </Button>
          <Button type="submit" loading={fileIssue.isPending} disabled={!chosen} data-action="file-issue-send">
            {t.tests.fileIssueSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

import { useId, useState } from "react";
import { subjectKey, useInspectAccess, type AccessReport, type AccessRight, type AccessStep, type Subject } from "@/api/permissions";
import type { Project } from "@/api/projects";
import { Icon } from "@/components/icons";
import { Dialog, ErrorBanner, Skeleton, Tag, cx } from "@/components/ui";
import { t } from "@/i18n";
import { SubjectGlyph, SubjectPicker } from "./SubjectPicker";

/** The first step a right does not meet, which is why it is refused; -1 when it is allowed. */
export function decidingStep(right: AccessRight): number {
  return right.steps.findIndex((step) => !step.passed);
}

/** One step as a sentence about the person checked. */
export function stepSentence(step: AccessStep, report: Pick<AccessReport, "role" | "roleSource">): string {
  switch (step.kind) {
    case "orgAdmin": {
      const role = t.access.orgAdmin(report.role === "owner");
      return report.roleSource === "oidc" ? `${role} ${t.access.fromProvider}` : role;
    }
    case "guest":
      if (report.role !== "guest") return t.access.memberReached;
      return step.passed ? t.access.guestNamed : t.access.guestNotReached;
    case "grant":
      return step.passed ? t.access.grantYes : t.access.grantNo;
    case "archived":
      return t.access.archived;
  }
}

/** Pick a member and see what they may do in a project: each right, and the grants behind it, the one that decides a no marked. */
export function AccessDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const [person, setPerson] = useState<Subject>();
  return (
    <Dialog title={t.access.title(project.name)} wide onClose={onClose} data-access-dialog={project.key}>
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">{t.access.intro}</p>
        <SubjectPicker label={t.access.pickerLabel} peopleOnly inline onPick={setPerson} />
        {person?.id && <Report projectKey={project.key} userId={person.id} />}
      </div>
    </Dialog>
  );
}

function Report({ projectKey, userId }: { projectKey: string; userId: string }) {
  const { data, error, refetch } = useInspectAccess(projectKey, userId);
  const headingId = useId();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data)
    return (
      <div role="status" aria-label={t.access.loading}>
        <Skeleton />
      </div>
    );
  return (
    <section className="space-y-3" aria-labelledby={headingId} data-access-report={data.person.id}>
      <h3 id={headingId} className="text-sm font-semibold text-ink">
        {t.access.heading(data.person.name || data.person.email)}
      </h3>
      <ul className="space-y-3">
        {data.rights.map((right) => (
          <RightCard key={right.right} right={right} report={data} />
        ))}
      </ul>
    </section>
  );
}

function RightCard({ right, report }: { right: AccessRight; report: AccessReport }) {
  const deciding = decidingStep(right);
  return (
    <li className="space-y-2 rounded-control border border-border p-3" data-access-right={right.right} data-allowed={right.allowed}>
      <h4 className="flex items-center gap-2 text-sm font-semibold text-ink">
        {right.allowed ? <Icon.Check className="text-success" /> : <Icon.X className="text-danger" />}
        <span>{t.access.rights[right.right]}</span>
        <Tag className={right.allowed ? "text-success" : "text-danger"}>{right.allowed ? t.access.allowed : t.access.denied}</Tag>
      </h4>
      <ol className="space-y-2">
        {right.steps.map((step, i) => (
          <Step key={step.kind} step={step} report={report} deciding={i === deciding} />
        ))}
      </ol>
    </li>
  );
}

function Step({ step, report, deciding }: { step: AccessStep; report: AccessReport; deciding: boolean }) {
  return (
    <li
      className={cx("flex gap-2 text-sm", deciding ? "rounded-control bg-surface-raised p-2 text-ink" : "text-ink-muted")}
      data-access-step={step.kind}
      data-passed={step.passed}
      data-decides={deciding || undefined}
    >
      <span className="mt-0.5 shrink-0" aria-hidden="true">
        {step.passed ? <Icon.Check className="text-success" /> : <Icon.X className="text-danger" />}
      </span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <p>
          <span className="sr-only">{step.passed ? t.access.passed : t.access.failed}: </span>
          {stepSentence(step, report)}
          {deciding && <strong className="ml-1.5 font-semibold text-danger">{t.access.decides}</strong>}
        </p>
        {step.grants.length > 0 && (
          <ul className="flex flex-wrap gap-1.5" data-grant-list>
            {step.grants.map((grant) => (
              <li
                key={subjectKey(grant.subject)}
                className="inline-flex h-7 items-center gap-1.5 rounded-control border border-border bg-surface-raised px-2 text-sm text-ink"
                data-grant={grant.subject.name}
              >
                <SubjectGlyph type={grant.subject.type} />
                <span>{grant.subject.type === "everyone" ? t.permissions.everyone : grant.subject.name}</span>
                <span className="text-xs text-ink-subtle">{t.permissions.roles[grant.role]}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}

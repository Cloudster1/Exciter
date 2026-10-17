import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useLockRequirement, type Requirement } from "@/api/requirements";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LOCK_REASON_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

type Lock = NonNullable<Requirement["lock"]>;

/** Why a locked requirement says so, by its status or an administrator's hand (#205). */
export function lockSentence(key: string, lock: Lock): string {
  return lock.by === "hand" ? t.control.lockedByHand(key, lock.lockedByName, lock.reason) : t.control.lockedByStatus(key, lock.status?.name ?? "");
}

/** Whether the reader may lock and unlock the project's requirements by hand. */
export function keepsLocks(project: Project): boolean {
  return project.can.administer && !project.archivedAt;
}

/** A locked requirement's banner, with the administrators' unlock of a hand lock. */
export function LockBanner({ project, requirement, onNotice }: { project: Project; requirement: Requirement; onNotice: (message: string) => void }) {
  const { unlock } = useLockRequirement(requirement.key);
  const lock = requirement.lock;
  if (!lock) return null;
  return (
    <div
      role="status"
      className="flex flex-wrap items-start gap-2 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink"
      data-lock={lock.by}
    >
      <Icon.Lock className="mt-0.5 size-4 shrink-0 text-ink-muted" aria-hidden="true" />
      <div className="min-w-0 flex-1 space-y-1">
        <p>{lockSentence(requirement.key, lock)}</p>
        <p className="text-ink-muted">{t.control.lockedStill}</p>
        {unlock.error && <ErrorBanner>{unlock.error.message}</ErrorBanner>}
      </div>
      {lock.by === "hand" && keepsLocks(project) && (
        <Button
          size="sm"
          variant="secondary"
          loading={unlock.isPending}
          onClick={() => {
            if (window.confirm(t.control.confirmUnlock)) unlock.mutate(undefined, { onSuccess: () => onNotice(t.control.unlockedNotice(requirement.key)) });
          }}
          data-action="unlock-requirement"
        >
          {t.control.unlock}
        </Button>
      )}
    </div>
  );
}

/** The administrators' lock by hand, for a requirement not already locked so. */
export function LockAction({ project, requirement, onNotice }: { project: Project; requirement: Requirement; onNotice: (message: string) => void }) {
  const [open, setOpen] = useState(false);
  if (!keepsLocks(project) || requirement.kind === "section" || requirement.trashedAt || requirement.lock?.by === "hand") return null;
  return (
    <>
      <Button variant="secondary" icon={<Icon.Lock />} onClick={() => setOpen(true)} data-action="lock-requirement">
        {t.control.lock}
      </Button>
      {open && (
        <LockDialog
          requirement={requirement}
          onClose={() => setOpen(false)}
          onLocked={() => {
            setOpen(false);
            onNotice(t.control.lockedNotice(requirement.key));
          }}
        />
      )}
    </>
  );
}

function LockDialog({ requirement, onClose, onLocked }: { requirement: Requirement; onClose: () => void; onLocked: () => void }) {
  const { lock } = useLockRequirement(requirement.key);
  const [reason, setReason] = useState("");
  const fields = lock.error instanceof ApiError ? lock.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    if (reason.trim()) lock.mutate(reason.trim(), { onSuccess: onLocked });
  }

  return (
    <Dialog title={t.control.lockTitle(requirement.key)} onClose={onClose} data-lock-dialog="">
      <form onSubmit={submit} className="space-y-3" aria-label={t.control.lockTitle(requirement.key)}>
        <p className="text-sm text-ink-muted">{t.control.lockIntro}</p>
        {lock.error && !fields.reason && <ErrorBanner>{lock.error.message}</ErrorBanner>}
        <Field
          label={t.control.lockReason}
          hint={t.control.lockReasonHint}
          value={reason}
          maxLength={LOCK_REASON_MAX_LENGTH}
          error={fields.reason}
          onChange={(event) => setReason(event.target.value)}
          rows={3}
        />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.control.cancel}
          </Button>
          <Button type="submit" loading={lock.isPending} disabled={!reason.trim()} data-action="confirm-lock">
            {t.control.lockSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

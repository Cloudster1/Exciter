import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Requirement } from "@/api/requirements";
import { useTakeTransition, useTransitions, type AvailableTransition } from "@/api/workflows";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { REVISION_COMMENT_MAX_LENGTH, TRANSITION_COMMENT_ROWS } from "@/config";
import { t } from "@/i18n";
import { StatusBadge } from "./StatusBadge";

/** A requirement's status and the transitions the reader may take from it now; one that asks why opens a
 * comment first, and a refusal says what to do about it. */
export function TransitionBar({ requirement }: { requirement: Requirement }) {
  const open = requirement.can.transition && !requirement.trashedAt;
  const { data: transitions } = useTransitions(requirement.key, open && Boolean(requirement.status));
  const take = useTakeTransition(requirement.key);
  const [asking, setAsking] = useState<AvailableTransition | null>(null);
  const [notice, setNotice] = useState("");
  if (!requirement.status) return null;
  const fields = take.error instanceof ApiError ? take.error.fields : {};
  const refusal = take.error && !asking ? [take.error.message, ...Object.values(fields)].join(" ") : "";

  function go(transition: AvailableTransition, comment?: string) {
    setNotice("");
    take.mutate(
      { transitionId: transition.id, comment },
      {
        onSuccess: (moved) => {
          setAsking(null);
          setNotice(t.workflows.moved(moved.status?.name ?? transition.toStatus.name));
        },
      },
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2" data-transition-bar>
      <StatusBadge name={requirement.status.name} category={requirement.status.category} className="text-sm" />
      {open &&
        (transitions ?? []).map((transition) => (
          <Button
            key={transition.id}
            size="sm"
            variant="secondary"
            loading={take.isPending && take.variables?.transitionId === transition.id}
            onClick={() => (transition.needsComment ? setAsking(transition) : go(transition))}
            data-transition={transition.name}
            title={t.workflows.leadsTo(transition.toStatus.name)}
          >
            {transition.name}
          </Button>
        ))}
      <span role="status" className="text-sm text-ink-muted empty:hidden" data-transition-notice>
        {notice}
      </span>
      {refusal && (
        <div className="w-full">
          <ErrorBanner>{refusal}</ErrorBanner>
        </div>
      )}
      {asking && (
        <CommentDialog
          transition={asking}
          pending={take.isPending}
          error={take.error ? (fields.comment ?? [take.error.message, ...Object.values(fields)].join(" ")) : undefined}
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

/** Asks why before a transition that wants a comment, and says what refused it. */
export function CommentDialog({
  transition,
  pending,
  error,
  onSubmit,
  onClose,
}: {
  transition: AvailableTransition;
  pending: boolean;
  error?: string;
  onSubmit: (comment: string) => void;
  onClose: () => void;
}) {
  const [comment, setComment] = useState("");
  function submit(event: FormEvent) {
    event.preventDefault();
    onSubmit(comment.trim());
  }
  return (
    <Dialog open title={transition.name} onClose={onClose} attrs={{ "data-transition-dialog": "" }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-sm text-ink-muted">{t.workflows.leadsTo(transition.toStatus.name)}</p>
        <Field
          label={t.workflows.why}
          rows={TRANSITION_COMMENT_ROWS}
          value={comment}
          maxLength={REVISION_COMMENT_MAX_LENGTH}
          onChange={(event) => setComment(event.target.value)}
          error={error}
          autoFocus
        />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={pending} disabled={!comment.trim()} data-action="confirm-transition">
            {transition.name}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

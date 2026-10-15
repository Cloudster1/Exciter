import { useState, type FormEvent, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useComparison } from "@/api/history";
import { useClearances, useClearLink, type MovedEnd } from "@/api/links";
import { Button, Dialog, ErrorBanner, Field, Skeleton, Tag } from "@/components/ui";
import { CLEAR_COMMENT_MAX_LENGTH } from "@/config";
import { ComparisonView } from "@/features/history/RequirementHistory";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

// Suspect links (#36): the badge, and checking a link by reading what changed before confirming it.

const clearedOn = localDateFormat({ dateStyle: "medium" });

/** A link waiting to be checked since an end moved on; with a count, how many of a requirement's links do. */
export function SuspectBadge({ count, hint = t.suspect.badgeHint }: { count?: number; hint?: string }) {
  return (
    <Tag className="border-warning bg-warning-subtle text-ink" title={hint} data-suspect-badge={count ?? ""}>
      {count === undefined ? t.suspect.badge : t.suspect.count(count)}
    </Tag>
  );
}

/** One end that moved on, from the revision confirmed to the one it has now. */
export interface Change {
  reqKey: string;
  from: number;
  to: number;
}

/** Checking a link: what changed at each end that moved, the confirmations before, and why it still holds. */
export function ClearDialog({
  title,
  intro,
  changes,
  linkId,
  pending,
  error,
  onConfirm,
  onClose,
  children,
}: {
  title: string;
  intro: string;
  changes: Change[];
  /** What else to read before confirming, such as a page in Stator. */
  children?: ReactNode;
  /** A trace link's id, whose earlier confirmations are listed. */
  linkId?: string;
  pending: boolean;
  error: Error | null;
  onConfirm: (comment: string) => void;
  onClose: () => void;
}) {
  const [comment, setComment] = useState("");
  const fields = error instanceof ApiError ? error.fields : {};
  const refusal = error
    ? (fields.sourceRevision ?? fields.targetRevision ?? fields.revision ?? fields.pageVersion ?? (fields.comment ? "" : error.message))
    : "";

  function submit(event: FormEvent) {
    event.preventDefault();
    onConfirm(comment.trim());
  }

  return (
    <Dialog title={title} fill onClose={onClose} data-clear-dialog="">
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">{intro}</p>
        {children}
        {changes.map((change) => (
          <section key={change.reqKey} aria-labelledby={`moved-${change.reqKey}`} className="space-y-2" data-moved-end={change.reqKey}>
            <h3 id={`moved-${change.reqKey}`} className="text-sm font-semibold text-ink">
              {t.suspect.moved(change.reqKey, change.from, change.to)}
            </h3>
            <EndChange change={change} />
          </section>
        ))}
        {linkId && <Earlier linkId={linkId} />}
        <form onSubmit={submit} className="space-y-3 border-t border-border pt-3" aria-label={title}>
          <Field
            id="clear-comment"
            label={t.suspect.comment}
            rows={2}
            maxLength={CLEAR_COMMENT_MAX_LENGTH}
            value={comment}
            error={fields.comment}
            onChange={(event) => setComment(event.target.value)}
          />
          {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
          <div className="flex gap-2">
            <Button type="submit" loading={pending} disabled={!comment.trim()} data-action="confirm-link">
              {t.suspect.confirm}
            </Button>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t.suspect.cancel}
            </Button>
          </div>
        </form>
      </div>
    </Dialog>
  );
}

function EndChange({ change }: { change: Change }) {
  const { data, error } = useComparison(change.reqKey, change.from, change.to);
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return <ComparisonView comparison={data} />;
}

function Earlier({ linkId }: { linkId: string }) {
  const { data } = useClearances(linkId);
  if (!data || data.length === 0) return null;
  return (
    <div className="space-y-1">
      <h3 className="text-sm font-semibold text-ink">{t.suspect.earlier}</h3>
      <ul className="space-y-1 text-sm" aria-label={t.suspect.earlier}>
        {data.map((c) => (
          <li key={c.id} data-clearance={c.id}>
            <span className="text-ink-muted">
              {t.suspect.earlierRow(c.clearedByName, clearedOn.format(new Date(c.clearedAt)), c.sourceRevision, c.targetRevision)}:
            </span>{" "}
            {c.comment}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A requirement at one end of a link, as the reader holds it now. */
export interface LinkEnd {
  key: string;
  revision: number;
}

/** A trace link to check, both ends in their places, from a requirement's links or the project's list. */
export interface CheckedLink {
  id: string;
  forward: string;
  source: LinkEnd;
  target: LinkEnd;
  ends: MovedEnd[];
}

/** Checks a trace link and confirms it against both ends as they are now. */
export function ClearLinkDialog({ link, onCleared, onClose }: { link: CheckedLink; onCleared: () => void; onClose: () => void }) {
  const clear = useClearLink();
  const changes = link.ends.map((moved) => ({
    reqKey: moved.end === "source" ? link.source.key : link.target.key,
    from: moved.reviewed,
    to: moved.current,
  }));
  return (
    <ClearDialog
      title={t.suspect.dialogTitle(link.source.key, link.forward, link.target.key)}
      intro={t.suspect.intro}
      changes={changes}
      linkId={link.id}
      pending={clear.isPending}
      error={clear.error}
      onClose={onClose}
      onConfirm={(comment) =>
        clear.mutate(
          { id: link.id, sourceRevision: link.source.revision, targetRevision: link.target.revision, comment },
          {
            onSuccess: () => {
              onCleared();
              onClose();
            },
          },
        )
      }
    />
  );
}

import { useState } from "react";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useRequirement } from "@/api/requirements";
import { useBulkRequestReview, useDecide, useReview, type BulkReviewResult, type Decision } from "@/api/reviews";
import type { SpecAddress, SpecEntry } from "@/api/tree";
import { Button, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { REVIEW_COMMENT_MAX_LENGTH } from "@/config";
import { CommentsSection } from "@/features/comments/CommentsSection";
import { resumeSigning, type Resumed } from "@/features/reviews/signing";
import { SignDialog } from "@/features/reviews/SignDialog";
import { t } from "@/i18n";

/** Orders two outlines as the document reads them, 1.10 after 1.9. */
export function byOutline(a: string, b: string): number {
  const x = a.split(".").map(Number);
  const y = b.split(".").map(Number);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? -1) - (y[i] ?? -1);
    if (d !== 0) return d;
  }
  return 0;
}

/** The entries review mode shows: what still waits, and what the reader decided since they began, each where it stands. */
export function withDecided(entries: SpecEntry[], decided: Map<string, SpecEntry>): SpecEntry[] {
  const shown = new Set(entries.map((entry) => entry.node.key));
  const kept = [...decided.values()].filter((entry) => !shown.has(entry.node.key));
  // The decided copy wins over a read from before the decision, which still says nobody decided.
  return [...entries.map((entry) => decided.get(entry.node.key) ?? entry), ...kept].sort((a, b) => byOutline(a.node.outline, b.node.outline));
}

/** How many of the reviews shown the reader has decided, out of how many. */
export function progressOf(entries: SpecEntry[]): { decided: number; total: number } {
  const theirs = entries.filter((entry) => entry.review?.approver);
  return { decided: theirs.filter((entry) => entry.review?.decision).length, total: theirs.length };
}

/** Starts and ends review mode: what waits for the reader, compared with its last approval. */
export function ReviewModeButton({ address, onChange }: { address: SpecAddress; onChange: (next: SpecAddress) => void }) {
  return address.waiting || address.session ? (
    <Button variant="secondary" onClick={() => onChange({})} data-action="leave-review">
      {t.tree.leaveReview}
    </Button>
  ) : (
    <Button variant="secondary" onClick={() => onChange({ waiting: true, changes: "approved" })} data-action="review-mode">
      {t.tree.reviewMode}
    </Button>
  );
}

/** An entry's pending review: where it stands, the reader's decision, and approve and reject where they may decide. */
export function EntryReview({ project, entry, onDecided }: { project: Project; entry: SpecEntry; onDecided: (decision: Decision) => void }) {
  const review = entry.review;
  const decide = useDecide(entry.node.key);
  // A signature the identity provider interrupted comes back here, once, to be finished (#46).
  const [resumed] = useState(() => (review ? resumeSigning(review.id) : undefined));
  const [comment, setComment] = useState(resumed?.comment ?? "");
  const [signing, setSigning] = useState<Pick<Resumed, "decision" | "proof" | "failure"> | undefined>(resumed);
  const full = useReview(signing ? review?.id : undefined);
  if (!review) return null;
  const superseded = decide.error instanceof ApiError && (decide.error.code === "review_closed" || decide.error.code === "revision_moved_on");
  const send = (decision: Decision) => {
    if (project.settings.signedApprovals) {
      setSigning({ decision });
      return;
    }
    decide.mutate(
      { reviewId: review.id, decision, revision: review.revision, comment: comment.trim() || undefined },
      {
        onSuccess: () => {
          setComment("");
          onDecided(decision);
        },
      },
    );
  };
  return (
    <div className="space-y-2 rounded-control border border-border bg-surface p-3 text-sm" data-entry-review={review.id}>
      <p className="text-ink-muted" data-review-standing={`${review.approvals}/${review.required}`}>
        {t.tree.reviewStanding(review.revision, review.approvals, review.required, review.rejections)}
      </p>
      {review.decision && (
        <p className="font-medium text-ink" data-your-decision={review.decision}>
          {t.tree.youDecided[review.decision as Decision]}
        </p>
      )}
      {superseded ? (
        <p className="text-ink-muted" data-review-superseded>
          {t.tree.superseded}
        </p>
      ) : (
        review.mayDecide && (
          <div className="space-y-2">
            <Field
              id={`review-comment-${entry.node.id}`}
              label={t.review.yourComment}
              hint={t.tree.rejectNeedsComment}
              rows={2}
              maxLength={REVIEW_COMMENT_MAX_LENGTH}
              value={comment}
              onChange={(event) => setComment(event.target.value)}
            />
            <div className="flex gap-2">
              <Button
                size="sm"
                loading={decide.isPending && decide.variables?.decision === "approve"}
                onClick={() => send("approve")}
                data-action="approve-entry"
              >
                {t.review.approve}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={!comment.trim()}
                loading={decide.isPending && decide.variables?.decision === "reject"}
                onClick={() => send("reject")}
                data-action="reject-entry"
              >
                {t.review.reject}
              </Button>
            </div>
          </div>
        )
      )}
      {decide.error && !superseded && <ErrorBanner>{decide.error.message}</ErrorBanner>}
      {signing &&
        (full.data ? (
          <SignDialog
            reqKey={entry.node.key}
            review={full.data}
            decision={signing.decision}
            comment={comment}
            proof={signing.proof}
            failure={signing.failure}
            onClose={() => setSigning(undefined)}
            onSigned={() => {
              const decision = signing.decision;
              setSigning(undefined);
              setComment("");
              onDecided(decision);
            }}
          />
        ) : (
          <Skeleton />
        ))}
    </div>
  );
}

/** An entry's comment threads, opened inside it; a new one is written on the requirement's current revision. */
export function EntryComments({ entry }: { entry: SpecEntry }) {
  const [open, setOpen] = useState(false);
  return (
    <div data-entry-comments={entry.node.key}>
      <Button variant="ghost" size="sm" aria-expanded={open} onClick={() => setOpen(!open)} data-action="entry-comments">
        {open ? t.tree.hideComments : t.tree.showComments(entry.comments.open)}
      </Button>
      {open && <OpenComments reqKey={entry.node.key} />}
    </div>
  );
}

/** Reads the requirement once its threads are opened, which is what the comments are kept with. */
function OpenComments({ reqKey }: { reqKey: string }) {
  const { data: requirement } = useRequirement(reqKey);
  return requirement ? <CommentsSection requirement={requirement} /> : <Skeleton />;
}

/** Asks for the review of requirements at the revisions the document read, and says how each went. */
export function AskForReviews({
  projectKey,
  entries,
  label,
  onDone,
}: {
  projectKey: string;
  entries: SpecEntry[];
  label: string;
  onDone: (notice: string) => void;
}) {
  const ask = useBulkRequestReview(projectKey);
  const [results, setResults] = useState<BulkReviewResult[]>([]);
  const refused = results.filter((each) => each.outcome === "refused");
  if (entries.length === 0) return null;
  return (
    <div className="flex w-full flex-wrap items-center gap-2" data-ask-reviews>
      <Button
        size="sm"
        variant="secondary"
        loading={ask.isPending}
        onClick={async () => {
          // The refetch that follows takes this button away once everything below is in review, and a callback
          // given to mutate would then never run; the promise settles either way, so the outcome is still said.
          const done = await ask
            .mutateAsync({ items: entries.map((entry) => ({ key: entry.node.key, revision: entry.node.revision })) })
            .catch(() => undefined);
          if (!done) return;
          setResults(done);
          const asked = done.filter((each) => each.outcome === "requested").length;
          const pending = done.filter((each) => each.error?.code === "review_pending").length;
          onDone(t.tree.asked(asked, pending, done.length - asked - pending));
        }}
        data-action="ask-reviews"
      >
        {label}
      </Button>
      {ask.error && <ErrorBanner>{ask.error.message}</ErrorBanner>}
      {refused.length > 0 && (
        <ul className="w-full space-y-1 text-sm" aria-label={t.tree.notAsked} data-ask-refused>
          {refused.map((each) => (
            <li key={each.key} data-not-asked={each.key} className={each.error?.code === "review_pending" ? "text-ink-muted" : "text-danger"}>
              <span className="font-mono">{each.key}</span>: {each.error?.code === "review_pending" ? t.tree.alreadyPending : each.error?.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

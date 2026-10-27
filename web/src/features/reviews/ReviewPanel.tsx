import { useState, type FormEvent } from "react";
import type { Requirement } from "@/api/requirements";
import { useDecide, useRequestReview, useReviews, useWithdrawReview, type Decision, type Review } from "@/api/reviews";
import { Button, Card, ErrorBanner, Field, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { REVIEW_COMMENT_MAX_LENGTH } from "@/config";
import { Through } from "@/components/Through";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { SignDialog } from "./SignDialog";
import { resumeSigning, type Resumed } from "./signing";

// A requirement's approval (#35): where it stands, the review now open with Approve and Reject for its approvers,
// Ask for review for its editors, and the reviews before.

const when = localDateFormat({ dateStyle: "medium" });

function standing(requirement: Requirement): string {
  const { approvedRevision, current } = requirement.approval;
  if (approvedRevision == null) return t.review.notApproved;
  return current ? t.review.approved(approvedRevision) : t.review.approvedEarlier(approvedRevision, requirement.revision);
}

/** The requirement's approval and its reviews; a section has none. */
export function ReviewPanel({ requirement }: { requirement: Requirement }) {
  const { data, error, isLoading, refetch } = useReviews(requirement.key);
  const reviews = data?.reviews ?? [];
  const pending = reviews.find((each) => each.state === "pending");
  const past = reviews.filter((each) => each.state !== "pending");
  const mayAsk = requirement.can.edit && !requirement.trashedAt && !pending;
  return (
    <Card className="space-y-3 p-4">
      <section aria-labelledby="review-title" className="space-y-3" data-review-panel="">
        <SectionTitle id="review-title">{t.review.title}</SectionTitle>
        <p
          className="text-sm text-ink"
          data-approval-standing={requirement.approval.current ? "current" : requirement.approval.approvedRevision ? "earlier" : "none"}
        >
          {standing(requirement)}
        </p>
        {error ? (
          <ErrorBanner onRetry={() => void refetch()}>{t.review.failed}</ErrorBanner>
        ) : isLoading ? (
          <Skeleton />
        ) : (
          <>
            {pending && <PendingReview requirement={requirement} review={pending} />}
            {mayAsk && <AskForReview requirement={requirement} />}
            {past.length > 0 && (
              <div className="space-y-1">
                <h3 className="text-xs font-medium text-ink-muted">{t.review.history}</h3>
                <ul className="space-y-1" aria-label={t.review.history}>
                  {past.map((each) => (
                    <li key={each.id} className="flex flex-wrap items-center gap-2 text-sm text-ink-muted" data-past-review={each.state}>
                      <Tag>{t.review.states[each.state]}</Tag>
                      <span>{t.review.pastRow(each.revision, when.format(new Date(each.requestedAt)))}</span>
                      <span>· {t.review.approvals(each.approvals, each.required)}</span>
                      {each.decisions.length > 0 && <Decisions review={each} />}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </>
        )}
      </section>
    </Card>
  );
}

function AskForReview({ requirement }: { requirement: Requirement }) {
  const ask = useRequestReview(requirement.key);
  const [open, setOpen] = useState(false);
  const [comment, setComment] = useState("");
  if (!open) {
    return (
      <Button variant="secondary" onClick={() => setOpen(true)} data-action="ask-review">
        {t.review.ask}
      </Button>
    );
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    ask.mutate({ revision: requirement.revision, comment: comment.trim() || undefined }, { onSuccess: () => setOpen(false) });
  }
  return (
    <form onSubmit={submit} className="space-y-2" aria-label={t.review.askTitle(requirement.revision)} data-ask-review="">
      <Field
        id="review-request-comment"
        label={t.review.askComment}
        rows={2}
        maxLength={REVIEW_COMMENT_MAX_LENGTH}
        value={comment}
        onChange={(event) => setComment(event.target.value)}
      />
      {ask.error && <ErrorBanner>{ask.error.message}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" loading={ask.isPending} data-action="send-review-request">
          {t.review.askSend}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            ask.reset();
            setOpen(false);
          }}
        >
          {t.review.cancel}
        </Button>
      </div>
    </form>
  );
}

/** Who decided a review and what they said, which an earlier review keeps so its author reads why it was rejected. */
function Decisions({ review }: { review: Review }) {
  return (
    <ul className="w-full space-y-1 text-sm" aria-label={t.review.decisions}>
      {review.decisions.map((d) => (
        <li key={d.reviewerId} data-review-decision={d.decision}>
          <span className="font-medium text-ink">{d.reviewerName || t.review.formerMember}</span> {t.review.decided[d.decision]}
          {d.comment && <span className="text-ink-muted">: {d.comment}</span>}
          {d.signature && (
            <p className="text-xs text-ink-muted" data-review-signature={d.signature.method}>
              {t.review.signed(d.signature.meaning, t.review.signedWith[d.signature.method] ?? d.signature.method, when.format(new Date(d.signature.signedAt)))}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}

function PendingReview({ requirement, review }: { requirement: Requirement; review: Review }) {
  const decide = useDecide(requirement.key);
  const withdraw = useWithdrawReview(requirement.key);
  // A signature the identity provider interrupted comes back here, once, to be finished (#46).
  const [resumed] = useState(() => resumeSigning(review.id));
  const [comment, setComment] = useState(resumed?.comment ?? "");
  const [signing, setSigning] = useState<Pick<Resumed, "decision" | "proof" | "failure"> | undefined>(resumed);
  const send = (decision: Decision) => {
    if (review.signed) {
      setSigning({ decision });
      return;
    }
    decide.mutate({ reviewId: review.id, decision, revision: review.revision, comment: comment.trim() || undefined }, { onSuccess: () => setComment("") });
  };
  return (
    <div className="space-y-2 rounded-overlay border border-border bg-surface p-3" data-pending-review={review.id}>
      <p className="text-sm text-ink">
        {t.review.pending(review.revision, review.requestedByName)} <Through via={review.via} />
      </p>
      {review.comment && <p className="text-sm text-ink-muted italic">{review.comment}</p>}
      <p className="text-sm text-ink-muted" data-review-approvals={`${review.approvals}/${review.required}`}>
        {t.review.approvals(review.approvals, review.required)}
      </p>
      <div className="text-sm">
        <span className="text-ink-muted">{t.review.approvers}: </span>
        <span data-review-approvers="">{review.approvers.map((a) => a.name).join(", ")}</span>
      </div>
      {review.decisions.length > 0 && <Decisions review={review} />}
      {review.can.decide && (
        <div className="space-y-2">
          <Field
            id="review-decision-comment"
            label={t.review.yourComment}
            rows={2}
            maxLength={REVIEW_COMMENT_MAX_LENGTH}
            value={comment}
            onChange={(event) => setComment(event.target.value)}
          />
          <div className="flex gap-2">
            <Button loading={decide.isPending && decide.variables?.decision === "approve"} onClick={() => send("approve")} data-action="approve-review">
              {t.review.approve}
            </Button>
            <Button
              variant="secondary"
              loading={decide.isPending && decide.variables?.decision === "reject"}
              onClick={() => send("reject")}
              data-action="reject-review"
            >
              {t.review.reject}
            </Button>
          </div>
        </div>
      )}
      {decide.error && <ErrorBanner>{decide.error.message}</ErrorBanner>}
      {signing && (
        <SignDialog
          reqKey={requirement.key}
          review={review}
          decision={signing.decision}
          comment={comment}
          proof={signing.proof}
          failure={signing.failure}
          onClose={() => setSigning(undefined)}
          onSigned={() => {
            setSigning(undefined);
            setComment("");
          }}
        />
      )}
      {review.can.withdraw && (
        <Button
          size="sm"
          variant="ghost"
          loading={withdraw.isPending}
          onClick={() => {
            if (window.confirm(t.review.confirmWithdraw)) withdraw.mutate(review.id);
          }}
          data-action="withdraw-review"
        >
          {t.review.withdraw}
        </Button>
      )}
      {withdraw.error && <ErrorBanner>{withdraw.error.message}</ErrorBanner>}
    </div>
  );
}

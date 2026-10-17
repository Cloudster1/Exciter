import { ApiError } from "@/api/client";
import { useDecide, type Decision, type Review } from "@/api/reviews";
import { t } from "@/i18n";
import { SignatureDialog } from "./SignatureDialog";
import { keepSigning } from "./signing";

/** Signs a decision on a review whose project asks for electronic signatures (#46). */
export function SignDialog({
  reqKey,
  review,
  decision,
  comment,
  proof,
  failure,
  onClose,
  onSigned,
}: {
  reqKey: string;
  review: Review;
  decision: Decision;
  comment: string;
  /** The re-authentication a sign-in at the provider brought back, to sign with now. */
  proof?: string;
  /** Why a sign-in at the provider brought none. */
  failure?: string;
  onClose: () => void;
  onSigned: () => void;
}) {
  const decide = useDecide(reqKey);
  const verb = t.review.decisionVerb[decision] ?? decision;
  const fields = decide.error instanceof ApiError ? decide.error.fields : {};

  // The provider brings the browser back to this page, which the address tells which signature to finish.
  const back = new URL(window.location.href);
  back.searchParams.set("sign", decision);
  back.searchParams.set("review", review.id);

  return (
    <SignatureDialog
      kind={decision}
      title={t.review.signTitle(verb)}
      intro={t.review.signIntro}
      meaning={review.meanings[decision] ?? ""}
      action={proof ? t.review.signAfterProvider(verb) : t.review.signWithPassword(verb)}
      pending={decide.isPending}
      refusal={fields.password ?? decide.error?.message}
      proof={proof}
      failure={failure}
      back={back}
      onLeave={() => keepSigning(review.id, decision, comment)}
      onSign={(body) =>
        decide.mutate({ reviewId: review.id, decision, revision: review.revision, comment: comment.trim() || undefined, ...body }, { onSuccess: onSigned })
      }
      onClose={onClose}
    />
  );
}

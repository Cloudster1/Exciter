import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { reauthStartURL, useMe } from "@/api/auth";
import { useDecide, type Decision, type Review } from "@/api/reviews";
import { Button, ButtonLink, Dialog, ErrorBanner, Input } from "@/components/ui";
import { t } from "@/i18n";
import { keepSigning } from "./signing";

/**
 * Signs a decision on a review whose project asks for electronic signatures (#46): the meaning the server wrote, then
 * the password, or a fresh sign-in at the identity provider, which comes back here with the proof to sign with.
 */
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
  const { data: me } = useMe();
  const decide = useDecide(reqKey);
  const [password, setPassword] = useState("");
  const verb = t.review.decisionVerb[decision] ?? decision;
  const fields = decide.error instanceof ApiError ? decide.error.fields : {};
  const refusal = fields.password ?? (decide.error && !fields.password ? decide.error.message : undefined);

  function sign(body: { password?: string; reauthentication?: string }) {
    decide.mutate({ reviewId: review.id, decision, revision: review.revision, comment: comment.trim() || undefined, ...body }, { onSuccess: onSigned });
  }

  function withPassword(event: FormEvent) {
    event.preventDefault();
    if (password) sign({ password });
  }

  // The provider brings the browser back to this page, which the address tells which signature to finish.
  const back = new URL(window.location.href);
  back.searchParams.set("sign", decision);
  back.searchParams.set("review", review.id);

  return (
    <Dialog title={t.review.signTitle(verb)} onClose={onClose} data-sign-dialog={decision}>
      <div className="space-y-3">
        <p className="text-sm text-ink-muted">{t.review.signIntro}</p>
        <figure className="rounded-control border border-border bg-surface-raised p-3">
          <figcaption className="text-xs font-semibold uppercase text-ink-muted">{t.review.signMeaning}</figcaption>
          <p className="mt-1 text-sm text-ink" data-sign-meaning="">
            {review.meanings[decision]}
          </p>
        </figure>
        {failure && <ErrorBanner>{t.review.signatureError[failure] ?? t.review.signatureError.unavailable}</ErrorBanner>}
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        {proof ? (
          <div className="space-y-2">
            <p role="status" className="text-sm text-ink">
              {t.review.signedInAgain}
            </p>
            <Button loading={decide.isPending} onClick={() => sign({ reauthentication: proof })} data-action="sign-after-provider">
              {t.review.signAfterProvider(verb)}
            </Button>
          </div>
        ) : me?.signsWith === "password" ? (
          <form onSubmit={withPassword} className="space-y-2" aria-label={t.review.signTitle(verb)}>
            <label htmlFor="sign-password" className="block text-sm font-medium text-ink">
              {t.review.signPassword}
            </label>
            <Input
              id="sign-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              aria-describedby="sign-password-hint"
              data-sign-password=""
            />
            <p id="sign-password-hint" className="text-xs text-ink-muted">
              {t.review.signPasswordHint}
            </p>
            <Button type="submit" loading={decide.isPending} disabled={!password} data-action="sign-with-password">
              {t.review.signWithPassword(verb)}
            </Button>
          </form>
        ) : me?.signsWith === "provider" ? (
          <div className="space-y-2">
            <p className="text-sm text-ink-muted">{t.review.signProviderHint}</p>
            <ButtonLink
              variant="primary"
              href={reauthStartURL(back.pathname + back.search)}
              onClick={() => keepSigning(review.id, decision, comment)}
              data-action="sign-with-provider"
            >
              {t.review.signWithProvider}
            </ButtonLink>
          </div>
        ) : (
          <p className="text-sm text-ink" data-cannot-sign="">
            {t.review.cannotSign}
          </p>
        )}
        <div className="flex justify-end">
          <Button variant="secondary" onClick={onClose} data-action="cancel-sign">
            {t.review.cancelSign}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

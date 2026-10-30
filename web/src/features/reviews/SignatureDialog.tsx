import { useState, type FormEvent } from "react";
import { reauthStartURL, useMe } from "@/api/auth";
import { Button, ButtonLink, Dialog, ErrorBanner, Input, DialogFooter } from "@/components/ui";
import { t } from "@/i18n";

/**
 * An electronic signature (#46, #205): the meaning the server wrote, then the password, or a fresh sign-in at the
 * identity provider, which comes back to `back` with the proof to sign with.
 */
export function SignatureDialog({
  title,
  intro,
  meaning,
  action,
  pending,
  refusal,
  proof,
  failure,
  back,
  onLeave,
  onSign,
  onClose,
  kind,
}: {
  title: string;
  intro: string;
  meaning: string;
  /** What the sign button says, such as Sign and approve. */
  action: string;
  pending: boolean;
  refusal?: string;
  /** The re-authentication a sign-in at the provider brought back, to sign with now. */
  proof?: string;
  /** Why a sign-in at the provider brought none. */
  failure?: string;
  /** Where the provider brings the browser back to, which says which signature to finish. */
  back: URL;
  /** Keeps what the provider's round trip would lose. */
  onLeave: () => void;
  onSign: (body: { password?: string; reauthentication?: string }) => void;
  onClose: () => void;
  /** What is signed, for the dialog's data attribute. */
  kind: string;
}) {
  const { data: me } = useMe();
  const [password, setPassword] = useState("");

  function withPassword(event: FormEvent) {
    event.preventDefault();
    if (password) onSign({ password });
  }

  return (
    <Dialog open title={title} onClose={onClose} attrs={{ "data-sign-dialog": kind }}>
      <div className="space-y-3">
        <p className="text-sm text-ink-muted">{intro}</p>
        <figure className="rounded-control border border-border bg-surface-raised p-3">
          <figcaption className="text-xs font-semibold uppercase text-ink-muted">{t.review.signMeaning}</figcaption>
          <p className="mt-1 text-sm text-ink" data-sign-meaning="">
            {meaning}
          </p>
        </figure>
        {failure && <ErrorBanner>{t.review.signatureError[failure] ?? t.review.signatureError.unavailable}</ErrorBanner>}
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        {proof ? (
          <div className="space-y-2">
            <p role="status" className="text-sm text-ink">
              {t.review.signedInAgain}
            </p>
            <Button loading={pending} onClick={() => onSign({ reauthentication: proof })} data-action="sign-after-provider">
              {action}
            </Button>
          </div>
        ) : me?.signsWith === "password" ? (
          <form onSubmit={withPassword} className="space-y-2" aria-label={title}>
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
            <Button type="submit" loading={pending} disabled={!password} data-action="sign-with-password">
              {action}
            </Button>
          </form>
        ) : me?.signsWith === "provider" ? (
          <div className="space-y-2">
            <p className="text-sm text-ink-muted">{t.review.signProviderHint}</p>
            <ButtonLink variant="primary" href={reauthStartURL(back.pathname + back.search)} onClick={onLeave} data-action="sign-with-provider">
              {t.review.signWithProvider}
            </ButtonLink>
          </div>
        ) : (
          <p className="text-sm text-ink" data-cannot-sign="">
            {t.review.cannotSign}
          </p>
        )}
        <DialogFooter>
          <Button variant="secondary" onClick={onClose} data-action="cancel-sign">
            {t.review.cancelSign}
          </Button>
        </DialogFooter>
      </div>
    </Dialog>
  );
}

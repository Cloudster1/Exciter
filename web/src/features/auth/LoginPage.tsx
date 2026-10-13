import { useState, type FormEvent } from "react";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { ssoStartURL, useLogin } from "@/api/auth";
import { Button, ButtonLink, Card, ErrorBanner, Field } from "@/components/ui";
import { Logo } from "@/components/Logo";
import { APP_NAME, SSO_WAITING } from "@/config";
import { t } from "@/i18n";
import { safeNext } from "@/lib/session";

/** The reason the API put on the address after a failed sign-in, as a sentence. */
export function ssoFailure(reason: string | undefined): string | null {
  if (!reason) return null;
  const known: Record<string, string | undefined> = t.login.failures;
  return known[reason] ?? t.login.failures.failed;
}

/** The way in: the installation's identity provider, or a password for somebody given one here. */
export function LoginPage({ next, sso }: { next?: string; sso?: string }) {
  const navigate = useNavigate();
  const router = useRouter();
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const target = safeNext(next);
  // Not a failure: the provider let them through and an administrator has yet to let them in, told as news.
  const waiting = sso === SSO_WAITING;
  const failure = waiting ? null : ssoFailure(sso);
  const fields = login.error instanceof ApiError ? login.error.fields : {};
  const formError = login.error && Object.keys(fields).length === 0 ? login.error.message : null;

  function signIn(event: FormEvent) {
    event.preventDefault();
    login.mutate({ email: email.trim(), password }, { onSuccess: () => (target ? router.history.push(target) : navigate({ to: "/" })) });
  }

  return (
    <main className="flex min-h-full items-center justify-center bg-backdrop px-4 py-12" data-login>
      <div className="w-full max-w-sm">
        <div className="mb-6">
          <Logo className="mb-3 rounded-overlay" />
          <p className="font-mono text-sm font-medium tracking-wide text-ink-muted">{APP_NAME}</p>
          <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">{t.login.title}</h1>
          <p className="mt-1 text-sm text-ink-muted">{t.login.subtitle}</p>
        </div>

        <Card className="space-y-4 p-5">
          {waiting && (
            <div role="status" className="rounded-control border border-border bg-surface-raised px-3 py-2" data-sso-waiting>
              <h2 className="text-sm font-semibold text-ink">{t.login.waitingTitle}</h2>
              <p className="mt-1 text-sm text-ink-muted">{t.login.waitingBody}</p>
            </div>
          )}
          {failure && <ErrorBanner>{failure}</ErrorBanner>}
          {/* A navigation, not a fetch: the browser has to follow the provider's redirects. */}
          <ButtonLink variant="primary" className="w-full" href={ssoStartURL(target)} data-action="sso">
            {t.login.sso}
          </ButtonLink>
        </Card>

        <Card className="mt-5 p-5">
          <form onSubmit={signIn} className="space-y-4" noValidate aria-labelledby="password-sign-in">
            <div>
              <h2 id="password-sign-in" className="text-sm font-semibold text-ink">
                {t.login.passwordTitle}
              </h2>
              <p className="mt-1 text-sm text-ink-muted">{t.login.passwordHint}</p>
            </div>
            {formError && <ErrorBanner>{formError}</ErrorBanner>}
            <Field
              label={t.login.email}
              type="email"
              autoComplete="username"
              required
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              error={fields.email}
            />
            <Field
              label={t.login.password}
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              error={fields.password}
            />
            <Button type="submit" variant="secondary" className="w-full" loading={login.isPending} data-action="password-sign-in">
              {t.login.submit}
            </Button>
          </form>
        </Card>
      </div>
    </main>
  );
}

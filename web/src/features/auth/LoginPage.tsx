import { useState, type FormEvent, type MouseEvent } from "react";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { ssoStartURL, useLogin } from "@/api/auth";
import { Button, ButtonLink, Card, ErrorBanner, Field } from "@/components/ui";
import { Logo } from "@/components/Logo";
import { APP_NAME, LAST_ORG_KEY, SSO_WAITING } from "@/config";
import { t } from "@/i18n";
import { safeNext } from "@/lib/session";

function readLastOrg(): string {
  try {
    return window.localStorage.getItem(LAST_ORG_KEY) ?? "";
  } catch {
    return "";
  }
}

function rememberOrg(slug: string) {
  try {
    window.localStorage.setItem(LAST_ORG_KEY, slug);
  } catch {
    // A browser that refuses storage asks for the organization every time.
  }
}

/** The reason the API put on the address after a failed sign-in, as a sentence. */
export function ssoFailure(reason: string | undefined): string | null {
  if (!reason) return null;
  const known: Record<string, string | undefined> = t.login.failures;
  return known[reason] ?? t.login.failures.failed;
}

/** The way in: the organization's identity provider, or a password for somebody given one here. */
export function LoginPage({ next, sso, org: named }: { next?: string; sso?: string; org?: string }) {
  const navigate = useNavigate();
  const router = useRouter();
  const login = useLogin();
  const [org, setOrg] = useState(() => named ?? readLastOrg());
  const [orgError, setOrgError] = useState<string>();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const target = safeNext(next);
  // Not a failure: the provider let them through and an administrator has yet to let them in, told as news.
  const waiting = sso === SSO_WAITING;
  const failure = waiting ? null : ssoFailure(sso);
  const fields = login.error instanceof ApiError ? login.error.fields : {};
  const formError = login.error && Object.keys(fields).length === 0 ? login.error.message : null;

  // A navigation, not a fetch: the browser has to follow the provider's redirects.
  function startSSO(event: MouseEvent<HTMLAnchorElement>) {
    if (!org.trim()) {
      event.preventDefault();
      setOrgError(t.login.organizationMissing);
      return;
    }
    rememberOrg(org.trim());
  }

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
          <Field
            label={t.login.organization}
            placeholder={t.login.organizationPlaceholder}
            autoComplete="organization"
            autoCapitalize="none"
            spellCheck={false}
            value={org}
            onChange={(event) => {
              setOrg(event.target.value);
              setOrgError(undefined);
            }}
            hint={t.login.organizationHint}
            error={orgError}
          />
          <ButtonLink variant="primary" className="w-full" href={ssoStartURL(org, target)} onClick={startSSO} data-action="sso">
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

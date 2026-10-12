import { useState, type FormEvent } from "react";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useLogin } from "@/api/auth";
import { Button, Card, ErrorBanner, Field } from "@/components/ui";
import { Logo } from "@/components/Logo";
import { APP_NAME } from "@/config";
import { t } from "@/i18n";
import { safeNext } from "@/lib/session";

/** The way in: an email address and a password. Single sign-on joins it with #7. */
export function LoginPage({ next }: { next?: string }) {
  const navigate = useNavigate();
  const router = useRouter();
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const target = safeNext(next);
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
          <Logo className="mb-3" />
          <p className="font-mono text-sm font-medium tracking-wide text-ink-muted">{APP_NAME}</p>
          <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">{t.login.title}</h1>
          <p className="mt-1 text-sm text-ink-muted">{t.login.subtitle}</p>
        </div>

        <Card className="p-5">
          <form onSubmit={signIn} className="space-y-4" noValidate aria-label={t.login.title}>
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
            <Button type="submit" variant="primary" className="w-full" loading={login.isPending} data-action="sign-in">
              {t.login.submit}
            </Button>
          </form>
        </Card>
      </div>
    </main>
  );
}

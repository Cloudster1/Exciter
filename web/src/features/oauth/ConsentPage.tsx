import { useState } from "react";
import { ApiError } from "@/api/client";
import { useAuthorizationRequest, useDecideAuthorization, type AuthorizationRequest } from "@/api/oauth";
import { Button, Card, Checkbox, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { ProjectPicker } from "@/features/tokens/ProjectPicker";
import { t } from "@/i18n";
import { leaveTo } from "@/lib/leave";

/** Where a person lets an app connect as them, or refuses it, and where they read why an app was refused (#188). */
export function ConsentPage({ request, error }: { request?: string; error?: string }) {
  return (
    <div className="mx-auto max-w-2xl">
      <PageHeader title={t.oauth.title} />
      {error ? (
        <ErrorBanner>{t.oauth.refused[error] ?? t.oauth.refusedOther}</ErrorBanner>
      ) : request ? (
        <PendingRequest id={request} />
      ) : (
        <ErrorBanner>{t.oauth.noRequest}</ErrorBanner>
      )}
    </div>
  );
}

function PendingRequest({ id }: { id: string }) {
  const { data, isLoading, error } = useAuthorizationRequest(id);
  if (isLoading) return <Skeleton />;
  if (error instanceof ApiError && error.status === 404) return <ErrorBanner>{t.oauth.expired}</ErrorBanner>;
  if (error || !data) return <ErrorBanner>{error?.message}</ErrorBanner>;
  return <Consent request={data} />;
}

function Consent({ request }: { request: AuthorizationRequest }) {
  const decide = useDecideAuthorization(request.id);
  // A client that asked only to read cannot be granted more, so the box is ticked and stays so.
  const [readOnly, setReadOnly] = useState(request.readOnly);
  const [projects, setProjects] = useState<string[]>([]);
  const [leaving, setLeaving] = useState(false);
  const fields = decide.error instanceof ApiError ? decide.error.fields : {};
  const expired = decide.error instanceof ApiError && decide.error.status === 404;
  const formError = decide.error && !expired && Object.keys(fields).length === 0 ? decide.error.message : null;
  const answer = (allow: boolean) =>
    decide.mutate(allow ? { allow, readOnly, ...(projects.length > 0 ? { projects } : {}) } : { allow, readOnly: false }, {
      onSuccess: (to) => {
        setLeaving(true);
        leaveTo(to);
      },
    });
  const address = request.client.uri || request.client.id;

  return (
    <Card className="space-y-4 p-4" data-consent={request.client.id}>
      <p className="text-base font-medium text-ink">{t.oauth.asks(request.client.name)}</p>
      <p className="text-sm text-ink-muted">{t.oauth.mayDo}</p>
      <div className="space-y-1 text-sm">
        <p>
          <span className="font-medium text-ink">{t.oauth.address}: </span>
          <span className="break-all font-mono text-ink" data-consent-address>
            {address}
          </span>
        </p>
        <p className="text-ink-muted">{request.client.kind === "metadata" ? t.oauth.knownMetadata : t.oauth.knownRegistered}</p>
        <p className="text-ink-muted" data-consent-host>
          {t.oauth.goesBack(request.redirectHost)}
        </p>
        {request.readOnly && <p className="text-ink-muted">{t.oauth.askedToRead}</p>}
        {request.evidence && (
          <p className="text-ink-muted" data-consent-evidence>
            {t.oauth.askedForEvidence}
          </p>
        )}
      </div>
      {!request.evidence && (
        <div>
          <Checkbox
            label={t.oauth.readOnly}
            checked={readOnly}
            disabled={request.readOnly}
            onChange={(event) => setReadOnly(event.target.checked)}
            aria-describedby="consent-read-only-hint"
            data-consent-read-only=""
          />
          <p id="consent-read-only-hint" className="text-xs text-ink-muted">
            {t.oauth.readOnlyHint}
          </p>
          {fields.readOnly && <ErrorBanner>{fields.readOnly}</ErrorBanner>}
        </div>
      )}
      <ProjectPicker id="consent-projects" picked={projects} onChange={setProjects} some={t.oauth.projectsSome} error={fields.projects} />
      {expired && <ErrorBanner>{t.oauth.expired}</ErrorBanner>}
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <div className="flex items-center gap-3">
        <Button loading={decide.isPending && decide.variables?.allow} disabled={leaving || expired} onClick={() => answer(true)} data-action="allow-app">
          {t.oauth.allow}
        </Button>
        <Button
          variant="secondary"
          loading={decide.isPending && !decide.variables?.allow}
          disabled={leaving || expired}
          onClick={() => answer(false)}
          data-action="deny-app"
        >
          {t.oauth.deny}
        </Button>
        <p role="status" className="text-sm text-ink-muted empty:hidden">
          {leaving ? t.oauth.leaving : ""}
        </p>
      </div>
    </Card>
  );
}

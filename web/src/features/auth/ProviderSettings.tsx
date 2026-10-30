import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { ssoStartURL, useProvider, useSaveProvider, type Provider, type SaveProviderInput } from "@/api/auth";
import { Button, Card, Checkbox, ErrorBanner, Field, Skeleton, useToast } from "@/components/ui";
import { HTTP_FORBIDDEN, SSO_DEFAULT_GROUPS_CLAIM, SSO_DEFAULT_SCOPES } from "@/config";
import { t } from "@/i18n";

// Adapted from Armature's and Stator's ProviderSettings.

interface Draft {
  issuer: string;
  clientId: string;
  clientSecret: string;
  groupsClaim: string;
  admitByGroup: boolean;
}

function draftOf(provider: Provider | null | undefined): Draft {
  return {
    issuer: provider?.issuer ?? "",
    clientId: provider?.clientId ?? "",
    clientSecret: "",
    groupsClaim: provider?.groupsClaim ?? SSO_DEFAULT_GROUPS_CLAIM,
    admitByGroup: provider?.admitByGroup ?? false,
  };
}

/** The installation's identity provider, the Single sign-on tab of Access. */
export function ProviderSettings() {
  const { data, isLoading, error } = useProvider();
  // Held here rather than in the form: a save remounts the form, and its error has to outlive the remount.
  const save = useSaveProvider();
  if (isLoading) return <Skeleton />;
  if (error) return <ErrorBanner>{error instanceof ApiError && error.status === HTTP_FORBIDDEN ? t.sso.notAdmin : error.message}</ErrorBanner>;
  // Keyed by the saved row, so a save that comes back starts the form afresh.
  return <ProviderForm key={data?.provider?.updatedAt ?? "new"} provider={data?.provider ?? null} save={save} />;
}

// The client secret is write-only: it never comes back, and a blank field keeps the stored one, so saving the rest
// of the form cannot erase it. The scopes and creating groups are dynamo-connect's to set, so the form sends back
// what is stored.
function ProviderForm({ provider, save }: { provider: Provider | null; save: ReturnType<typeof useSaveProvider> }) {
  const [draft, setDraft] = useState(() => draftOf(provider));
  const toast = useToast();
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;
  const address = `${window.location.origin}${ssoStartURL()}`;

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  function send(body: SaveProviderInput, done: string) {
    save.mutate(body, { onSuccess: () => toast.success(done) });
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const { clientSecret, ...rest } = draft;
    const kept = { scopes: provider?.scopes ?? SSO_DEFAULT_SCOPES, createGroups: provider?.createGroups ?? false };
    send({ ...rest, ...kept, ...(clientSecret ? { clientSecret } : {}), enabled: true }, t.sso.saved);
  }

  function turnOff() {
    if (!provider) return;
    const { issuer, clientId, groupsClaim, scopes, createGroups, admitByGroup } = provider;
    send({ issuer, clientId, groupsClaim, scopes, createGroups, admitByGroup, enabled: false }, t.sso.turnedOff);
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.sso.title}>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.sso.intro}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field
            label={t.sso.issuer}
            value={draft.issuer}
            placeholder="https://id.example.com/realms/acme"
            hint={t.sso.issuerHint}
            error={fields.issuer}
            onChange={(event) => set("issuer", event.target.value)}
          />
          <Field label={t.sso.clientId} value={draft.clientId} error={fields.clientId} onChange={(event) => set("clientId", event.target.value)} />
          <Field
            label={t.sso.clientSecret}
            type="password"
            autoComplete="off"
            value={draft.clientSecret}
            placeholder={provider?.hasSecret ? t.sso.clientSecretStored : ""}
            error={fields.clientSecret}
            onChange={(event) => set("clientSecret", event.target.value)}
          />
          <Field
            label={t.sso.groupsClaim}
            value={draft.groupsClaim}
            hint={t.sso.groupsClaimHint}
            error={fields.groupsClaim}
            onChange={(event) => set("groupsClaim", event.target.value)}
          />
        </div>
        <div>
          <Checkbox
            label={t.sso.admitByGroup}
            checked={draft.admitByGroup}
            aria-describedby="sso-admit-by-group"
            onChange={(event) => set("admitByGroup", event.target.checked)}
            data-admit-by-group
          />
          <p id="sso-admit-by-group" className="pl-6 text-xs text-ink-muted">
            {t.sso.admitByGroupDetail}
          </p>
        </div>
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" loading={save.isPending} data-action="save-sso">
            {provider ? t.sso.save : t.sso.setUp}
          </Button>
          {provider?.enabled && (
            <Button type="button" variant="ghost" disabled={save.isPending} onClick={turnOff} data-action="turn-off-sso">
              {t.sso.turnOff}
            </Button>
          )}
          {provider && (
            <span className="text-sm text-ink-muted" data-sso-state={provider.enabled ? "on" : "off"}>
              {provider.enabled ? t.sso.signInAt : t.sso.offAddress}{" "}
              <code className="rounded bg-surface-raised px-1 font-mono text-xs break-all text-ink" data-sso-start>
                {address}
              </code>
            </span>
          )}
        </div>
      </Card>
    </form>
  );
}

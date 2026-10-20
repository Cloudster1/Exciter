import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useRemoveStatorConnection, useSaveStatorConnection, useStatorConnection, type StatorConnection, type StatorConnectionInput } from "@/api/stator";
import { Button, Card, Checkbox, ErrorBanner, Field, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COPY_FEEDBACK_MS, STATOR_WEBHOOK_SECRET_PREFIX } from "@/config";
import { t } from "@/i18n";

/** The installation's Stator instance and its webhook, for administrators, as Armature's page. */
export function ConnectionSettings() {
  const { data, isLoading, error } = useStatorConnection();
  // Held here rather than in the form: a save remounts the form, and what it
  // says about that save has to outlive the remount.
  const save = useSaveStatorConnection();
  const remove = useRemoveStatorConnection();
  const [notice, setNotice] = useState("");
  if (isLoading) return <Skeleton />;
  if (error) return <ErrorBanner>{error instanceof ApiError && error.status === 403 ? t.stator.notAdmin : error.message}</ErrorBanner>;
  const connection = data ?? null;

  function disconnect() {
    if (!window.confirm(t.stator.confirmDisconnect)) return;
    setNotice("");
    remove.mutate(undefined, { onSuccess: () => setNotice(t.stator.disconnected) });
  }

  return (
    <div className="space-y-6">
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-stator-notice>
        {notice}
      </p>
      <ConnectionForm key={connection?.updatedAt ?? "new"} connection={connection} save={save} onSaved={setNotice} />
      {connection && <Webhook connection={connection} />}
      {connection && (
        <section aria-labelledby="stator-disconnect" className="space-y-2">
          <SectionTitle id="stator-disconnect">{t.stator.disconnectTitle}</SectionTitle>
          <p className="text-sm text-ink-muted">{t.stator.disconnectIntro}</p>
          {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
          <Button variant="danger" onClick={disconnect} loading={remove.isPending} data-action="disconnect-stator">
            {t.stator.disconnect}
          </Button>
        </section>
      )}
    </div>
  );
}

// The webhook secret is write-only: it never comes back, and a blank field
// keeps the stored one, so saving the address cannot erase it.
function ConnectionForm({
  connection,
  save,
  onSaved,
}: {
  connection: StatorConnection | null;
  save: ReturnType<typeof useSaveStatorConnection>;
  onSaved: (notice: string) => void;
}) {
  const [baseUrl, setBaseUrl] = useState(connection?.baseUrl ?? "");
  const [secret, setSecret] = useState("");
  const [forget, setForget] = useState(false);
  const [clientId, setClientId] = useState(connection?.clientId ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [forgetClient, setForgetClient] = useState(false);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    onSaved("");
    const body: StatorConnectionInput = { baseUrl: baseUrl.trim() };
    if (secret.trim()) body.webhookSecret = secret.trim();
    else if (forget) body.webhookSecret = "";
    if (clientId.trim() !== (connection?.clientId ?? "")) body.clientId = clientId.trim();
    if (clientSecret.trim()) body.clientSecret = clientSecret.trim();
    else if (forgetClient) body.clientSecret = "";
    save.mutate(body, { onSuccess: () => onSaved(t.stator.saved) });
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.stator.title} data-stator-connection>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.stator.intro}</p>
        <Field
          label={t.stator.baseUrl}
          value={baseUrl}
          type="url"
          placeholder="https://stator.example.com"
          hint={t.stator.baseUrlHint}
          error={fields.baseUrl}
          onChange={(event) => setBaseUrl(event.target.value)}
        />
        <Field
          label={t.stator.webhookSecret}
          type="password"
          autoComplete="off"
          value={secret}
          placeholder={connection?.webhookSecretSet ? t.stator.secretStored : `${STATOR_WEBHOOK_SECRET_PREFIX}...`}
          error={fields.webhookSecret}
          onChange={(event) => setSecret(event.target.value)}
        />
        {connection?.webhookSecretSet && <Checkbox label={t.stator.forgetSecret} checked={forget} onChange={(event) => setForget(event.target.checked)} />}
        {connection && (
          <p className="text-sm text-ink-muted" data-stator-connected>
            {t.stator.connectedCount(connection.connected)} {connection.remoteLinks ? t.stator.remoteLinks : t.stator.noRemoteLinks}
          </p>
        )}
        <fieldset className="space-y-3" data-stator-sign-in-app>
          <legend className="text-sm font-medium text-ink">{t.stator.signIn.title}</legend>
          <p className="text-sm text-ink-muted">{t.stator.signIn.intro}</p>
          {connection && (
            <p className="text-sm text-ink-muted" data-stator-exchange={connection.tokenExchange && connection.exchangeOffered ? "on" : "off"}>
              {!connection.tokenExchange ? t.stator.signIn.off : connection.exchangeOffered ? t.stator.signIn.on : t.stator.signIn.notOffered}
            </p>
          )}
          <Field
            label={t.stator.signIn.clientId}
            value={clientId}
            autoComplete="off"
            error={fields.clientId}
            onChange={(event) => setClientId(event.target.value)}
          />
          <Field
            label={t.stator.signIn.clientSecret}
            type="password"
            autoComplete="off"
            value={clientSecret}
            placeholder={connection?.clientSecretSet ? t.stator.secretStored : ""}
            error={fields.clientSecret}
            onChange={(event) => setClientSecret(event.target.value)}
          />
          {connection?.clientSecretSet && (
            <Checkbox label={t.stator.signIn.forgetSecret} checked={forgetClient} onChange={(event) => setForgetClient(event.target.checked)} />
          )}
        </fieldset>
        {connection && <p className="text-xs text-ink-muted">{t.stator.movesForget}</p>}
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <Button type="submit" loading={save.isPending} data-action="save-stator">
          {connection ? t.stator.save : t.stator.connect}
        </Button>
      </Card>
    </form>
  );
}

function Webhook({ connection }: { connection: StatorConnection }) {
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.stator.copied;
    try {
      await navigator.clipboard.writeText(connection.webhookUrl);
    } catch {
      message = t.stator.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <section aria-labelledby="stator-webhook" className="space-y-3">
      <SectionTitle id="stator-webhook">{t.stator.webhookTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.stator.webhookIntro}</p>
      <ol className="list-decimal space-y-1 pl-5 text-sm text-ink">
        {t.stator.webhookSteps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      <Field label={t.stator.webhookUrl} value={connection.webhookUrl} readOnly className="font-mono text-xs" onFocus={(event) => event.target.select()} />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-webhook-url">
          {t.stator.copy}
        </Button>
        <p role="status" className="text-xs text-ink-muted" data-copy-status>
          {said}
        </p>
      </div>
      <div>
        <h3 className="text-xs font-medium text-ink-muted">{t.stator.topics}</h3>
        <ul className="mt-1 flex flex-wrap gap-1.5" aria-label={t.stator.topics}>
          {connection.webhookTopics.map((topic) => (
            <li key={topic}>
              <code className="rounded bg-surface-raised px-1 font-mono text-xs text-ink">{topic}</code>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

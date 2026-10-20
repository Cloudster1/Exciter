import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useArmatureConnection,
  useRemoveArmatureConnection,
  useSaveArmatureConnection,
  type ArmatureConnection,
  type ArmatureConnectionInput,
} from "@/api/armature";
import { Button, Card, Checkbox, ErrorBanner, Field, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_WEBHOOK_SECRET_PREFIX, COPY_FEEDBACK_MS } from "@/config";
import { t } from "@/i18n";
import { MissingCapabilities } from "./MissingCapabilities";

/** The installation's Armature instance and its webhook, for administrators; Stator's page. */
export function ConnectionSettings() {
  const { data, isLoading, error } = useArmatureConnection();
  // Held here rather than in the form: a save remounts the form, and what it
  // says about that save has to outlive the remount.
  const save = useSaveArmatureConnection();
  const remove = useRemoveArmatureConnection();
  const [notice, setNotice] = useState("");
  if (isLoading) return <Skeleton />;
  if (error) return <ErrorBanner>{error instanceof ApiError && error.status === 403 ? t.armature.notAdmin : error.message}</ErrorBanner>;
  const connection = data ?? null;

  function disconnect() {
    if (!window.confirm(t.armature.confirmDisconnect)) return;
    setNotice("");
    remove.mutate(undefined, { onSuccess: () => setNotice(t.armature.disconnected) });
  }

  return (
    <div className="space-y-6">
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-armature-notice>
        {notice}
      </p>
      <ConnectionForm key={connection?.updatedAt ?? "new"} connection={connection} save={save} onSaved={setNotice} />
      {connection && <Webhook connection={connection} />}
      {connection && (
        <section aria-labelledby="armature-disconnect" className="space-y-2">
          <SectionTitle id="armature-disconnect">{t.armature.disconnectTitle}</SectionTitle>
          <p className="text-sm text-ink-muted">{t.armature.disconnectIntro}</p>
          {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
          <Button variant="danger" onClick={disconnect} loading={remove.isPending} data-action="disconnect-armature">
            {t.armature.disconnect}
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
  connection: ArmatureConnection | null;
  save: ReturnType<typeof useSaveArmatureConnection>;
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
    const body: ArmatureConnectionInput = { baseUrl: baseUrl.trim() };
    if (secret.trim()) body.webhookSecret = secret.trim();
    else if (forget) body.webhookSecret = "";
    if (clientId.trim() !== (connection?.clientId ?? "")) body.clientId = clientId.trim();
    if (clientSecret.trim()) body.clientSecret = clientSecret.trim();
    else if (forgetClient) body.clientSecret = "";
    save.mutate(body, { onSuccess: () => onSaved(t.armature.saved) });
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.armature.title} data-armature-connection>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.armature.intro}</p>
        <Field
          label={t.armature.baseUrl}
          value={baseUrl}
          type="url"
          placeholder="https://armature.example.com"
          hint={t.armature.baseUrlHint}
          error={fields.baseUrl}
          onChange={(event) => setBaseUrl(event.target.value)}
        />
        <Field
          label={t.armature.webhookSecret}
          type="password"
          autoComplete="off"
          value={secret}
          placeholder={connection?.webhookSecretSet ? t.armature.secretStored : `${ARMATURE_WEBHOOK_SECRET_PREFIX}...`}
          error={fields.webhookSecret}
          onChange={(event) => setSecret(event.target.value)}
        />
        {connection?.webhookSecretSet && <Checkbox label={t.armature.forgetSecret} checked={forget} onChange={(event) => setForget(event.target.checked)} />}
        {connection && (
          <p className="text-sm text-ink-muted" data-armature-connected>
            {t.armature.connectedCount(connection.connected)} {connection.armatureOrgId && t.armature.orgKnown}
          </p>
        )}
        {connection && <MissingCapabilities missing={connection.missing} />}
        <fieldset className="space-y-3" data-armature-sign-in-app>
          <legend className="text-sm font-medium text-ink">{t.armature.signIn.title}</legend>
          <p className="text-sm text-ink-muted">{t.armature.signIn.intro}</p>
          {connection && (
            <p className="text-sm text-ink-muted" data-armature-exchange={connection.tokenExchange && connection.exchangeOffered ? "on" : "off"}>
              {!connection.tokenExchange ? t.armature.signIn.off : connection.exchangeOffered ? t.armature.signIn.on : t.armature.signIn.notOffered}
            </p>
          )}
          <Field
            label={t.armature.signIn.clientId}
            value={clientId}
            autoComplete="off"
            error={fields.clientId}
            onChange={(event) => setClientId(event.target.value)}
          />
          <Field
            label={t.armature.signIn.clientSecret}
            type="password"
            autoComplete="off"
            value={clientSecret}
            placeholder={connection?.clientSecretSet ? t.armature.secretStored : ""}
            error={fields.clientSecret}
            onChange={(event) => setClientSecret(event.target.value)}
          />
          {connection?.clientSecretSet && (
            <Checkbox label={t.armature.signIn.forgetSecret} checked={forgetClient} onChange={(event) => setForgetClient(event.target.checked)} />
          )}
        </fieldset>
        {connection && <p className="text-xs text-ink-muted">{t.armature.movesForget}</p>}
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <Button type="submit" loading={save.isPending} data-action="save-armature">
          {connection ? t.armature.save : t.armature.connect}
        </Button>
      </Card>
    </form>
  );
}

function Webhook({ connection }: { connection: ArmatureConnection }) {
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.armature.copied;
    try {
      await navigator.clipboard.writeText(connection.webhookUrl);
    } catch {
      message = t.armature.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <section aria-labelledby="armature-webhook" className="space-y-3">
      <SectionTitle id="armature-webhook">{t.armature.webhookTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.armature.webhookIntro}</p>
      <ol className="list-decimal space-y-1 pl-5 text-sm text-ink">
        {t.armature.webhookSteps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      <Field label={t.armature.webhookUrl} value={connection.webhookUrl} readOnly className="font-mono text-xs" onFocus={(event) => event.target.select()} />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-webhook-url">
          {t.armature.copy}
        </Button>
        <p role="status" className="text-xs text-ink-muted" data-copy-status>
          {said}
        </p>
      </div>
      <div>
        <h3 className="text-xs font-medium text-ink-muted">{t.armature.topics}</h3>
        <ul className="mt-1 flex flex-wrap gap-1.5" aria-label={t.armature.topics}>
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

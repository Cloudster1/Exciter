import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useCreatePartnerApp, useDeletePartnerApp, usePartnerApps, useRotatePartnerSecret, useUpdatePartnerApp, type PartnerApp } from "@/api/oauth";
import { Button, Card, Checkbox, ErrorBanner, Field, Skeleton, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COPY_FEEDBACK_MS, PARTNER_WRITE_SCOPE, TOKEN_READ_SCOPE } from "@/config";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** The tools beside Exciter that connect each person at their sign-in there, for administrators to register (#273). */
export function PartnerApps() {
  const { data: apps, error } = usePartnerApps();
  const update = useUpdatePartnerApp();
  const rotate = useRotatePartnerSecret();
  const remove = useDeletePartnerApp();
  const [fresh, setFresh] = useState<PartnerApp | null>(null);
  const toast = useToast();
  const confirm = useConfirm();
  const failed = error ?? update.error ?? rotate.error ?? remove.error;

  async function rotateSecret(app: PartnerApp) {
    const asked = { noun: t.kit.confirm.things.secretOf(app.name), verb: t.kit.confirm.verbs.rotate, body: t.oauth.partnerConfirmRotate(app.name) };
    if (!(await confirm(asked))) return;
    rotate.mutate(app.id, { onSuccess: setFresh });
  }

  async function removeApp(app: PartnerApp) {
    const noun = t.kit.confirm.things.named(t.kit.confirm.things.app, app.name);
    if (!(await confirm({ noun, verb: t.kit.confirm.verbs.remove, body: t.oauth.partnerConfirmRemove }))) return;
    remove.mutate(app.id, { onSuccess: () => toast.success(t.oauth.partnerRemoved(app.name)) });
  }

  // The box follows the change at once, as the list catches up only once it is saved.
  function writes(app: PartnerApp): boolean {
    const pending = update.isPending && update.variables?.id === app.id ? update.variables.scopes : undefined;
    return (pending ?? app.scopes).includes(PARTNER_WRITE_SCOPE);
  }

  function letWrite(app: PartnerApp, write: boolean) {
    update.mutate({ id: app.id, scopes: write ? [TOKEN_READ_SCOPE, PARTNER_WRITE_SCOPE] : [TOKEN_READ_SCOPE] });
  }

  return (
    <Card className="space-y-4 p-4" data-partner-apps>
      <div className="space-y-1">
        <h2 className="text-sm font-semibold text-ink">{t.oauth.partnersTitle}</h2>
        <p className="text-sm text-ink-muted">{t.oauth.partnersIntro}</p>
      </div>
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      {fresh?.secret && <FreshPartnerSecret app={fresh} onDone={() => setFresh(null)} />}
      {!apps && !error && <Skeleton />}
      {apps && apps.length === 0 && <p className="text-sm text-ink-muted">{t.oauth.partnersEmpty}</p>}
      {apps && apps.length > 0 && (
        <ul className="divide-y divide-border" aria-label={t.oauth.partnersTitle}>
          {apps.map((app) => (
            <li key={app.id} className="space-y-2 py-3" data-partner-row={app.name}>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-ink">{app.name}</span>
                {!app.scopes.includes(PARTNER_WRITE_SCOPE) && <Tag>{t.tokens.tagReadOnly}</Tag>}
                <span className="text-xs text-ink-muted">{t.oauth.partnerGrants(app.grants)}</span>
              </div>
              <p className="text-xs text-ink-muted">{app.keycloakClientId ? t.oauth.partnerKeycloak(app.keycloakClientId) : t.oauth.partnerNoKeycloak}</p>
              <p className="font-mono text-xs text-ink-muted">{t.oauth.partnerClientId(app.id)}</p>
              <div className="flex flex-wrap items-center gap-3">
                <Checkbox
                  label={t.oauth.partnerMayWrite}
                  checked={writes(app)}
                  disabled={update.isPending}
                  onChange={(event) => letWrite(app, event.target.checked)}
                  data-action="partner-write"
                />
                <Button
                  variant="secondary"
                  size="sm"
                  aria-label={t.oauth.partnerRotateLabel(app.name)}
                  loading={rotate.isPending && rotate.variables === app.id}
                  onClick={() => rotateSecret(app)}
                  data-action="rotate-partner-secret"
                >
                  {t.oauth.partnerRotate}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  icon={<Icon.Trash />}
                  aria-label={t.oauth.partnerRemoveLabel(app.name)}
                  loading={remove.isPending && remove.variables === app.id}
                  onClick={() => removeApp(app)}
                  data-action="remove-partner"
                >
                  {t.oauth.partnerRemove}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <NewPartnerApp onMade={setFresh} />
    </Card>
  );
}

function NewPartnerApp({ onMade }: { onMade: (app: PartnerApp) => void }) {
  const create = useCreatePartnerApp();
  const [name, setName] = useState("");
  const [clientUri, setClientUri] = useState("");
  const [keycloakClientId, setKeycloakClientId] = useState("");
  const [write, setWrite] = useState(false);
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const formError = create.error && Object.keys(fields).length === 0 ? create.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, clientUri, keycloakClientId, scopes: write ? [TOKEN_READ_SCOPE, PARTNER_WRITE_SCOPE] : [TOKEN_READ_SCOPE] },
      {
        onSuccess: (app) => {
          setName("");
          setClientUri("");
          setKeycloakClientId("");
          setWrite(false);
          onMade(app);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.oauth.partnerAdd} className="space-y-3 border-t border-border pt-4" data-new-partner>
      <Field label={t.oauth.partnerName} value={name} error={fields.name} onChange={(event) => setName(event.target.value)} />
      <Field label={t.oauth.partnerAddress} value={clientUri} error={fields.clientUri} onChange={(event) => setClientUri(event.target.value)} />
      <Field
        label={t.oauth.partnerKeycloakField}
        hint={t.oauth.partnerKeycloakHint}
        value={keycloakClientId}
        error={fields.keycloakClientId}
        onChange={(event) => setKeycloakClientId(event.target.value)}
      />
      <Checkbox label={t.oauth.partnerMayWrite} checked={write} onChange={(event) => setWrite(event.target.checked)} />
      {fields.scopes && <ErrorBanner>{fields.scopes}</ErrorBanner>}
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <Button type="submit" icon={<Icon.Plus />} loading={create.isPending} data-action="add-partner">
        {t.oauth.partnerAdd}
      </Button>
    </form>
  );
}

// The status line is always there, empty until needed, so a screen reader hears the copy succeed.
function FreshPartnerSecret({ app, onDone }: { app: PartnerApp; onDone: () => void }) {
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.webhooks.copied;
    try {
      await navigator.clipboard.writeText(app.secret ?? "");
    } catch {
      message = t.webhooks.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <Card className="space-y-2 border-accent/40 p-4" data-partner-secret>
      <h3 className="text-sm font-semibold text-ink">{t.oauth.partnerFreshTitle}</h3>
      <p className="text-sm text-ink-muted">{t.oauth.partnerFreshBody(app.name)}</p>
      <Field label={t.oauth.partnerClientIdField} value={app.id} readOnly className="font-mono text-xs" onFocus={(event) => event.target.select()} />
      <Field
        label={t.oauth.partnerSecretField(app.name)}
        value={app.secret ?? ""}
        readOnly
        className="font-mono text-xs"
        onFocus={(event) => event.target.select()}
        data-partner-secret-value
      />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-partner-secret">
          {t.webhooks.copy}
        </Button>
        <Button variant="ghost" size="sm" onClick={onDone} data-action="partner-secret-done">
          {t.webhooks.done}
        </Button>
        <p role="status" className="text-xs text-ink-muted">
          {said}
        </p>
      </div>
    </Card>
  );
}

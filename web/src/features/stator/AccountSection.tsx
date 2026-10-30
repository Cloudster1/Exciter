import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useCheckStator, useConnectStator, useDisconnectStator, useStatorAccount, type StatorAccount } from "@/api/stator";
import { Button, Card, ErrorBanner, Field, Page, PageHeader, Skeleton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { STATOR_TOKEN_PREFIX } from "@/config";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { localDateFormat } from "@/lib/format";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** The caller's own Stator account: paste a token, see whom it acts as, check it, disconnect; as Armature's. */
export function AccountSection() {
  const { data: account, isLoading, error } = useStatorAccount();
  const check = useCheckStator();
  const disconnect = useDisconnectStator();
  const confirm = useConfirm();
  const toast = useToast();

  async function forget() {
    const noun = t.kit.confirm.things.yourAccount(t.nav.stator);
    if (!(await confirm({ noun, verb: t.kit.confirm.verbs.disconnect, body: t.stator.account.confirmDisconnect }))) return;
    disconnect.mutate(undefined, { onSuccess: () => toast.success(t.stator.account.disconnected) });
  }

  function checkNow() {
    check.mutate(undefined, { onSuccess: (checked) => checked.status === "ok" && toast.success(t.stator.account.checked) });
  }

  const failure = error ?? check.error ?? disconnect.error;
  return (
    <Page width="narrow">
      <div className="space-y-3" data-stator-account>
        <PageHeader title={t.stator.account.title} meta={t.stator.account.intro} />
        {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
        {isLoading || !account ? (
          isLoading && <Skeleton />
        ) : !account.configured ? (
          <Card className="p-4 text-sm text-ink" data-stator-status="not_configured">
            {t.stator.account.notConfigured}
          </Card>
        ) : (
          <Card className="space-y-4 p-4" data-stator-status={account.status} data-stator-source={account.source}>
            {account.connected && <Connected account={account} />}
            {account.status !== "ok" && account.signIn && !account.signIn.connected && (
              <p className="flex items-start gap-1.5 text-sm text-ink" data-stator-sign-in>
                <Icon.Warning aria-hidden="true" className="mt-0.5 shrink-0" />
                {account.signIn.message}
              </p>
            )}
            {account.connected && (
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" onClick={checkNow} loading={check.isPending} data-action="check-stator">
                  {t.stator.account.check}
                </Button>
                <Button variant="ghost" size="sm" onClick={forget} loading={disconnect.isPending} data-action="disconnect-stator-account">
                  {t.stator.account.disconnect}
                </Button>
              </div>
            )}
            {account.status !== "ok" && <TokenForm account={account} />}
          </Card>
        )}
      </div>
    </Page>
  );
}

function Connected({ account }: { account: StatorAccount }) {
  return (
    <div className="space-y-1 text-sm">
      {account.user && (
        <p className="text-ink" data-stator-user>
          {t.stator.account.connectedAs(account.user.name, account.user.email)}
        </p>
      )}
      {account.source === "sign_in" && <p className="text-ink-muted">{t.stator.account.atSignIn}</p>}
      {account.status === "rejected" && <ErrorBanner>{t.stator.account.rejected}</ErrorBanner>}
      {account.status === "unreachable" && <p className="text-ink-muted">{t.stator.account.unreachable}</p>}
      {account.checkedAt && <p className="text-ink-muted">{t.stator.account.checkedAt(when.format(new Date(account.checkedAt)))}</p>}
    </div>
  );
}

// The token field is offered while there is no working token: a stored token is never shown, and a new one replaces
// it.
function TokenForm({ account }: { account: StatorAccount }) {
  const connect = useConnectStator();
  const [token, setToken] = useState("");
  const fields = connect.error instanceof ApiError ? connect.error.fields : {};
  const formError = connect.error && Object.keys(fields).length === 0 ? connect.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    connect.mutate(token.trim(), {
      onSuccess: () => setToken(""),
    });
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-3" data-stator-token-form>
      <p className="text-sm text-ink-muted">{t.stator.account.steps}</p>
      {account.baseUrl && (
        <a href={account.baseUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-accent hover:underline">
          {t.stator.account.openStator}
          <Icon.External aria-hidden="true" />
        </a>
      )}
      <Field
        label={account.connected ? t.stator.account.replace : t.stator.account.token}
        type="password"
        autoComplete="off"
        placeholder={`${STATOR_TOKEN_PREFIX}...`}
        value={token}
        error={fields.token}
        onChange={(event) => setToken(event.target.value)}
      />
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <Button type="submit" loading={connect.isPending} disabled={!token.trim()} data-action="connect-stator">
        {t.stator.account.connect}
      </Button>
    </form>
  );
}

import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useCheckArmature, useConnectArmature, useDisconnectArmature, type ArmatureAccount } from "@/api/armature";
import { Button, Card, ErrorBanner, Field, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_TOKEN_PREFIX } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { MissingCapabilities } from "./MissingCapabilities";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** The caller's own Armature account: paste a token, see whom it acts as, check it, disconnect; Stator's section. */
export function AccountSection() {
  const { data: account, isLoading, error } = useArmatureAccount();
  const check = useCheckArmature();
  const disconnect = useDisconnectArmature();
  const [notice, setNotice] = useState("");

  function forget() {
    if (!window.confirm(t.armature.account.confirmDisconnect)) return;
    setNotice("");
    disconnect.mutate(undefined, { onSuccess: () => setNotice(t.armature.account.disconnected) });
  }

  function checkNow() {
    setNotice("");
    check.mutate(undefined, { onSuccess: (checked) => checked.status === "ok" && setNotice(t.armature.account.checked) });
  }

  const failure = error ?? check.error ?? disconnect.error;
  return (
    <div className="mx-auto max-w-3xl space-y-3" data-armature-account>
      <PageHeader title={t.armature.account.title} meta={t.armature.account.intro} />
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-armature-notice>
        {notice}
      </p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {isLoading || !account ? (
        isLoading && <Skeleton />
      ) : !account.configured ? (
        <Card className="p-4 text-sm text-ink" data-armature-status="not_configured">
          {t.armature.account.notConfigured}
        </Card>
      ) : (
        <Card className="space-y-4 p-4" data-armature-status={account.status}>
          {account.connected && <Connected account={account} />}
          <MissingCapabilities missing={account.missing} />
          {account.connected && (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" onClick={checkNow} loading={check.isPending} data-action="check-armature">
                {t.armature.account.check}
              </Button>
              <Button variant="ghost" size="sm" onClick={forget} loading={disconnect.isPending} data-action="disconnect-armature-account">
                {t.armature.account.disconnect}
              </Button>
            </div>
          )}
          {(account.status !== "ok" || account.readOnly) && <TokenForm account={account} onConnected={() => setNotice("")} />}
        </Card>
      )}
    </div>
  );
}

function Connected({ account }: { account: ArmatureAccount }) {
  return (
    <div className="space-y-1 text-sm">
      {account.user && (
        <p className="text-ink" data-armature-user>
          {t.armature.account.connectedAs(account.user.name, account.user.email)}
        </p>
      )}
      {account.readOnly && (
        <p className="flex items-start gap-1.5 text-ink" data-armature-read-only>
          <Icon.Warning aria-hidden="true" className="mt-0.5 shrink-0" />
          {t.armature.account.readOnly}
        </p>
      )}
      {account.status === "rejected" && <ErrorBanner>{t.armature.account.rejected}</ErrorBanner>}
      {account.status === "unreachable" && <p className="text-ink-muted">{t.armature.account.unreachable}</p>}
      {account.checkedAt && <p className="text-ink-muted">{t.armature.account.checkedAt(when.format(new Date(account.checkedAt)))}</p>}
    </div>
  );
}

// The token field is offered while there is no working token, or one that may only read: a stored token is never
// shown, and a new one replaces it.
function TokenForm({ account, onConnected }: { account: ArmatureAccount; onConnected: () => void }) {
  const connect = useConnectArmature();
  const [token, setToken] = useState("");
  const fields = connect.error instanceof ApiError ? connect.error.fields : {};
  const formError = connect.error && Object.keys(fields).length === 0 ? connect.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    connect.mutate(token.trim(), {
      onSuccess: () => {
        setToken("");
        onConnected();
      },
    });
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-3" data-armature-token-form>
      <p className="text-sm text-ink-muted">{t.armature.account.steps}</p>
      {account.baseUrl && (
        <a href={account.baseUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-accent hover:underline">
          {t.armature.account.openArmature}
          <Icon.External aria-hidden="true" />
        </a>
      )}
      <Field
        label={account.connected ? t.armature.account.replace : t.armature.account.token}
        type="password"
        autoComplete="off"
        placeholder={`${ARMATURE_TOKEN_PREFIX}...`}
        value={token}
        error={fields.token}
        onChange={(event) => setToken(event.target.value)}
      />
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <Button type="submit" loading={connect.isPending} disabled={!token.trim()} data-action="connect-armature">
        {t.armature.account.connect}
      </Button>
    </form>
  );
}

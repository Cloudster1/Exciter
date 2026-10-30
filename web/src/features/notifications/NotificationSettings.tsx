import { useState, type FormEvent } from "react";
import {
  DIGESTS,
  NOTIFICATION_KINDS,
  switchedOn,
  usePreferences,
  useSavePreferences,
  type Digest,
  type NotificationKind,
  type Preferences,
} from "@/api/notifications";
import { Button, Card, Checkbox, ErrorBanner, PageHeader, Select, Skeleton, Table, Td, Th, Page } from "@/components/ui";
import { DAILY_DIGEST_HOUR_UTC } from "@/config";
import { t } from "@/i18n";

// Notification preferences (#34), Stator's NotificationSettings with the kinds of requirements.

function digestLabel(digest: Digest): string {
  return digest === "daily" ? t.notifications.digests.daily(DAILY_DIGEST_HOUR_UTC) : t.notifications.digests[digest];
}

/** How the caller wants to be told: each kind in the app and by email, when emails go, and watching what they write. */
export function NotificationSettings() {
  const { data, error, isLoading, refetch } = usePreferences();
  return (
    <Page width="narrow" data-notification-settings="">
      <PageHeader title={t.notifications.preferencesTitle} meta={t.notifications.preferencesIntro} />
      {error ? <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner> : isLoading || !data ? <Skeleton /> : <PreferencesForm saved={data} />}
    </Page>
  );
}

function PreferencesForm({ saved }: { saved: Preferences }) {
  const save = useSavePreferences();
  const [draft, setDraft] = useState<Preferences>(saved);
  const [notice, setNotice] = useState("");

  function toggle(channel: "inApp" | "email", kind: NotificationKind, on: boolean) {
    setNotice("");
    setDraft((current) => ({ ...current, [channel]: { ...current[channel], [kind]: on } }));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate(draft, {
      onSuccess: (next) => {
        setDraft(next);
        setNotice(t.notifications.saved);
      },
    });
  }

  return (
    <form onSubmit={submit} className="space-y-4" noValidate>
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <Table>
        <thead>
          <tr>
            <Th>{t.notifications.kindColumn}</Th>
            <Th className="w-28 text-center">{t.notifications.inApp}</Th>
            <Th className="w-28 text-center">{t.notifications.email}</Th>
          </tr>
        </thead>
        <tbody>
          {NOTIFICATION_KINDS.map((kind) => {
            const words = t.notifications.kinds[kind];
            const inApp = switchedOn(draft.inApp, kind);
            return (
              <tr key={kind} data-preference={kind}>
                <Td>{words}</Td>
                <Td className="text-center">
                  <Checkbox
                    label={<span className="sr-only">{t.notifications.inAppFor(words)}</span>}
                    checked={inApp}
                    onChange={(event) => toggle("inApp", kind, event.target.checked)}
                    data-channel="inApp"
                  />
                </Td>
                <Td className="text-center">
                  <Checkbox
                    label={<span className="sr-only">{t.notifications.emailFor(words)}</span>}
                    // Mail is a copy of the row in the app, so without one there is nothing to mail.
                    checked={inApp && switchedOn(draft.email, kind)}
                    disabled={!inApp}
                    onChange={(event) => toggle("email", kind, event.target.checked)}
                    data-channel="email"
                  />
                </Td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      <Card className="space-y-4 p-4">
        <Select
          label={t.notifications.digest}
          hint={t.notifications.digestHint}
          value={draft.digest}
          onChange={(event) => {
            setNotice("");
            setDraft({ ...draft, digest: event.target.value as Digest });
          }}
          data-field="digest"
        >
          {DIGESTS.map((digest) => (
            <option key={digest} value={digest}>
              {digestLabel(digest)}
            </option>
          ))}
        </Select>
        <div>
          <Checkbox
            label={t.notifications.autoWatch}
            checked={draft.autoWatch}
            onChange={(event) => {
              setNotice("");
              setDraft({ ...draft, autoWatch: event.target.checked });
            }}
            aria-describedby="auto-watch-hint"
            data-field="autoWatch"
          />
          <p id="auto-watch-hint" className="mt-1 text-sm text-ink-subtle">
            {t.notifications.autoWatchHint}
          </p>
        </div>
      </Card>
      <div className="flex items-center justify-end gap-3">
        <p role="status" className="text-sm text-ink-muted empty:hidden" data-preferences-saved="">
          {notice}
        </p>
        <Button type="submit" loading={save.isPending} data-action="save-preferences">
          {t.notifications.save}
        </Button>
      </div>
    </form>
  );
}

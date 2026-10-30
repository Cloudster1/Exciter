import { useInstallation, type Installation } from "@/api/installation";
import { useSwitchOAuthConnections } from "@/api/oauth";
import { Card, ErrorBanner, Page, PageHeader, Skeleton, Switch } from "@/components/ui";
import { AllTokens } from "@/features/tokens/AllTokens";
import { t } from "@/i18n";
import { AllConnectedApps } from "./AllConnectedApps";
import { PartnerApps } from "./PartnerApps";

/** Admin's Connected apps: whether assistants may connect, the partner apps, and every grant and token people hold. */
export function InstallationApps() {
  const { data, error } = useInstallation();
  return (
    <Page width="content" className="space-y-6" data-installation-apps="">
      <PageHeader title={t.admin.connectedApps.title} meta={t.admin.connectedApps.intro} />
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {data ? <OAuthConnections installation={data} /> : !error && <Skeleton />}
      <PartnerApps />
      <AllConnectedApps />
      <AllTokens />
    </Page>
  );
}

/** Whether assistants may connect with OAuth (#188); off, every app connected that way is refused until it is on. */
function OAuthConnections({ installation }: { installation: Installation }) {
  const switching = useSwitchOAuthConnections();
  const on = switching.data?.oauthConnections ?? installation.oauthConnections;
  return (
    <Card className="space-y-3 p-4" data-oauth-connections={on ? "on" : "off"}>
      <div className="flex items-center gap-3">
        <Switch
          label={t.oauth.switchLabel}
          checked={on}
          disabled={switching.isPending}
          onChange={(next) => switching.mutate(next)}
          data-action="switch-oauth"
        />
        <span className="text-sm font-medium text-ink">{t.oauth.switchLabel}</span>
      </div>
      <p className="text-sm text-ink-muted">{t.oauth.switchIntro}</p>
      {switching.error && <ErrorBanner>{switching.error.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {switching.isSuccess ? (on ? t.oauth.switchedOn : t.oauth.switchedOff) : ""}
      </p>
    </Card>
  );
}

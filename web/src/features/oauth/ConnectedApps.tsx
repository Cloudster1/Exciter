import { useState } from "react";
import { useEndGrant, useGrants, type Grant } from "@/api/oauth";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PARTNER_CLIENT_KIND, TOKEN_EVIDENCE_SCOPE, TOKEN_READ_SCOPE } from "@/config";
import { reachOf, when } from "@/features/tokens/TokensPage";
import { t } from "@/i18n";

/** The app a grant connects, with what it reaches; a row of either list. */
export function AppCell({ grant }: { grant: Grant }) {
  return (
    <>
      <span className="font-medium text-ink">{grant.client.name}</span>
      {grant.scopes.includes(TOKEN_READ_SCOPE) && <Tag className="ml-2">{t.tokens.tagReadOnly}</Tag>}
      {grant.scopes.includes(TOKEN_EVIDENCE_SCOPE) && <Tag className="ml-2">{t.tokens.tagEvidence}</Tag>}
      <p className="text-xs text-ink-muted">{grant.client.kind === PARTNER_CLIENT_KIND ? t.oauth.viaSignIn : t.oauth.sendsBackTo(grant.redirectHost)}</p>
      <p className="text-xs text-ink-muted" data-app-projects={grant.allProjects ? "" : grant.projects.join(",")}>
        {reachOf(grant)}
      </p>
    </>
  );
}

export function lastUsed(grant: Grant): string {
  return grant.lastUsedAt ? when.format(new Date(grant.lastUsedAt)) : t.tokens.neverUsed;
}

/** A person's connected apps: what each may do and when it was last used, each ended here (#189). */
export function ConnectedApps() {
  const { data: grants, error } = useGrants();
  const end = useEndGrant();
  const [notice, setNotice] = useState("");

  function endGrant(grant: Grant) {
    if (!window.confirm(t.oauth.confirmEnd(grant.client.name))) return;
    end.mutate(grant.id, { onSuccess: () => setNotice(t.oauth.ended(grant.client.name)) });
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <PageHeader title={t.oauth.appsTitle} meta={t.oauth.appsIntro} />
      {(error ?? end.error) && <ErrorBanner>{(error ?? end.error)?.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-apps-notice>
        {notice}
      </p>
      {!grants && !error && <Skeleton />}
      {grants && grants.length === 0 && <EmptyState icon={<Icon.Link />} title={t.oauth.appsEmpty} description={t.oauth.appsEmptyBody} />}
      {grants && grants.length > 0 && (
        <Table aria-label={t.oauth.appsTitle}>
          <thead>
            <tr>
              <Th>{t.oauth.columnApp}</Th>
              <Th>{t.oauth.columnConnected}</Th>
              <Th>{t.tokens.columnLastUsed}</Th>
              <Th className="w-10">
                <span className="sr-only">{t.tokens.columnActions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {grants.map((grant) => (
              <tr key={grant.id} data-app-row={grant.client.name}>
                <Td>
                  <AppCell grant={grant} />
                </Td>
                <Td className="text-ink-muted">{when.format(new Date(grant.createdAt))}</Td>
                <Td className="text-ink-muted" data-app-last-used>
                  {lastUsed(grant)}
                </Td>
                <Td className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t.oauth.endLabel(grant.client.name)}
                    loading={end.isPending && end.variables === grant.id}
                    onClick={() => endGrant(grant)}
                    data-action="end-app"
                  >
                    {t.oauth.end}
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

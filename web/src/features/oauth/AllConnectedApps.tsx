import { useState } from "react";
import { useEndGrant, useEveryonesGrants, type OrgGrant } from "@/api/oauth";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { AppCell, lastUsed } from "./ConnectedApps";

/** Everybody's connected apps with whose each is, for the administrators to end any that should stop (#189). */
export function AllConnectedApps() {
  const { data: grants, error } = useEveryonesGrants();
  const end = useEndGrant(true);
  const [notice, setNotice] = useState("");

  function endGrant(grant: OrgGrant) {
    if (!window.confirm(t.oauth.confirmEnd(grant.client.name))) return;
    end.mutate(grant.id, { onSuccess: () => setNotice(t.oauth.ended(grant.client.name)) });
  }

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <PageHeader title={t.oauth.allTitle} meta={t.oauth.allIntro} />
      {(error ?? end.error) && <ErrorBanner>{(error ?? end.error)?.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-apps-notice>
        {notice}
      </p>
      {!grants && !error && <Skeleton />}
      {grants && grants.length === 0 && <EmptyState icon={<Icon.Link />} title={t.oauth.allEmpty} />}
      {grants && grants.length > 0 && (
        <Table aria-label={t.oauth.allTitle}>
          <thead>
            <tr>
              <Th>{t.oauth.columnApp}</Th>
              <Th>{t.tokens.columnOwner}</Th>
              <Th>{t.tokens.columnLastUsed}</Th>
              <Th className="w-10">
                <span className="sr-only">{t.tokens.columnActions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {grants.map((grant) => (
              <tr key={grant.id} data-app-row={grant.client.name} data-app-owner={grant.owner.email}>
                <Td>
                  <AppCell grant={grant} />
                </Td>
                <Td className="text-ink">{grant.owner.name || grant.owner.email}</Td>
                <Td className="text-ink-muted">{lastUsed(grant)}</Td>
                <Td className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t.oauth.endOwnedLabel(grant.client.name, grant.owner.name || grant.owner.email)}
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

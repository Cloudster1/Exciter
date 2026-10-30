import { useEndGrant, useEveryonesGrants, type OrgGrant } from "@/api/oauth";
import { Button, EmptyState, ErrorBanner, Skeleton, Table, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { AppCell, lastUsed } from "./ConnectedApps";

/** Everybody's connected apps with whose each is, for the administrators to end any that should stop (#189). */
export function AllConnectedApps() {
  const { data: grants, error } = useEveryonesGrants();
  const end = useEndGrant(true);
  const toast = useToast();
  const confirm = useConfirm();

  async function endGrant(grant: OrgGrant) {
    const noun = t.kit.confirm.things.named(t.kit.confirm.things.app, grant.client.name);
    if (!(await confirm({ noun, verb: t.kit.confirm.verbs.disconnect, body: t.oauth.confirmEnd }))) return;
    end.mutate(grant.id, { onSuccess: () => toast.success(t.oauth.ended(grant.client.name)) });
  }

  return (
    <section aria-labelledby="all-connected-apps" className="space-y-3" data-section="all-connected-apps">
      <div>
        <h2 id="all-connected-apps" className="text-base font-semibold text-ink">
          {t.oauth.allTitle}
        </h2>
        <p className="text-sm text-ink-muted">{t.oauth.allIntro}</p>
      </div>
      {(error ?? end.error) && <ErrorBanner>{(error ?? end.error)?.message}</ErrorBanner>}
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
    </section>
  );
}

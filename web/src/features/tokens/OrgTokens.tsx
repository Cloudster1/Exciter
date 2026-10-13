import { useState } from "react";
import { useOrgTokens, useRevokeToken, type OrgApiToken } from "@/api/tokens";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TOKEN_READ_SCOPE } from "@/config";
import { t } from "@/i18n";
import { expired, reachOf, when } from "./TokensPage";

/** Every token of the organization with whose it is, for its administrators to revoke any that should stop. */
export function OrgTokens() {
  const { data: tokens, error } = useOrgTokens();
  const revoke = useRevokeToken(true);
  const [notice, setNotice] = useState("");
  const now = new Date();

  function revokeToken(token: OrgApiToken) {
    if (!window.confirm(t.tokens.confirmRevoke(token.name))) return;
    revoke.mutate(token.id, { onSuccess: () => setNotice(t.tokens.revoked(token.name)) });
  }

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <PageHeader title={t.tokens.orgTitle} meta={t.tokens.orgIntro} />
      {(error ?? revoke.error) && <ErrorBanner>{(error ?? revoke.error)?.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-tokens-notice>
        {notice}
      </p>
      {!tokens && !error && <Skeleton />}
      {tokens && tokens.length === 0 && <EmptyState icon={<Icon.Key />} title={t.tokens.orgEmpty} />}
      {tokens && tokens.length > 0 && (
        <Table aria-label={t.tokens.orgTitle}>
          <thead>
            <tr>
              <Th>{t.tokens.columnName}</Th>
              <Th>{t.tokens.columnOwner}</Th>
              <Th>{t.tokens.columnLastUsed}</Th>
              <Th>{t.tokens.columnExpires}</Th>
              <Th className="w-10">
                <span className="sr-only">{t.tokens.columnActions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((token) => (
              <tr key={token.id} data-token-row={token.name} data-token-owner={token.owner.email}>
                <Td>
                  <span className="font-medium text-ink">{token.name}</span>
                  {token.scopes.includes(TOKEN_READ_SCOPE) && <Tag className="ml-2">{t.tokens.tagReadOnly}</Tag>}
                  {expired(token, now) && <Tag className="ml-2">{t.tokens.tagExpired}</Tag>}
                  <p className="text-xs text-ink-muted">{reachOf(token)}</p>
                </Td>
                <Td className="text-ink">{token.owner.name || token.owner.email}</Td>
                <Td className="text-ink-muted">{token.lastUsedAt ? when.format(new Date(token.lastUsedAt)) : t.tokens.neverUsed}</Td>
                <Td className="text-ink-muted">{token.expiresAt ? when.format(new Date(token.expiresAt)) : t.tokens.neverExpires}</Td>
                <Td className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t.tokens.revokeOwnedLabel(token.name, token.owner.name || token.owner.email)}
                    loading={revoke.isPending && revoke.variables === token.id}
                    onClick={() => revokeToken(token)}
                    data-action="revoke-token"
                  >
                    {t.tokens.revoke}
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

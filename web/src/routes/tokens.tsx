import { createRoute } from "@tanstack/react-router";
import { ORG_TOKENS_PATH, TOKENS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { OrgTokens } from "@/features/tokens/OrgTokens";
import { TokensPage } from "@/features/tokens/TokensPage";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** A person's own personal access tokens, under their account. */
export const tokensRoute = createRoute({ getParentRoute: () => appRoute, path: TOKENS_PATH, component: TokensPage });

/** Every token of the organization, for its administrators. */
export const orgTokensRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ORG_TOKENS_PATH,
  component: () => (
    <AdminsOnly title={t.tokens.orgTitle} refusal={t.tokens.notAdmin}>
      <OrgTokens />
    </AdminsOnly>
  ),
});

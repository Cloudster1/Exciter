import { createRoute } from "@tanstack/react-router";
import { ALL_TOKENS_PATH, TOKENS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { AllTokens } from "@/features/tokens/AllTokens";
import { TokensPage } from "@/features/tokens/TokensPage";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** A person's own personal access tokens, under their account. */
export const tokensRoute = createRoute({ getParentRoute: () => appRoute, path: TOKENS_PATH, component: TokensPage });

/** Every token of the installation, for its administrators. */
export const allTokensRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ALL_TOKENS_PATH,
  component: () => (
    <AdminsOnly title={t.tokens.allTitle} refusal={t.tokens.notAdmin}>
      <AllTokens />
    </AdminsOnly>
  ),
});

import { createRoute } from "@tanstack/react-router";
import { TOKENS_PATH } from "@/config";
import { TokensPage } from "@/features/tokens/TokensPage";
import { appRoute } from "./app";

/** A person's own personal access tokens, under their account. */
export const tokensRoute = createRoute({ getParentRoute: () => appRoute, path: TOKENS_PATH, component: TokensPage });

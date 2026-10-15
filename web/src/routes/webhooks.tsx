import { createRoute } from "@tanstack/react-router";
import { WEBHOOKS_PATH } from "@/config";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { Webhooks } from "@/features/webhooks/Webhooks";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where the installation's requirement events are posted, signed, with a log of every attempt, for its administrators (#48). */
export const webhooksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: WEBHOOKS_PATH,
  component: () => (
    <AdminsOnly title={t.webhooks.title} refusal={t.webhooks.notAdmin}>
      <Webhooks />
    </AdminsOnly>
  ),
});

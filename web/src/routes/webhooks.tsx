import { createRoute } from "@tanstack/react-router";
import { WEBHOOKS_PATH } from "@/config";
import { Webhooks } from "@/features/webhooks/Webhooks";
import { adminRoute } from "./admin";

/** Where the installation's requirement events are posted, signed, with a log of every attempt, for its administrators (#48). */
export const webhooksRoute = createRoute({ getParentRoute: () => adminRoute, path: WEBHOOKS_PATH, component: Webhooks });

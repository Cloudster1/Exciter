import { createRoute } from "@tanstack/react-router";
import { NOTIFICATION_SETTINGS_PATH, NOTIFICATIONS_PATH, WATCHING_PATH } from "@/config";
import { NotificationList } from "@/features/notifications/NotificationList";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { WatchingList } from "@/features/watching/WatchingList";
import { appRoute } from "./app";

/** Everything the caller was told; the bell leads here. */
export const notificationsRoute = createRoute({ getParentRoute: () => appRoute, path: NOTIFICATIONS_PATH, component: NotificationList });

/** Where the caller chooses what they hear about; every notification mail links here. */
export const notificationSettingsRoute = createRoute({ getParentRoute: () => appRoute, path: NOTIFICATION_SETTINGS_PATH, component: NotificationSettings });

/** The requirements and projects the caller watches. */
export const watchingRoute = createRoute({ getParentRoute: () => appRoute, path: WATCHING_PATH, component: WatchingList });

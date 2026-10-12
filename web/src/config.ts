/** Tunable values shared across the client. */

/** The product's name, as the title bar and the sidebar show it. */
export const APP_NAME = "Exciter";

/** The logo's width on the sign-in page, and the mark's beside the name in the sidebar, in pixels. */
export const LOGO_WIDTH_PX = 112;
export const MARK_WIDTH_PX = 24;

/** The release this client was built from, read from the root VERSION file at build time. */
export const APP_VERSION: string = __APP_VERSION__;

/** Where the API lives, relative to the page; the dev server and nginx proxy it. */
export const API_BASE = "/api/v1";
/** The status the API answers when nobody is signed in, which sends the reader to sign in. */
export const HTTP_UNAUTHORIZED = 401;

/** The sidebar, open and as the icon rail it folds to. */
export const SIDEBAR_WIDTH = 232;
export const SIDEBAR_RAIL_WIDTH = 48;

/** Below this viewport width the sidebar starts as a rail. */
export const SIDEBAR_RAIL_BELOW_PX = 1024;

/** Below this viewport width the rail goes and the sidebar is a drawer; index.css names the same width `shell`. */
export const SIDEBAR_DRAWER_BELOW_PX = 768;

/** A skeleton waits this long before showing, so a fast load never flashes. */
export const SKELETON_DELAY_MS = 150;

/** A tooltip waits this long, so a pointer passing over shows nothing. */
export const TOOLTIP_DELAY_MS = 400;

/** The icon grid and stroke: every glyph is drawn to these two numbers. */
export const ICON_SIZE_PX = 16;
export const ICON_STROKE = 1.5;

/** The gap between a menu's trigger and its list, in pixels. */
export const MENU_GAP_PX = 4;

/** How long a query's answer stays fresh before a refetch. */
export const QUERY_STALE_MS = 10_000;

/** Where a visitor without a session is sent, with where they were headed in `next`. */
export const LOGIN_PATH = "/login";

/** Who is signed in changes only by signing in or out, so the answer is kept a minute. */
export const ME_STALE_MS = 60_000;

/** Where this browser remembers the interface language the person chose, so a reload starts in it. */
export const LANGUAGE_STORAGE_KEY = "exciter.language";

/** Where an administrator manages the organization's people. */
export const MEMBERS_PATH = "/settings/members";

/** Where an administrator reads the organization's audit log. */
export const AUDIT_PATH = "/settings/audit";
/** Entries the audit log shows per page, within the API's 200; the export has the rest. */
export const AUDIT_PAGE_SIZE = 50;
/** Where the audit log is downloaded as a spreadsheet, under the API's base. */
export const AUDIT_EXPORT_PATH = "/audit/export";

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
/** The status the API answers somebody who may not see a page, such as a member on an administrator's settings. */
export const HTTP_FORBIDDEN = 403;

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

/** Where an administrator renames the installation. */
export const INSTALLATION_PATH = "/settings/installation";

/** Where an administrator manages the installation's people. */
export const MEMBERS_PATH = "/settings/members";

/** Where an administrator sets up single sign-on, maps its groups to roles and lets waiting people in. */
export const SSO_PATH = "/settings/sso";
/** The groups claim and scopes an identity provider starts with, matching the API's defaults. */
export const SSO_DEFAULT_GROUPS_CLAIM = "groups";
export const SSO_DEFAULT_SCOPES = "openid profile email";
/** The group a mapping's form shows as an example, the development realm's administrators. */
export const SSO_GROUP_EXAMPLE = "exciter-administrators";
/** What a sign-in through the provider came back with when the person is known there but not let in here. */
export const SSO_WAITING = "not_a_member";

/** Where an administrator reads the installation's audit log. */
export const AUDIT_PATH = "/settings/audit";
/** Entries the audit log shows per page, within the API's 200; the export has the rest. */
export const AUDIT_PAGE_SIZE = 50;
/** Where the audit log is downloaded as a spreadsheet, under the API's base. */
export const AUDIT_EXPORT_PATH = "/audit/export";

/** Where a person picks, makes and imports themes, and where an administrator names the installation's default. */
export const THEMES_PATH = "/settings/themes";
export const DEFAULT_THEME_PATH = "/settings/default-theme";
/** The largest file a theme takes, matching the API's limit. */
export const THEME_ASSET_MAX_BYTES = 12 * 1024 * 1024;
/** The most extra CSS a theme carries, matching the API's limit. */
export const THEME_CSS_MAX_BYTES = 32 * 1024;
/** How long a draft settles before the live preview is recompiled. */
export const THEME_PREVIEW_DEBOUNCE_MS = 150;
/** The size an icon is drawn at in the theme editor's list. */
export const THEME_ICON_PREVIEW_PX = 20;
/** Lines the extra CSS box shows before it scrolls. */
export const THEME_CSS_ROWS = 12;
/** The largest radius a theme may set, matching the API's limit. */
export const THEME_MAX_RADIUS = 32;
/** The furthest a cursor's point may sit from its picture's corner, matching the API's limit. */
export const THEME_MAX_HOTSPOT = 128;
/** The size a cursor picture is best drawn at; larger ones are refused by some browsers. */
export const THEME_CURSOR_PX = 32;
/** What a colour field shows, and its picker starts at, when the stylesheet's own value cannot be read. */
export const THEME_COLOUR_PLACEHOLDER = "#000000";
/** The example a shadow field shows, written the way box-shadow takes it. */
export const THEME_SHADOW_PLACEHOLDER = "0 1px 2px rgb(0 0 0 / 0.1)";
/** Bytes in a kilobyte, for the sizes the editor prints. */
export const KILOBYTE = 1024;

/** How many pieces a click throws, how long each lives, and how hard they fall. */
export const CONFETTI_PIECES_PER_CLICK = 28;
export const CONFETTI_LIFE_MS = 1100;
export const CONFETTI_GRAVITY_PX_PER_S2 = 1600;
/** The colours of the paper scraps, and the characters thrown with them, as Stator throws them. */
export const CONFETTI_COLOURS = ["#ff1744", "#ffab00", "#00c853", "#2979ff", "#d500f9", "#00e5ff", "#ff2e88", "#ffd600"];
export const CONFETTI_GLYPHS = ["\u{1F389}", "\u{2728}", "\u{1F9E7}", "\u{1F308}", "\u{1F4A5}", "\u{1F340}", "\u{1F38A}", "\u{2B50}"];

/** How many wandering points the constellation backdrop draws per million pixels of page. */
export const CONSTELLATION_POINTS_PER_MEGAPIXEL = 70;
/** How close two of them have to be for a line to join them, in pixels. */
export const CONSTELLATION_LINK_PX = 150;
/** How fast a point wanders, in pixels per second, before its own variation. */
export const CONSTELLATION_SPEED_PX_PER_S = 14;
/** The grey a point or a line is drawn in when the theme's colour cannot be read. */
export const CONSTELLATION_FALLBACK_COLOUR = "#808080";

/** Where the projects live, and the limits their forms share with the server's project package. */
export const PROJECTS_PATH = "/projects";
export const NEW_PROJECT_PATH = "/projects/new";
export const PROJECT_KEY_MAX_LENGTH = 10;
export const PROJECT_NAME_MAX_LENGTH = 100;
export const PROJECT_DESCRIPTION_MAX_LENGTH = 1000;
/** How long typing a project's name rests before the server is asked for a free key. */
export const KEY_SUGGEST_DEBOUNCE_MS = 300;

/** How many people or groups a picker offers at once, and how long typing rests before it asks; the server's perm.DefaultPickerLimit. */
export const PICKER_LIMIT = 20;
/** The most a picker gives at once, perm.MaxPickerLimit, which naming the people a value holds asks for. */
export const MAX_PICKER_LIMIT = 50;
export const PICKER_DEBOUNCE_MS = 200;

/** The heading levels the editor offers; the server's document.MaxHeadingLevel is the deepest. */
export const EDITOR_HEADING_LEVELS = [1, 2, 3] as const;
/** How many people an at sign offers at once. */
export const MENTION_LIMIT = 8;
/** The size of a table the toolbar inserts. */
export const NEW_TABLE_ROWS = 3;
export const NEW_TABLE_COLUMNS = 3;
/** How many requirements a page of the list shows; the server's search.MaxGridLimit is the most it gives. */
export const REQUIREMENT_PAGE_SIZE = 50;
/** The longest title and comment the server takes, its requirement.MaxTitleLength and MaxCommentLength. */
export const REQUIREMENT_TITLE_MAX_LENGTH = 300;
export const REVISION_COMMENT_MAX_LENGTH = 500;

/** The icons a requirement type may take, by the name the server stores; TypeIcon draws each. */
export const TYPE_ICONS = ["requirement", "interface", "constraint", "test", "need", "function", "risk", "data"] as const;
/** The colours a type is offered in, which a theme's own palette does not change since they tell types apart. */
export const TYPE_COLOURS = ["#4f6bed", "#0f9d8a", "#d97706", "#c2410c", "#7c3aed", "#db2777", "#2563eb", "#64748b"] as const;

/** How many nodes of the specification one read brings; the server's requirement.MaxSpecLimit is the most. */
export const SPEC_PAGE_SIZE = 50;
/** How far a level of the tree is indented, and how much of a row's height its edges take as a drop beside it. */
export const TREE_INDENT_PX = 14;
export const TREE_DROP_EDGE = 0.25;

/** How many revisions, or entries of the activity, one window of a requirement's history shows. */
export const HISTORY_PAGE_SIZE = 20;

/** The size of a status on the workflow canvas, and the grid it snaps to when dropped, so rows line up. */
export const WORKFLOW_NODE_WIDTH = 240;
export const WORKFLOW_NODE_HEIGHT = 72;
export const WORKFLOW_GRID = 8;
/** Where the automatic layout puts the first column and row, and how far apart its columns (one per category) and rows go. */
export const WORKFLOW_LAYOUT_ORIGIN_X = 32;
export const WORKFLOW_LAYOUT_ORIGIN_Y = 40;
export const WORKFLOW_COLUMN_GAP = 296;
export const WORKFLOW_ROW_GAP = 104;
/** Where the "any status" source of global transitions is drawn: below the first rows, so its arrows climb to their targets. */
export const WORKFLOW_ANYWHERE_X = WORKFLOW_LAYOUT_ORIGIN_X;
export const WORKFLOW_ANYWHERE_Y = WORKFLOW_LAYOUT_ORIGIN_Y + 3 * WORKFLOW_ROW_GAP;
/** How far a transition's curve bows out, so two moves between the same pair stay apart. */
export const WORKFLOW_EDGE_BEND = 64;
/** How wide a band around a transition's curve takes a click. */
export const WORKFLOW_EDGE_HIT_WIDTH = 14;
/** The canvas is at least this big and grows with what is drawn, leaving the margin beyond the furthest status to drag to. */
export const WORKFLOW_CANVAS_MIN_WIDTH = 900;
export const WORKFLOW_CANVAS_MIN_HEIGHT = 520;
export const WORKFLOW_CANVAS_MARGIN = 160;
/** A pointer that travels less than this is a click; anything bigger takes that much off every drag. */
export const WORKFLOW_DRAG_THRESHOLD_PX = 1;
/** A transition dropped within this distance of a status's centre lands on it. */
export const WORKFLOW_CONNECT_RADIUS = 96;
/** The ring a transition is dragged out of, and the round badge that carries a status's category. */
export const WORKFLOW_HANDLE_SIZE = 16;
export const WORKFLOW_BADGE_SIZE = 36;
/** Names longer than this are cut on a status card, so a card stays one size. */
export const WORKFLOW_NAME_LENGTH = 26;
/** The longest name and description of a status, the server's workflow.MaxNameLength and MaxDescriptionLength. */
export const STATUS_NAME_MAX_LENGTH = 60;
export const STATUS_DESCRIPTION_MAX_LENGTH = 500;
/** The address segment that opens the designer on a workflow not made yet. */
export const NEW_WORKFLOW = "new";
/** How many lines the comment a transition asks for starts with. */
export const TRANSITION_COMMENT_ROWS = 3;

/** The longest label, matching the API's label.MaxNameLength. */
export const LABEL_NAME_MAX_LENGTH = 40;
/** How many labels the label box offers at once, within the API's label.MaxSuggestions of 50. */
export const LABEL_SUGGESTION_LIMIT = 10;
/** How long the label box waits after the last key before it asks the server. */
export const LABEL_SUGGEST_DEBOUNCE_MS = 200;
/** How many requirements one page of a label's list shows, within the API's label.MaxLimit of 100. */
export const LABEL_PAGE_SIZE = 20;

/** The longest search the server takes, its search.MaxQueryLength. */
export const SEARCH_QUERY_MAX_LENGTH = 200;
/** How many hits a page of the full search shows, within the server's search.MaxLimit of 100. */
export const SEARCH_PAGE_SIZE = 20;
/** How many requirements the Ctrl K palette offers, within the server's search.MaxQuickLimit of 20. */
export const QUICK_SEARCH_LIMIT = 8;
/** How long the palette waits after the last key before it asks the server. */
export const QUICK_SEARCH_DEBOUNCE_MS = 150;
/** Where the full search lives. */
export const SEARCH_PATH = "/search";
/** How many rows a page of the grid shows, within the server's search.MaxGridLimit of 500. */
export const GRID_PAGE_SIZE = 100;
/** The most attribute conditions and columns a grid takes, the server's search.MaxAttributeFilters and MaxColumns. */
export const GRID_MAX_CONDITIONS = 10;
export const GRID_MAX_COLUMNS = 30;
/** The longest view name the server takes, its search.MaxViewNameLength. */
export const VIEW_NAME_MAX_LENGTH = 100;

/** The longest link type key, name and phrase, the server's trace.MaxKeyLength, MaxNameLength and MaxPhraseLength. */
export const LINK_TYPE_KEY_MAX_LENGTH = 40;
export const LINK_TYPE_NAME_MAX_LENGTH = 40;
export const LINK_TYPE_PHRASE_MAX_LENGTH = 60;
/** The most rules a link type takes in a project, the server's trace.MaxRules. */
export const LINK_TYPE_MAX_RULES = 50;
/** Where the installation's link types are kept. */
export const LINK_TYPES_PATH = "/settings/link-types";

/** The longest description and widest picture a statement keeps, the server's document.MaxAltLength and MaxImageWidth. */
export const IMAGE_ALT_MAX_LENGTH = 500;
export const IMAGE_MAX_WIDTH_PX = 4000;
/** The types the server shows in place when asked, attachment.ShowsInPlace; everything else downloads. */
export const INLINE_FILE_TYPES: readonly string[] = ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain"];
/** Below this many of a unit a file size shows one decimal, as 3.4 MB; from it on, whole numbers. */
export const FILE_SIZE_DECIMALS_BELOW = 10;

/** Where a person keeps their personal access tokens, and where administrators see everybody's (#22, #86). */
export const TOKENS_PATH = "/settings/tokens";
export const ALL_TOKENS_PATH = "/settings/all-tokens";
/** The one scope a token may carry, and the longest name it takes, the API's auth.ScopeRead and MaxTokenNameLength. */
export const TOKEN_READ_SCOPE = "read";
export const TOKEN_NAME_MAX_LENGTH = 100;
/** The lifetimes offered for a new token, in days, and the one picked unless a person changes it. */
export const TOKEN_EXPIRY_DAYS = [7, 30, 90, 365] as const;
export const TOKEN_DEFAULT_EXPIRY_DAYS = 30;
/** The most projects one token names, the API's auth.MaxTokenProjects. */
export const TOKEN_MAX_PROJECTS = 50;
/** Where an assistant reaches the API, and the name its client settings give the server. */
export const MCP_PATH = `${API_BASE}/mcp`;
export const MCP_SERVER_NAME = "exciter";
export const DAY_MS = 24 * 60 * 60 * 1000;
/** Where an administrator connects Armature, and where a person connects their own Armature account (#26). */
export const ARMATURE_SETTINGS_PATH = "/settings/armature";
export const ARMATURE_ACCOUNT_PATH = "/settings/armature-account";
/** Where an administrator connects Stator, and where a person connects their own Stator account (#40). */
export const STATOR_SETTINGS_PATH = "/settings/stator";
export const STATOR_ACCOUNT_PATH = "/settings/stator-account";
/** How a Stator personal access token and a webhook secret begin, the API's stator.TokenPrefix and WebhookSecretPrefix. */
export const STATOR_TOKEN_PREFIX = "stator_pat_";
export const STATOR_WEBHOOK_SECRET_PREFIX = "stator_whs_";
/** The most pages one lookup asks for, the API's stator.MaxLookupPages, and how many the picker shows. */
export const STATOR_LOOKUP_MAX_PAGES = 50;
export const STATOR_SEARCH_LIMIT = 8;
/** How long the page picker waits after a keystroke before it asks Stator. */
export const STATOR_SEARCH_DEBOUNCE_MS = 250;
/** What a page can be to a requirement, read from the requirement, in the order they are offered (#41). */
export const STATOR_RELATIONS = ["specifies", "describes", "relates"] as const;
/** How an Armature personal access token and a webhook secret begin, the API's armature.TokenPrefix and WebhookSecretPrefix. */
export const ARMATURE_TOKEN_PREFIX = "armature_pat_";
export const ARMATURE_WEBHOOK_SECRET_PREFIX = "armature_whs_";
/** The most keys one lookup asks for, the API's armature.MaxLookupKeys, and how many issues the picker offers. */
export const ARMATURE_LOOKUP_MAX_KEYS = 50;
export const ARMATURE_SEARCH_LIMIT = 10;
/** How long the picker waits after the last key press before asking Armature (#27). */
export const ARMATURE_SEARCH_DEBOUNCE_MS = 250;
/** How often a requirement's links are read again while one still waits for the worker. */
export const ARMATURE_LINKS_POLL_MS = 2000;
/** The relations a requirement takes to an issue, in the order the picker offers them, the API's armature.Relations. */
export const ARMATURE_RELATIONS = ["implements", "verifies", "relates"] as const;
/** The relations a new issue takes to its requirement, the API's armature.CreateRelations (#28). */
export const ARMATURE_CREATE_RELATIONS = ["implements", "verifies"] as const;
/** The most requirements one round of creating issues takes, the API's armature.MaxCreateItems. */
export const ARMATURE_CREATE_MAX = 50;
/** How long "Copied" stays after a secret is copied. */
export const COPY_FEEDBACK_MS = 2000;

/** How often an import the worker has is read again while a person watches it. */
export const IMPORT_POLL_MS = 1000;
/** The largest file an import takes, the API's csvio.MaxImportBytes. */
export const IMPORT_MAX_BYTES = 8 * 1024 * 1024;
/** The largest ReqIF file or archive an import takes, the API's reqifio.MaxImportBytes. */
export const REQIF_MAX_BYTES = 64 * 1024 * 1024;

/** Where a person reads what they were told, chooses how they are told, and sees what they watch (#34). */
export const NOTIFICATIONS_PATH = "/notifications";
export const NOTIFICATION_SETTINGS_PATH = "/settings/notifications";
export const WATCHING_PATH = "/settings/watching";
/** How often the bell asks for the unread count; it also asks when the window regains focus. */
export const UNREAD_POLL_MS = 30_000;
/** Above this many unread the badge shows the number with a plus. */
export const UNREAD_BADGE_MAX = 99;
/** How many notifications the bell's panel lists, within the API's limit of 100. */
export const NOTIFICATION_PANEL_SIZE = 20;
/** How many notifications one page of the notifications page lists, within the API's limit of 100. */
export const NOTIFICATIONS_PAGE_SIZE = 50;
/** How many watchers the watchers dialog lists, the most the API gives at once. */
export const WATCHERS_LIMIT = 100;
/** How many of the caller's watches one page of the watching list shows, within the API's limit of 100. */
export const WATCHES_PAGE_SIZE = 20;
/** The hour, in UTC, a daily digest goes out, matching the API's notify.DailyDigestHour. */
export const DAILY_DIGEST_HOUR_UTC = 8;

/** Where the reviews waiting on the reader are listed (#35). */
export const REVIEWS_PATH = "/reviews";
/** The most approvals a rule asks for, as the API's review.MaxRequired. */
export const REVIEW_REQUIRED_MAX = 10;
/** The longest comment a review request or decision takes, as the API's review.MaxCommentLength. */
export const REVIEW_COMMENT_MAX_LENGTH = 500;
/** How many of a requirement's reviews its panel lists, and of the waiting reviews one page of the inbox. */
export const REVIEWS_PAGE_SIZE = 20;

/** How many suspect links one page of a project's list shows, within the API's trace.SuspectMaxLimit (#36). */
export const SUSPECTS_PAGE_SIZE = 20;
/** The longest comment a clearance takes, as the API's trace.MaxClearCommentLength. */
export const CLEAR_COMMENT_MAX_LENGTH = 500;

/** How many requirements one page of a baseline read as a specification shows, the API's baseline.DefaultLimit (#37). */
export const BASELINE_SPEC_PAGE_SIZE = 50;
/** The longest name and description a baseline takes, as the API's baseline.MaxNameLength and MaxDescriptionLength. */
export const BASELINE_NAME_MAX_LENGTH = 100;
export const BASELINE_DESCRIPTION_MAX_LENGTH = 2000;

/** How many requirements one page of a coverage report lists, the API's trace.CoverageDefaultLimit (#38). */
export const COVERAGE_PAGE_SIZE = 50;
/** How many requirements one page of a traceability matrix holds, the API's trace.MatrixDefaultLimit. */
export const MATRIX_PAGE_SIZE = 50;

import { t } from "@/i18n";

// The colour tokens a theme may redefine, grouped as the editor shows them, in Armature's order. A test keeps
// the list equal to the --color-* names in the stylesheet; the labels live in the i18n catalogue.

export interface TokenGroup {
  id: keyof typeof t.themes.groups;
  tokens: string[];
}

export const TOKEN_GROUPS: TokenGroup[] = [
  { id: "surfaces", tokens: ["canvas", "surface", "surface-raised", "surface-sunken", "surface-overlay", "surface-glass"] },
  { id: "backdrop", tokens: ["backdrop-from", "backdrop-to"] },
  { id: "borders", tokens: ["border", "border-strong"] },
  { id: "text", tokens: ["ink", "ink-muted", "ink-subtle", "ink-disabled"] },
  { id: "actions", tokens: ["primary", "primary-hover", "on-primary"] },
  { id: "accent", tokens: ["accent", "accent-hover", "accent-subtle", "on-accent", "focus", "selection"] },
  { id: "feedback", tokens: ["danger", "danger-hover", "danger-subtle", "on-danger", "success", "success-subtle", "warning", "warning-subtle", "epic"] },
  {
    id: "statuses",
    tokens: ["status-todo", "status-progress", "status-done", "status-todo-subtle", "status-progress-subtle", "status-done-subtle"],
  },
  { id: "charts", tokens: ["chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "chart-6"] },
];

/** Every token name, flat, in the editor's order. */
export const TOKEN_NAMES: string[] = TOKEN_GROUPS.flatMap((group) => group.tokens);

/** What the editor calls a token; the name itself when the catalogue has no word for it. */
export function tokenLabel(name: string): string {
  return t.themes.tokens[name] ?? name;
}

/** The pointers a theme may replace, with the CSS keyword and what each lands on. */
export const CURSOR_KINDS: Array<{ kind: string; keyword: string; selector: string }> = [
  { kind: "default", keyword: "default", selector: ":root, body" },
  { kind: "pointer", keyword: "pointer", selector: 'a, button, [role="button"], [role="tab"], [role="option"], label, select, summary, .cursor-pointer' },
  {
    kind: "text",
    keyword: "text",
    selector: 'input:not([type="checkbox"]):not([type="radio"]):not([type="file"]), textarea, [contenteditable="true"], .cursor-text',
  },
  { kind: "grab", keyword: "grab", selector: ".cursor-grab" },
  { kind: "grabbing", keyword: "grabbing", selector: ".cursor-grabbing" },
  { kind: "move", keyword: "move", selector: ".cursor-move" },
  { kind: "notAllowed", keyword: "not-allowed", selector: ':disabled, [aria-disabled="true"], .cursor-not-allowed' },
  { kind: "wait", keyword: "wait", selector: ".cursor-wait, .cursor-progress" },
];

/** The three elevations a theme may redraw. */
export const SHADOW_KEYS = ["1", "2", "3"] as const;

/** The moving pictures the shell knows how to draw, as the API names them. */
export const EFFECTS = ["constellation", "confetti"] as const;

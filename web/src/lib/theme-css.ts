import { API_BASE, ICON_STROKE } from "@/config";
import { CURSOR_KINDS, SHADOW_KEYS, TOKEN_NAMES } from "./theme-tokens";

// A theme compiles to one unlayered stylesheet, beating the stylesheet's @layer theme, of only what the theme
// names, loading nothing but its own files and inline images: Armature's compiler, output for output.

export interface ThemeAssetRef {
  id: string;
  contentType: string;
}

export interface ThemeSpec {
  colors: { light: Record<string, string>; dark: Record<string, string> };
  fonts: { sans?: { family: string; assetId?: string }; mono?: { family: string; assetId?: string } };
  shape: { radiusControl?: number; radiusOverlay?: number };
  shadows: Record<string, string>;
  cursors: Record<string, { assetId: string; hotspotX: number; hotspotY: number }>;
  icons: Record<string, { assetId?: string; paths?: string[] }>;
  backdrop?: { assetId: string; fit: "cover" | "tile" } | null;
  /** A moving picture the shell draws live: "constellation" or "confetti". */
  effect?: string;
  css: string;
}

export interface CompilableTheme {
  id: string;
  spec: ThemeSpec;
  assets: ThemeAssetRef[];
}

/** Where a theme's file is served from; the id never changes, so the browser keeps it. */
export function themeAssetURL(themeId: string, assetId: string): string {
  return `${API_BASE}/themes/${themeId}/assets/${assetId}`;
}

/** An empty theme: nothing overridden. */
export function emptySpec(): ThemeSpec {
  return { colors: { light: {}, dark: {} }, fonts: {}, shape: {}, shadows: {}, cursors: {}, icons: {}, backdrop: null, css: "" };
}

const known = new Set(TOKEN_NAMES);
const shadowKeys = new Set<string>(SHADOW_KEYS);

function declarations(set: Record<string, string>): string {
  return Object.entries(set)
    .filter(([name]) => known.has(name))
    .map(([name, value]) => `  --color-${name}: ${value};`)
    .join("\n");
}

function url(href: string): string {
  return `url(${JSON.stringify(href)})`;
}

function fontFormat(contentType: string): string {
  return contentType === "font/woff2" ? "woff2" : "woff";
}

/** Path data drawn on the 16px icon grid, as an inline image a mask can read. */
export function pathsAsImage(paths: string[]): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="black" stroke-width="${ICON_STROKE}" stroke-linecap="round" stroke-linejoin="round">${paths.map((d) => `<path d="${d}"/>`).join("")}</svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

/** The stylesheet a theme is. */
export function compileTheme(theme: CompilableTheme): string {
  const { spec } = theme;
  const asset = (id: string) => themeAssetURL(theme.id, id);
  const assetType = (id: string) => theme.assets.find((a) => a.id === id)?.contentType ?? "";
  const out: string[] = [];

  const light = declarations(spec.colors.light);
  if (light) {
    out.push(`:root[data-theme="light"] {\n${light}\n}`);
    out.push(`@media (prefers-color-scheme: light) {\n:root:not([data-theme="dark"]) {\n${light}\n}\n}`);
  }
  const dark = declarations(spec.colors.dark);
  if (dark) {
    out.push(`:root[data-theme="dark"] {\n${dark}\n}`);
    out.push(`@media (prefers-color-scheme: dark) {\n:root:not([data-theme="light"]) {\n${dark}\n}\n}`);
  }

  const root: string[] = [];
  for (const [role, fallback] of [
    ["sans", "ui-sans-serif, system-ui, sans-serif"],
    ["mono", "ui-monospace, monospace"],
  ] as const) {
    const font = spec.fonts[role];
    if (!font) continue;
    if (font.assetId) {
      out.push(
        `@font-face {\n  font-family: ${JSON.stringify(font.family)};\n  src: ${url(asset(font.assetId))} format("${fontFormat(assetType(font.assetId))}");\n  font-display: swap;\n}`,
      );
    }
    root.push(`  --font-${role}: ${JSON.stringify(font.family)}, ${fallback};`);
  }
  if (spec.shape.radiusControl !== undefined) root.push(`  --radius-control: ${spec.shape.radiusControl}px;`);
  if (spec.shape.radiusOverlay !== undefined) root.push(`  --radius-overlay: ${spec.shape.radiusOverlay}px;`);
  for (const [key, value] of Object.entries(spec.shadows)) {
    if (shadowKeys.has(key)) root.push(`  --shadow-${key}: ${value};`);
  }
  if (root.length) out.push(`:root {\n${root.join("\n")}\n}`);

  for (const each of CURSOR_KINDS) {
    const cursor = spec.cursors[each.kind];
    if (!cursor) continue;
    out.push(`${each.selector} {\n  cursor: ${url(asset(cursor.assetId))} ${cursor.hotspotX} ${cursor.hotspotY}, ${each.keyword};\n}`);
  }

  for (const [name, icon] of Object.entries(spec.icons)) {
    const image = icon.assetId ? asset(icon.assetId) : icon.paths?.length ? pathsAsImage(icon.paths) : null;
    if (!image) continue;
    const mask = `${url(image)} center / contain no-repeat`;
    out.push(`svg[data-icon=${JSON.stringify(name)}] > path {\n  display: none;\n}`);
    out.push(`svg[data-icon=${JSON.stringify(name)}] {\n  background-color: currentColor;\n  -webkit-mask: ${mask};\n  mask: ${mask};\n}`);
  }

  if (spec.backdrop) {
    const image = url(asset(spec.backdrop.assetId));
    out.push(
      spec.backdrop.fit === "tile"
        ? `.bg-backdrop {\n  background: ${image} repeat;\n}`
        : `.bg-backdrop {\n  background: ${image} center / cover no-repeat;\n}`,
    );
  }

  if (spec.css.trim()) out.push(spec.css.trim());
  return out.join("\n\n");
}

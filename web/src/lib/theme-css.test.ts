import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { compileTheme, emptySpec, pathsAsImage, themeAssetURL, type ThemeSpec } from "./theme-css";
import { TOKEN_NAMES } from "./theme-tokens";

const id = "11111111-1111-1111-1111-111111111111";
const file = "22222222-2222-2222-2222-222222222222";

function themed(spec: Partial<ThemeSpec>, assets: Array<{ id: string; contentType: string }> = []) {
  return compileTheme({ id, spec: { ...emptySpec(), ...spec }, assets });
}

describe("compiling a theme", () => {
  it("writes nothing for an empty theme", () => {
    expect(themed({})).toBe("");
  });

  // Light overrides must not leak into the dark theme: the dark palette
  // lives in a layer, and an unlayered :root rule would beat it.
  it("puts light colours under the light selectors only, and dark under the dark ones", () => {
    const css = themed({ colors: { light: { accent: "#ff0066" }, dark: { accent: "#00ffcc" } } });
    expect(css).toContain(':root[data-theme="light"] {\n  --color-accent: #ff0066;');
    expect(css).toContain('@media (prefers-color-scheme: light) {\n:root:not([data-theme="dark"]) {\n  --color-accent: #ff0066;');
    expect(css).toContain(':root[data-theme="dark"] {\n  --color-accent: #00ffcc;');
    expect(css).toContain('@media (prefers-color-scheme: dark) {\n:root:not([data-theme="light"]) {\n  --color-accent: #00ffcc;');
    expect(css).not.toMatch(/^:root \{/m);
  });

  it("ignores a token the stylesheet does not have", () => {
    expect(themed({ colors: { light: { chartreuse: "#7fff00" }, dark: {} } })).toBe("");
  });

  it("makes a face from an uploaded font and names it on the root", () => {
    const css = themed({ fonts: { sans: { family: "Atkinson", assetId: file } } }, [{ id: file, contentType: "font/woff2" }]);
    expect(css).toContain(`@font-face {\n  font-family: "Atkinson";\n  src: url("${themeAssetURL(id, file)}") format("woff2");`);
    expect(css).toContain('--font-sans: "Atkinson", ui-sans-serif, system-ui, sans-serif;');
  });

  it("sets radii and shadows on the root", () => {
    const css = themed({ shape: { radiusControl: 2, radiusOverlay: 14 }, shadows: { "2": "0 4px 8px #0003", "9": "nope" } });
    expect(css).toContain("--radius-control: 2px;");
    expect(css).toContain("--radius-overlay: 14px;");
    expect(css).toContain("--shadow-2: 0 4px 8px #0003;");
    expect(css).not.toContain("--shadow-9");
  });

  it("points a cursor at the theme's file with its hotspot and the keyword behind it", () => {
    const css = themed({ cursors: { pointer: { assetId: file, hotspotX: 3, hotspotY: 4 }, notAllowed: { assetId: file, hotspotX: 0, hotspotY: 0 } } });
    expect(css).toContain(`cursor: url("${themeAssetURL(id, file)}") 3 4, pointer;`);
    expect(css).toContain('a, button, [role="button"]');
    expect(css).toContain(`cursor: url("${themeAssetURL(id, file)}") 0 0, not-allowed;`);
  });

  it("replaces an icon by masking the glyph with the file, or with path data drawn inline", () => {
    const css = themed({ icons: { home: { assetId: file }, search: { paths: ["M2 2h12"] } } });
    expect(css).toContain('svg[data-icon="home"] > path {\n  display: none;');
    expect(css).toContain(
      `svg[data-icon="home"] {\n  background-color: currentColor;\n  -webkit-mask: url("${themeAssetURL(id, file)}") center / contain no-repeat;`,
    );
    expect(css).toContain(`svg[data-icon="search"] {\n  background-color: currentColor;\n  -webkit-mask: url("${pathsAsImage(["M2 2h12"])}")`);
    expect(pathsAsImage(["M2 2h12"])).toMatch(/^data:image\/svg\+xml,/);
  });

  it("draws the backdrop as a picture, covering or tiled", () => {
    expect(themed({ backdrop: { assetId: file, fit: "cover" } })).toContain(
      `.bg-backdrop {\n  background: url("${themeAssetURL(id, file)}") center / cover no-repeat;`,
    );
    expect(themed({ backdrop: { assetId: file, fit: "tile" } })).toContain(`.bg-backdrop {\n  background: url("${themeAssetURL(id, file)}") repeat;`);
  });

  it("appends the extra CSS last", () => {
    const css = themed({ colors: { light: { ink: "#000" }, dark: {} }, css: "[data-rail] { opacity: .5 }" });
    expect(css.endsWith("[data-rail] { opacity: .5 }")).toBe(true);
  });

  // The file Armature exports and the Themes page imports: a theme drawn in
  // square blocks and hard shadows, with every colour one this stylesheet has.
  it("compiles Armature's Minecraft theme file as it came", () => {
    const path = join(__dirname, "..", "..", "..", "backend", "internal", "theme", "testdata", "minecraft.armature-theme.json");
    const pkg = JSON.parse(readFileSync(path, "utf8")) as { format: string; name: string; spec: ThemeSpec };
    expect(pkg.format).toBe("armature-theme/1");
    const known = new Set(TOKEN_NAMES);
    expect(Object.keys(pkg.spec.colors.light).filter((name) => !known.has(name))).toEqual([]);
    expect(Object.keys(pkg.spec.colors.dark).filter((name) => !known.has(name))).toEqual([]);

    const css = compileTheme({ id, spec: { ...emptySpec(), ...pkg.spec }, assets: [] });
    expect(css).toContain(`--color-canvas: ${pkg.spec.colors.light.canvas};`);
    expect(css).toContain(`--color-canvas: ${pkg.spec.colors.dark.canvas};`);
    expect(css).toContain("--radius-control: 0px;");
    expect(css).toContain("--radius-overlay: 0px;");
    expect(css).toContain(`--shadow-1: ${pkg.spec.shadows["1"]};`);
    expect(css.endsWith(pkg.spec.css.trim())).toBe(true);
    expect(css).not.toContain(".bg-backdrop {\n  background: url(");
  });
});

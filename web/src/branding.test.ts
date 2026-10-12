import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = process.cwd();
const head = new DOMParser().parseFromString(readFileSync(resolve(root, "index.html"), "utf8"), "text/html").head;
const publicFile = (href: string) => resolve(root, "public", href.replace(/^\//, ""));

describe("the icons the document head names", () => {
  it("are a favicon, a touch icon and a manifest", () => {
    const rels = Array.from(head.querySelectorAll("link[rel]")).map((link) => link.getAttribute("rel"));
    expect(rels).toEqual(expect.arrayContaining(["icon", "apple-touch-icon", "manifest"]));
  });

  it("all exist in web/public, which the build copies to the site's root", () => {
    for (const link of Array.from(head.querySelectorAll("link[href]"))) {
      const href = link.getAttribute("href")!;
      expect(existsSync(publicFile(href)), `${href} is in web/public`).toBe(true);
    }
  });

  it("are the icons the manifest names too, and the manifest is valid JSON", () => {
    const manifest = JSON.parse(readFileSync(publicFile("/site.webmanifest"), "utf8")) as { icons: { src: string }[] };
    expect(manifest.icons.length).toBeGreaterThan(0);
    for (const icon of manifest.icons) expect(existsSync(publicFile(icon.src)), `${icon.src} is in web/public`).toBe(true);
  });
});

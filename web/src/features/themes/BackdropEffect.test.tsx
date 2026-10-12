import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { BackdropEffect } from "./BackdropEffect";
import { setThemePreview } from "./ThemeLoader";
import { withAlpha } from "@/lib/constellation";

const active = vi.fn();
vi.mock("@/api/themes", () => ({ useActiveTheme: () => ({ data: active() }) }));

describe("the moving picture behind the page", () => {
  afterEach(() => act(() => setThemePreview(null)));

  it("is not drawn for the built-in theme nor for one that asks for nothing", () => {
    active.mockReturnValue({ theme: null });
    expect(render(<BackdropEffect />).container.querySelector("[data-backdrop-effect]")).toBeNull();
    active.mockReturnValue({ theme: { spec: { effect: undefined } } });
    expect(render(<BackdropEffect />).container.querySelector("[data-backdrop-effect]")).toBeNull();
  });

  it("is mounted, with its canvas, when the chosen theme asks for the constellation", () => {
    active.mockReturnValue({ theme: { spec: { effect: "constellation" } } });
    const { container } = render(<BackdropEffect />);
    const mount = container.querySelector('[data-backdrop-effect="constellation"]');
    expect(mount).not.toBeNull();
    expect(mount!.querySelector("canvas")).not.toBeNull();
    expect(mount!.getAttribute("aria-hidden")).toBe("true");
  });

  it("lays the confetti over the page when the chosen theme asks for it", () => {
    active.mockReturnValue({ theme: { spec: { effect: "confetti" } } });
    const { container } = render(<BackdropEffect />);
    const mount = container.querySelector('[data-backdrop-effect="confetti"]');
    expect(mount).not.toBeNull();
    expect(mount!.querySelector("canvas")).not.toBeNull();
    expect(mount!.className).toContain("pointer-events-none");
  });

  it("follows the editor's draft while one is previewed", () => {
    active.mockReturnValue({ theme: null });
    setThemePreview("", "constellation");
    const { container } = render(<BackdropEffect />);
    expect(container.querySelector("[data-backdrop-effect]")).not.toBeNull();
  });

  it("turns the theme's colours into what a canvas takes", () => {
    expect(withAlpha("#5cc8ff", 0.5)).toBe("rgb(92 200 255 / 0.5)");
    expect(withAlpha("#abc", 1)).toBe("rgb(170 187 204 / 1)");
    expect(withAlpha("rgb(1 2 3 / 0.9)", 0.2)).toBe("rgb(1 2 3 / 0.2)");
    expect(withAlpha("hotpink", 0.3)).toBe("rgb(128 128 128 / 0.3)");
  });
});

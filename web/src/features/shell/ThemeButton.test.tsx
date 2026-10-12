import { beforeEach, describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { THEME_STORAGE_KEY } from "@/lib/theme";
import { renderAt } from "@/test/app";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

describe("the rail's theme button", () => {
  it("steps through Auto, Light and Dark, stamping the page and remembering the choice", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const button = () => rail.querySelector<HTMLElement>('[data-action="theme"]')!;
    expect(button()).toHaveAccessibleName("Auto");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);

    await userEvent.click(button());
    expect(button()).toHaveAccessibleName("Light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    await userEvent.click(button());
    expect(button()).toHaveAccessibleName("Dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");

    await userEvent.click(button());
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
    expect(screen.getAllByRole("button", { name: "Auto" }).length).toBeGreaterThan(0);
  });
});

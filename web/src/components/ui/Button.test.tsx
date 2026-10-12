import { describe, expect, it, vi } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import type { FormEvent, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Button, IconButton } from "./Button";

// Reading every file of the client outlasted vitest's five seconds twice on a
// busy host; the scan is a few hundred files, not a wait on anything.
const SOURCE_SCAN_MS = 30_000;

function inForm(children: ReactNode) {
  const submitted = vi.fn((event: FormEvent) => event.preventDefault());
  render(<form onSubmit={submitted}>{children}</form>);
  return submitted;
}

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sources(path);
    return /\.tsx$/.test(entry.name) && !/\.test\.tsx$/.test(entry.name) ? [path] : [];
  });
}

describe("buttons inside a form", () => {
  it("do not submit it unless they say so", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    const submitted = inForm(
      <>
        <Button onClick={onClick}>Bold</Button>
        <IconButton icon={null} label="Italic" onClick={onClick} />
      </>,
    );
    expect(screen.getByRole("button", { name: "Bold" })).toHaveAttribute("type", "button");
    expect(screen.getByRole("button", { name: "Italic" })).toHaveAttribute("type", "button");
    await user.click(screen.getByRole("button", { name: "Bold" }));
    await user.click(screen.getByRole("button", { name: "Italic" }));
    expect(onClick).toHaveBeenCalledTimes(2);
    expect(submitted).not.toHaveBeenCalled();
  });

  it("submit it from the button marked submit", async () => {
    const user = userEvent.setup();
    const submitted = inForm(
      <>
        <Button>Cancel</Button>
        <Button type="submit">Save</Button>
      </>,
    );
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(submitted).toHaveBeenCalledTimes(1);
  });

  // Buttons no longer submit by default, so a form whose submit button was
  // never marked would lose its submit without anything else failing.
  it(
    "leave every form in the client a submit button",
    () => {
      const forms = sources(join(__dirname, "..", ".."))
        .map((path) => ({ path, source: readFileSync(path, "utf8") }))
        .filter(({ source }) => /<form\b/.test(source));
      expect(forms.length).toBeGreaterThan(0);
      for (const { path, source } of forms) {
        const opened = source.match(/<form\b/g)!.length;
        const submits = source.match(/type="submit"/g)?.length ?? 0;
        expect(submits, `${path} has ${opened} form(s) and ${submits} submit button(s)`).toBeGreaterThanOrEqual(opened);
      }
    },
    SOURCE_SCAN_MS,
  );
});

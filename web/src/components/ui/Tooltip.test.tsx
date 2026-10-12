import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TOOLTIP_DELAY_MS } from "@/config";
import { Tooltip } from "./Tooltip";

describe("the tooltip", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("shows after its delay while its anchor has focus", () => {
    vi.useFakeTimers();
    render(
      <Tooltip text="Opens the page">
        <button type="button">Open</button>
      </Tooltip>,
    );
    fireEvent.focus(screen.getByRole("button", { name: "Open" }));
    expect(screen.queryByRole("tooltip")).toBeNull();
    act(() => vi.advanceTimersByTime(TOOLTIP_DELAY_MS));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Opens the page");
  });

  it("does nothing once it is gone before its delay runs out", () => {
    vi.useFakeTimers();
    const errors = vi.spyOn(console, "error");
    const { unmount } = render(
      <Tooltip text="Opens the page">
        <button type="button">Open</button>
      </Tooltip>,
    );
    fireEvent.focus(screen.getByRole("button", { name: "Open" }));
    unmount();
    expect(vi.getTimerCount()).toBe(0);
    act(() => vi.advanceTimersByTime(TOOLTIP_DELAY_MS));
    expect(errors).not.toHaveBeenCalled();
  });
});

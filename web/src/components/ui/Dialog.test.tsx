import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Dialog } from "./Dialog";

const scrimOf = () => screen.getByRole("dialog").parentElement as HTMLElement;

describe("a dialog's scrim", () => {
  it("closes the dialog on a click that began and ended on it", () => {
    const onClose = vi.fn();
    render(
      <Dialog title="Title" onClose={onClose}>
        <p>Inside</p>
      </Dialog>,
    );
    fireEvent.mouseDown(scrimOf());
    fireEvent.click(scrimOf());
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not close it when the press began inside and the dialog shrank away from under the pointer", () => {
    const onClose = vi.fn();
    render(
      <Dialog title="Title" onClose={onClose}>
        <p>Inside</p>
      </Dialog>,
    );
    fireEvent.mouseDown(screen.getByText("Inside"));
    // The browser names the common ancestor as the target of such a click.
    fireEvent.click(scrimOf());
    expect(onClose).not.toHaveBeenCalled();
  });

  it("does not close it on a click inside", () => {
    const onClose = vi.fn();
    render(
      <Dialog title="Title" onClose={onClose}>
        <p>Inside</p>
      </Dialog>,
    );
    fireEvent.mouseDown(screen.getByText("Inside"));
    fireEvent.click(screen.getByText("Inside"));
    expect(onClose).not.toHaveBeenCalled();
  });
});

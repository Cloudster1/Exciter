import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Button, ConfirmDialog, Dialog, DialogFooter, DockedPanel, Drawer, Menu, Popover } from "./index";
import { MENU_GAP_PX, POPOVER_EDGE_PX, POPOVER_MAX_HEIGHT_PX } from "@/config";
import { anchoredBox } from "./overlay";

describe("Dialog", () => {
  it("is modal, labelled by its title, closes on Escape and returns focus", async () => {
    function Host() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>Open</Button>
          <Dialog open={open} onClose={() => setOpen(false)} title="Rename folder" description="A new name.">
            <input aria-label="Name" />
          </Dialog>
        </>
      );
    }
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Open" });
    await userEvent.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Rename folder" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleDescription("A new name.");
    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(opener);
  });

  it("draws nothing while closed", () => {
    render(<Dialog open={false} onClose={() => {}} title="Hidden" />);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps Tab inside", async () => {
    render(
      <Dialog
        open
        onClose={() => {}}
        title="Two buttons"
        footer={
          <>
            <Button>One</Button>
            <Button>Two</Button>
          </>
        }
      />,
    );
    screen.getByRole("button", { name: "Two" }).focus();
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
  });

  it("is as wide as its size says, and carries the attributes the tests read", () => {
    const { rerender } = render(<Dialog open onClose={() => {}} title="Sized" size="sm" attrs={{ "data-sized": "" }} />);
    expect(screen.getByRole("dialog")).toHaveClass("max-w-sm");
    expect(screen.getByRole("dialog")).toHaveAttribute("data-sized", "");
    rerender(<Dialog open onClose={() => {}} title="Sized" />);
    expect(screen.getByRole("dialog")).toHaveClass("max-w-lg");
    rerender(<Dialog open onClose={() => {}} title="Sized" size="lg" />);
    expect(screen.getByRole("dialog")).toHaveClass("max-w-3xl");
  });
});

// jsdom lays nothing out, so what keeps a tall dialog in reach is read off the
// structure; the browser suite turns the wheel over it.
describe("DialogFooter", () => {
  it("draws a form's buttons in the pinned footer, where the submit still sends its form", async () => {
    const sent = vi.fn((event: { preventDefault: () => void }) => event.preventDefault());
    render(
      <Dialog open title="Rename" onClose={() => {}}>
        <form id="rename" onSubmit={sent}>
          <input aria-label="Name" />
          <DialogFooter>
            <Button type="submit" form="rename">
              Save
            </Button>
          </DialogFooter>
        </form>
      </Dialog>,
    );
    const save = await screen.findByRole("button", { name: "Save" });
    expect(save.closest("[data-dialog-footer]")).not.toBeNull();
    expect(save.closest("form")).toBeNull();
    await userEvent.click(save);
    expect(sent).toHaveBeenCalledTimes(1);
  });

  it("takes no room when nothing is drawn in it", () => {
    render(
      <Dialog open title="Note" onClose={() => {}}>
        <p>Only words.</p>
      </Dialog>,
    );
    expect(document.querySelector("[data-dialog-footer]")).toBeEmptyDOMElement();
  });
});

describe("Dialog taller than the window", () => {
  it("sits on a layer that scrolls like a page, with its title and buttons pinned to its edges", () => {
    render(
      <Dialog open onClose={() => {}} title="Many fields" footer={<Button>Create page</Button>}>
        <input aria-label="Last field" />
      </Dialog>,
    );
    const dialog = screen.getByRole("dialog", { name: "Many fields" });
    const layer = dialog.parentElement!;
    expect(layer).toHaveAttribute("data-dialog-backdrop");
    expect(layer).toHaveClass("fixed", "inset-0", "overflow-y-auto", "overscroll-contain");
    expect(dialog.querySelector("[data-dialog-body]")).not.toHaveClass("overflow-y-auto");
    expect(dialog.querySelector("[data-dialog-header]")).toHaveClass("tall:sticky", "tall:top-0");
    expect(dialog.querySelector("[data-dialog-footer]")).toHaveClass("tall:sticky", "tall:bottom-0");
    expect(dialog.querySelector("[data-dialog-footer]")).toContainElement(screen.getByRole("button", { name: "Create page" }));
  });

  it("stays open when its scrollbar is pressed and closes on a press beside it", () => {
    const onClose = vi.fn();
    render(<Dialog open onClose={onClose} title="Many fields" />);
    const layer = screen.getByRole("dialog").parentElement!;
    Object.defineProperty(layer, "clientWidth", { value: 1265 });
    // jsdom's pointer events carry no coordinates; a mouse event of that type does.
    const press = (clientX: number) => fireEvent(layer, new MouseEvent("pointerdown", { bubbles: true, clientX }));
    press(1272);
    expect(onClose).not.toHaveBeenCalled();
    press(20);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("stays open when a press began inside it, even if it ends on the layer", () => {
    const onClose = vi.fn();
    render(
      <Dialog open onClose={onClose} title="Shrinking">
        <p>Inside</p>
      </Dialog>,
    );
    fireEvent.pointerDown(screen.getByText("Inside"));
    fireEvent.click(screen.getByRole("dialog").parentElement!);
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("anchoredBox", () => {
  const view = { width: 1280, height: 600 };
  const anchor = (top: number, bottom = top + 30) => ({ top, bottom, left: 100, right: 300, height: bottom - top });

  it("goes below with the room that is left there when it fits", () => {
    const box = anchoredBox(anchor(100), { width: 200, height: 300 }, view);
    expect(box.top).toBe(130 + MENU_GAP_PX);
    expect(box.left).toBe(100);
    expect(box.maxHeight).toBe(Math.min(POPOVER_MAX_HEIGHT_PX, 600 - POPOVER_EDGE_PX - 130 - MENU_GAP_PX));
  });

  it("goes above when only that side has room", () => {
    const box = anchoredBox(anchor(500), { width: 200, height: 300 }, view);
    expect(box.top).toBe(500 - MENU_GAP_PX - 300);
    expect(box.maxHeight).toBe(500 - MENU_GAP_PX - POPOVER_EDGE_PX);
  });

  it("takes the roomier side and scrolls when neither side has room", () => {
    const below = anchoredBox(anchor(200), { width: 200, height: 900 }, view);
    expect(below.top).toBe(230 + MENU_GAP_PX);
    expect(below.maxHeight).toBe(600 - POPOVER_EDGE_PX - 230 - MENU_GAP_PX);
    const above = anchoredBox(anchor(400), { width: 200, height: 900 }, view);
    expect(above.maxHeight).toBe(400 - MENU_GAP_PX - POPOVER_EDGE_PX);
    expect(above.top).toBe(POPOVER_EDGE_PX);
  });

  it("never grows past the cap on a tall window", () => {
    const box = anchoredBox(anchor(10), { width: 200, height: 5000 }, { width: 1280, height: 4000 });
    expect(box.maxHeight).toBe(POPOVER_MAX_HEIGHT_PX);
  });

  it("lines up the right edges for an end-aligned panel", () => {
    expect(anchoredBox(anchor(100), { width: 200, height: 100 }, view, { align: "end" }).right).toBe(1280 - 300);
  });
});

describe("ConfirmDialog", () => {
  it("names the verb and the noun on the button that does it", async () => {
    const onConfirm = vi.fn();
    render(<ConfirmDialog open onClose={() => {}} onConfirm={onConfirm} noun="comment" body="Its replies go with it." />);
    expect(screen.getByRole("dialog", { name: "Delete comment?" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Delete comment" }));
    expect(onConfirm).toHaveBeenCalled();
  });
});

describe("Drawer", () => {
  it("opens from the side with its title and closes on Escape", async () => {
    const onClose = vi.fn();
    render(
      <Drawer open onClose={onClose} title="Preview" attrs={{ "data-preview-dialog": "1" }}>
        <p>Body</p>
      </Drawer>,
    );
    expect(screen.getByRole("dialog", { name: "Preview" })).toHaveAttribute("data-preview-dialog", "1");
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("leaves Escape to a field that is being typed in", async () => {
    const onClose = vi.fn();
    render(
      <Drawer open onClose={onClose} title="Review">
        <textarea aria-label="Comment" />
      </Drawer>,
    );
    await userEvent.click(screen.getByRole("textbox", { name: "Comment" }));
    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("DockedPanel", () => {
  it("sits beside the page without a scrim or a trap, and Escape closes it", async () => {
    const onClose = vi.fn();
    render(
      <>
        <button type="button">Outside</button>
        <DockedPanel open onClose={onClose} title="Comments" width={400}>
          <button type="button">Inside</button>
        </DockedPanel>
      </>,
    );
    const panel = screen.getByRole("complementary", { name: "Comments" });
    expect(panel).not.toHaveAttribute("aria-modal");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("button", { name: "Inside" })).toHaveFocus();
    await userEvent.tab();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "Outside" })).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("Menu groups, marks and separators", () => {
  const trigger = ({ toggle, ...aria }: { toggle: () => void; open: boolean }) => (
    <button type="button" onClick={toggle} {...aria}>
      Open
    </button>
  );

  it("marks the item picked among a set and reads it out as checked", async () => {
    render(
      <Menu
        label="Language"
        trigger={trigger}
        items={[
          { label: "English", checked: true, onSelect: () => {} },
          { label: "Deutsch", checked: false, onSelect: () => {} },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("menuitemradio", { name: "English" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemradio", { name: "Deutsch" })).toHaveAttribute("aria-checked", "false");
  });

  it("reads a group out by its heading, and sets a separated item apart by a line", async () => {
    render(
      <Menu
        label="Your account"
        trigger={trigger}
        items={[
          { label: "Profile", onSelect: () => {} },
          { label: "Sign out", separated: true, onSelect: () => {} },
          { group: "Switch to", items: [{ label: "Armature", href: "https://armature.example" }] },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("group", { name: "Switch to" })).toContainElement(screen.getByRole("menuitem", { name: "Armature" }));
    expect(screen.getAllByRole("separator")).toHaveLength(1);
  });
});

describe("Popover", () => {
  it("closes on a press outside and on Escape", async () => {
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <p>Outside</p>
          <Popover
            open={open}
            onClose={() => setOpen(false)}
            label="Filters"
            trigger={
              <button type="button" onClick={() => setOpen(true)}>
                Filter
              </button>
            }
          >
            <button type="button">Only mine</button>
          </Popover>
        </>
      );
    }
    render(<Host />);
    expect(screen.getByRole("dialog", { name: "Filters" })).toBeInTheDocument();
    await userEvent.click(screen.getByText("Outside"));
    expect(screen.queryByRole("dialog")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Filter" }));
    expect(screen.getByRole("dialog", { name: "Filters" })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
